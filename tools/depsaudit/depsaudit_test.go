package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestRuntimeDependencyPolicyIsExact(t *testing.T) {
	t.Parallel()

	want := []DependencyPolicy{
		{Module: "github.com/coder/websocket", EarliestGate: "R2", AllowedPackages: []string{"internal/transport"}},
		{Module: "github.com/jackc/pgx/v5", EarliestGate: "R1", AllowedPackages: []string{"internal/datasource/postgres"}},
		{Module: "github.com/lestrrat-go/jwx/v2", EarliestGate: "R3", AllowedPackages: []string{"internal/auth/oidc"}},
		{Module: "github.com/nats-io/nats.go", EarliestGate: "R5", AllowedPackages: []string{"internal/bus/nats"}},
		{Module: "github.com/prometheus/client_golang", EarliestGate: "R8", AllowedPackages: []string{"internal/observability", "cmd/conduit"}},
		{Module: "github.com/vektah/gqlparser/v2", EarliestGate: "R1", AllowedPackages: []string{"internal/graphql/ast"}},
		{Module: "go.opentelemetry.io/otel", EarliestGate: "R8", AllowedPackages: []string{"internal/observability", "cmd/conduit"}},
		{Module: "go.opentelemetry.io/otel/exporters/prometheus", EarliestGate: "R8", AllowedPackages: []string{"internal/observability", "cmd/conduit"}},
		{Module: "go.opentelemetry.io/otel/sdk", EarliestGate: "R8", AllowedPackages: []string{"internal/observability", "cmd/conduit"}},
		{Module: "gopkg.in/yaml.v3", EarliestGate: "R0", AllowedPackages: []string{"internal/config"}},
	}
	got := RuntimeAllowlist()
	sortPolicies(got)
	sortPolicies(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime allowlist drifted\ngot:  %#v\nwant: %#v", got, want)
	}

	wantLicenses := []string{"Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "MIT"}
	gotLicenses := AllowedLicenses()
	sort.Strings(gotLicenses)
	if !reflect.DeepEqual(gotLicenses, wantLicenses) {
		t.Fatalf("license allowlist = %v, want %v", gotLicenses, wantLicenses)
	}
}

func TestAuditAcceptsApprovedVendoredRuntimeAndReviewedTestDependency(t *testing.T) {
	t.Parallel()

	got, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:               filepath.Join("testdata", "valid"),
		GateStatusPath:           filepath.Join("testdata", "valid", "docs", "gate-status.json"),
		allowFixtureReplacements: true,
	})
	if err != nil {
		t.Fatalf("Audit(valid): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Audit(valid) returned findings:\n%s", formatAuditFindings(got))
	}
}

func TestAuditRejectsReplaceBeforeLoadingThePackageGraph(t *testing.T) {
	t.Parallel()

	root := copyDependencyFixture(t, "valid")
	if err := os.Remove(filepath.Join(root, "vendor", "modules.txt")); err != nil {
		t.Fatalf("remove vendor manifest: %v", err)
	}
	_, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:     root,
		GateStatusPath: filepath.Join(root, "docs", "gate-status.json"),
	})
	assertAuditErrorContains(t, err, "replace directive")
	if strings.Contains(err.Error(), "package graph") || strings.Contains(err.Error(), "vendor") {
		t.Fatalf("replace rejection did not precede graph loading: %v", err)
	}
}

func TestReplaceDetectionUsesTheGoParserForSingleAndBlockForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		directive string
	}{
		{name: "single", directive: "replace example.com/dependency => ./dependency\n"},
		{name: "block", directive: "replace (\n\texample.com/dependency => ./dependency\n)\n"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			goMod := "module example.com/replacetest\n\ngo 1.23.0\n\n" + test.directive
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}
			err := rejectGoModReplacements(context.Background(), root)
			assertAuditErrorContains(t, err, "replace directive")
		})
	}
}

func TestVendoredTreeDigestDetectsSourceAndLicenseChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "source", path: filepath.Join("vendor", "github.com", "coder", "websocket", "websocket.go")},
		{name: "license", path: filepath.Join("vendor", "github.com", "coder", "websocket", "LICENSE")},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := copyDependencyFixture(t, "valid")
			path := filepath.Join(root, test.path)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read vendored fixture file: %v", err)
			}
			contents = append(contents, []byte("\nchanged after review\n")...)
			if err := os.WriteFile(path, contents, 0o644); err != nil {
				t.Fatalf("mutate vendored fixture file: %v", err)
			}

			findings, err := auditDependencyFixture(root)
			if err != nil {
				t.Fatalf("Audit(modified vendor tree): %v", err)
			}
			assertAuditFinding(t, findings, "vendor-tree-digest-mismatch", "github.com/coder/websocket", "")
		})
	}
}

func TestDependencyReviewEntriesFailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*dependencyReviewManifest)
		wantKind string
		module   string
	}{
		{
			name: "missing review",
			mutate: func(manifest *dependencyReviewManifest) {
				manifest.Reviews = manifest.Reviews[1:]
			},
			wantKind: "missing-review",
			module:   "example.com/testhelper",
		},
		{
			name: "mismatched version",
			mutate: func(manifest *dependencyReviewManifest) {
				manifest.Reviews[0].Version = "v9.9.9"
			},
			wantKind: "review-version-mismatch",
			module:   "example.com/testhelper",
		},
		{
			name: "mismatched digest",
			mutate: func(manifest *dependencyReviewManifest) {
				manifest.Reviews[0].VendorTreeSHA256 = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
			},
			wantKind: "vendor-tree-digest-mismatch",
			module:   "example.com/testhelper",
		},
		{
			name: "duplicate review",
			mutate: func(manifest *dependencyReviewManifest) {
				manifest.Reviews = append(manifest.Reviews, manifest.Reviews[0])
			},
			wantKind: "duplicate-review",
			module:   "example.com/testhelper",
		},
		{
			name: "extra unknown module review",
			mutate: func(manifest *dependencyReviewManifest) {
				extra := manifest.Reviews[0]
				extra.Module = "unknown.example/dependency"
				manifest.Reviews = append(manifest.Reviews, extra)
			},
			wantKind: "extra-review",
			module:   "unknown.example/dependency",
		},
		{
			name: "unaccepted review",
			mutate: func(manifest *dependencyReviewManifest) {
				manifest.Reviews[0].Status = "pending"
			},
			wantKind: "malformed-review",
			module:   "example.com/testhelper",
		},
		{
			name: "path escape",
			mutate: func(manifest *dependencyReviewManifest) {
				manifest.Reviews[0].ReviewFile = "../../outside.md"
			},
			wantKind: "malformed-review",
			module:   "example.com/testhelper",
		},
		{
			name: "missing review file",
			mutate: func(manifest *dependencyReviewManifest) {
				manifest.Reviews[0].ReviewFile = "absent.md"
			},
			wantKind: "malformed-review",
			module:   "example.com/testhelper",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := copyDependencyFixture(t, "valid")
			manifestPath := filepath.Join(root, "docs", "dependencies", "reviews.json")
			manifest, err := readDependencyReviewManifest(manifestPath)
			if err != nil {
				t.Fatalf("read review manifest: %v", err)
			}
			test.mutate(&manifest)
			writeDependencyReviewManifest(t, manifestPath, manifest)

			findings, err := auditDependencyFixture(root)
			if err != nil {
				t.Fatalf("Audit(hostile review manifest): %v", err)
			}
			assertAuditFinding(t, findings, test.wantKind, test.module, "")
		})
	}
}

func TestDependencyReviewManifestRejectsUnknownAndMalformedJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(string) string
		want   string
	}{
		{
			name: "unknown entry field",
			mutate: func(contents string) string {
				return strings.Replace(contents, `"status": "accepted",`, `"status": "accepted", "unknown": true,`, 1)
			},
			want: "unknown field",
		},
		{
			name: "malformed JSON",
			mutate: func(contents string) string {
				return strings.TrimSuffix(strings.TrimSpace(contents), "}")
			},
			want: "unexpected EOF",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := copyDependencyFixture(t, "valid")
			manifestPath := filepath.Join(root, "docs", "dependencies", "reviews.json")
			contents, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read review manifest: %v", err)
			}
			if err := os.WriteFile(manifestPath, []byte(test.mutate(string(contents))), 0o644); err != nil {
				t.Fatalf("write hostile review manifest: %v", err)
			}
			_, err = auditDependencyFixture(root)
			assertAuditErrorContains(t, err, "decode dependency reviews", test.want)
		})
	}
}

func TestDependencyReviewFileRejectsIntermediateSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := copyDependencyFixture(t, "valid")
	outside := filepath.Join(root, "outside-reviews")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("create outside review directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "review.md"), []byte("# Outside\n"), 0o644); err != nil {
		t.Fatalf("create outside review file: %v", err)
	}
	link := filepath.Join(root, "docs", "dependencies", "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create escaping review symlink: %v", err)
	}
	manifestPath := filepath.Join(root, "docs", "dependencies", "reviews.json")
	manifest, err := readDependencyReviewManifest(manifestPath)
	if err != nil {
		t.Fatalf("read dependency review manifest: %v", err)
	}
	manifest.Reviews[0].ReviewFile = "escape/review.md"
	writeDependencyReviewManifest(t, manifestPath, manifest)

	findings, err := auditDependencyFixture(root)
	if err != nil {
		t.Fatalf("Audit(review symlink escape): %v", err)
	}
	assertAuditFinding(t, findings, "malformed-review", "example.com/testhelper", "")
}

func TestTransitiveDisclosuresExactlyMatchNonReachableGoSumModules(t *testing.T) {
	t.Parallel()

	const (
		modulePath = "example.com/upstream-test"
		version    = "v1.2.3"
		goSum      = "example.com/upstream-test v1.2.3 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
			"example.com/upstream-test v1.2.3/go.mod h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n"
	)
	t.Run("missing deduplicated disclosure", func(t *testing.T) {
		t.Parallel()

		root := copyDependencyFixture(t, "valid")
		if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte(goSum), 0o644); err != nil {
			t.Fatalf("write go.sum: %v", err)
		}
		findings, err := auditDependencyFixture(root)
		if err != nil {
			t.Fatalf("Audit(missing disclosure): %v", err)
		}
		assertAuditFinding(t, findings, "missing-disclosure", modulePath, "")
		count := 0
		for _, finding := range findings {
			if finding.Kind == "missing-disclosure" && finding.Module == modulePath {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("module and /go.mod sums produced %d missing disclosures, want 1", count)
		}
	})

	t.Run("exact disclosure accepted", func(t *testing.T) {
		t.Parallel()

		root := copyDependencyFixture(t, "valid")
		if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte(goSum), 0o644); err != nil {
			t.Fatalf("write go.sum: %v", err)
		}
		manifestPath := filepath.Join(root, "docs", "dependencies", "reviews.json")
		manifest, err := readDependencyReviewManifest(manifestPath)
		if err != nil {
			t.Fatalf("read dependency reviews: %v", err)
		}
		manifest.TransitiveDisclosures = []transitiveDisclosure{{
			Module: modulePath, Version: version, Relationship: "upstream-test-only", ReviewFile: "fixture-review.md",
		}}
		writeDependencyReviewManifest(t, manifestPath, manifest)
		findings, err := auditDependencyFixture(root)
		if err != nil {
			t.Fatalf("Audit(exact disclosure): %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("exact disclosure produced findings:\n%s", formatAuditFindings(findings))
		}
	})

	t.Run("extra stale disclosure rejected", func(t *testing.T) {
		t.Parallel()

		root := copyDependencyFixture(t, "valid")
		manifestPath := filepath.Join(root, "docs", "dependencies", "reviews.json")
		manifest, err := readDependencyReviewManifest(manifestPath)
		if err != nil {
			t.Fatalf("read dependency reviews: %v", err)
		}
		manifest.TransitiveDisclosures = []transitiveDisclosure{{
			Module: modulePath, Version: version, Relationship: "upstream-test-only", ReviewFile: "fixture-review.md",
		}}
		writeDependencyReviewManifest(t, manifestPath, manifest)
		findings, err := auditDependencyFixture(root)
		if err != nil {
			t.Fatalf("Audit(extra disclosure): %v", err)
		}
		assertAuditFinding(t, findings, "extra-disclosure", modulePath, "")
	})
}

func TestAuditRejectsEveryDependencyPolicyViolation(t *testing.T) {
	t.Parallel()

	got, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:               filepath.Join("testdata", "invalid"),
		GateStatusPath:           filepath.Join("testdata", "invalid", "docs", "gate-status.json"),
		allowFixtureReplacements: true,
	})
	if err != nil {
		t.Fatalf("Audit(invalid): %v", err)
	}

	assertAuditFinding(t, got, "unapproved-direct-runtime", "example.com/rogue", "")
	assertAuditFinding(t, got, "forbidden-license", "github.com/coder/websocket", "")
	assertAuditFinding(t, got, "wrong-package", "github.com/coder/websocket", "example.com/depsinvalid/internal/fanout")
	assertAuditFinding(t, got, "dependency-before-gate", "github.com/nats-io/nats.go", "")
}

func TestAuditRejectsDirectRuntimeImportDespiteIndirectRequireMarker(t *testing.T) {
	t.Parallel()

	got, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:               filepath.Join("testdata", "indirect-label"),
		GateStatusPath:           filepath.Join("testdata", "indirect-label", "docs", "gate-status.json"),
		allowFixtureReplacements: true,
	})
	if err != nil {
		t.Fatalf("Audit(indirect-label): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Audit(indirect-label) findings = %d, want 1:\n%s", len(got), formatAuditFindings(got))
	}
	assertAuditFinding(t, got, "unapproved-direct-runtime", "example.com/rogue", "")
}

func TestAuditFailsClosedWhenTestPackageGraphCannotLoad(t *testing.T) {
	t.Parallel()

	_, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:               filepath.Join("testdata", "broken-test-graph"),
		GateStatusPath:           filepath.Join("testdata", "broken-test-graph", "docs", "gate-status.json"),
		allowFixtureReplacements: true,
	})
	assertAuditErrorContains(t, err, "load test package graph", "internal/missing")
}

func TestAuditNeverFallsBackFromAnAuthoritativeVendorManifest(t *testing.T) {
	t.Parallel()

	_, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:               filepath.Join("testdata", "broken-vendor"),
		GateStatusPath:           filepath.Join("testdata", "broken-vendor", "docs", "gate-status.json"),
		allowFixtureReplacements: true,
	})
	assertAuditErrorContains(t, err, "load runtime package graph", "inconsistent vendoring")
}

func TestAuditRejectsMismatchedVendoredModuleVersion(t *testing.T) {
	t.Parallel()

	_, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:               filepath.Join("testdata", "mismatched-version"),
		GateStatusPath:           filepath.Join("testdata", "mismatched-version", "docs", "gate-status.json"),
		allowFixtureReplacements: true,
	})
	assertAuditErrorContains(t, err, "load runtime package graph", "v1.0.0", "v1.0.1")
}

func TestVendorVersionFindingsCoverRuntimeAndTestReachabilityExactly(t *testing.T) {
	t.Parallel()

	graph := moduleGraph{
		Modules: map[string]moduleInfo{
			"example.com/runtime":     {Path: "example.com/runtime", Version: "v1.2.3"},
			"example.com/testonly":    {Path: "example.com/testonly", Version: "v2.3.4"},
			"example.com/unreachable": {Path: "example.com/unreachable", Version: "v3.4.5"},
		},
		RuntimeModules: map[string]bool{"example.com/runtime": true},
		TestModules:    map[string]bool{"example.com/testonly": true},
	}
	manifest := map[string]string{
		"example.com/runtime":     "v1.2.4",
		"example.com/testonly":    "v2.3.5",
		"example.com/unreachable": "v9.9.9",
	}

	got := vendorVersionFindings(graph, manifest, "vendor/modules.txt")
	if len(got) != 2 {
		t.Fatalf("vendorVersionFindings returned %d findings, want 2:\n%s", len(got), formatAuditFindings(got))
	}
	assertAuditFinding(t, got, "vendor-version-mismatch", "example.com/runtime", "")
	assertAuditFinding(t, got, "vendor-version-mismatch", "example.com/testonly", "")
	for _, finding := range got {
		if strings.Contains(finding.Message, "v9.9.9") {
			t.Errorf("unreachable module affected version audit: %#v", finding)
		}
	}
}

func TestEnsureModuleKeepsTheSelectedGraphVersion(t *testing.T) {
	t.Parallel()

	modules := map[string]moduleInfo{
		"example.com/dependency": {
			Path: "example.com/dependency", Version: "v1.0.0", Indirect: true,
		},
	}
	ensureModule(modules, moduleInfo{
		Path: "example.com/dependency", Version: "v1.1.0",
	})

	got := modules["example.com/dependency"]
	if got.Version != "v1.1.0" {
		t.Errorf("selected graph version = %q, want v1.1.0", got.Version)
	}
	if !got.Indirect {
		t.Error("go.mod indirect classification was not preserved")
	}
}

func TestAuditReportsMissingVendorManifest(t *testing.T) {
	t.Parallel()

	got, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:               filepath.Join("testdata", "missing-vendor"),
		GateStatusPath:           filepath.Join("testdata", "missing-vendor", "docs", "gate-status.json"),
		allowFixtureReplacements: true,
	})
	if err != nil {
		t.Fatalf("Audit(missing-vendor): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Audit(missing-vendor) findings = %d, want 1:\n%s", len(got), formatAuditFindings(got))
	}
	assertAuditFinding(t, got, "missing-vendor", "github.com/coder/websocket", "")
}

func TestAuditUsesVendorWithAnEmptyOfflineModuleCache(t *testing.T) {
	tests := []struct {
		name                     string
		root                     string
		gatePath                 string
		allowFixtureReplacements bool
	}{
		{
			name:                     "fixture",
			root:                     filepath.Join("testdata", "valid"),
			gatePath:                 filepath.Join("testdata", "valid", "docs", "gate-status.json"),
			allowFixtureReplacements: true,
		},
		{
			name:     "real repository",
			root:     filepath.Clean(filepath.Join("..", "..")),
			gatePath: filepath.Clean(filepath.Join("..", "..", "docs", "gate-status.json")),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOMODCACHE", t.TempDir())
			t.Setenv("GOPROXY", "off")
			t.Setenv("GOSUMDB", "off")
			t.Setenv("GOTOOLCHAIN", "local")

			got, err := Audit(context.Background(), AuditOptions{
				ModuleRoot:               test.root,
				GateStatusPath:           test.gatePath,
				allowFixtureReplacements: test.allowFixtureReplacements,
			})
			if err != nil {
				t.Fatalf("Audit(%s) with an empty offline module cache: %v", test.name, err)
			}
			if len(got) != 0 {
				t.Fatalf("Audit(%s) returned findings:\n%s", test.name, formatAuditFindings(got))
			}
		})
	}
}

func TestRunRejectsReplaceDirectivesWithoutATestOverride(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	code := Run(
		context.Background(),
		[]string{"-root", filepath.Join("testdata", "invalid")},
		io.Discard,
		&stderr,
	)
	if code != 2 {
		t.Fatalf("Run exit code = %d, want 2; stderr:\n%s", code, stderr.String())
	}
	for _, text := range []string{"go.mod", "replace directive", "forbid"} {
		if !strings.Contains(stderr.String(), text) {
			t.Errorf("stderr does not contain %q:\n%s", text, stderr.String())
		}
	}
}

func TestUNIT021_RealRepositoryDependencySupplyChainAuditPassesOffline(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOTOOLCHAIN", "local")

	got, err := AuditRepository(context.Background(), filepath.Clean(filepath.Join("..", "..")))
	if err != nil {
		t.Fatalf("AuditRepository: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("current repository has dependency findings:\n%s", formatAuditFindings(got))
	}
}

func auditDependencyFixture(root string) ([]Finding, error) {
	return Audit(context.Background(), AuditOptions{
		ModuleRoot:               root,
		GateStatusPath:           filepath.Join(root, "docs", "gate-status.json"),
		allowFixtureReplacements: true,
	})
}

func copyDependencyFixture(t *testing.T, name string) string {
	t.Helper()

	source := filepath.Join("testdata", name)
	destination := filepath.Join(t.TempDir(), name)
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy dependency fixture: %v", err)
	}
	return destination
}

func writeDependencyReviewManifest(t *testing.T, path string, manifest dependencyReviewManifest) {
	t.Helper()

	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("encode dependency review manifest: %v", err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write dependency review manifest: %v", err)
	}
}

func sortPolicies(policies []DependencyPolicy) {
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Module < policies[j].Module
	})
}

func assertAuditFinding(t *testing.T, findings []Finding, kind, module, packagePath string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Kind != kind || finding.Module != module {
			continue
		}
		if packagePath != "" && finding.Package != packagePath {
			continue
		}
		if finding.Message == "" {
			t.Fatalf("finding %#v has no actionable message", finding)
		}
		return
	}
	t.Fatalf("missing finding kind=%q module=%q package=%q\ngot:\n%s", kind, module, packagePath, formatAuditFindings(findings))
}

func assertAuditErrorContains(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("Audit returned nil error, want a fail-closed graph error")
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("Audit error does not contain %q: %v", fragment, err)
		}
	}
}

func formatAuditFindings(findings []Finding) string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, finding.Kind+" "+finding.Module+" "+finding.Package+" "+finding.Path+": "+finding.Message)
	}
	return strings.Join(lines, "\n")
}
