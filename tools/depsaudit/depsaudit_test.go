package main

import (
	"bytes"
	"context"
	"io"
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
		ModuleRoot:     filepath.Join("testdata", "valid"),
		GateStatusPath: filepath.Join("testdata", "valid", "docs", "gate-status.json"),
	})
	if err != nil {
		t.Fatalf("Audit(valid): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Audit(valid) returned findings:\n%s", formatAuditFindings(got))
	}
}

func TestAuditRejectsEveryDependencyPolicyViolation(t *testing.T) {
	t.Parallel()

	got, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:     filepath.Join("testdata", "invalid"),
		GateStatusPath: filepath.Join("testdata", "invalid", "docs", "gate-status.json"),
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
		ModuleRoot:     filepath.Join("testdata", "indirect-label"),
		GateStatusPath: filepath.Join("testdata", "indirect-label", "docs", "gate-status.json"),
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
		ModuleRoot:     filepath.Join("testdata", "broken-test-graph"),
		GateStatusPath: filepath.Join("testdata", "broken-test-graph", "docs", "gate-status.json"),
	})
	assertAuditErrorContains(t, err, "load test package graph", "internal/missing")
}

func TestAuditNeverFallsBackFromAnAuthoritativeVendorManifest(t *testing.T) {
	t.Parallel()

	_, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:     filepath.Join("testdata", "broken-vendor"),
		GateStatusPath: filepath.Join("testdata", "broken-vendor", "docs", "gate-status.json"),
	})
	assertAuditErrorContains(t, err, "load runtime package graph", "inconsistent vendoring")
}

func TestAuditRejectsMismatchedVendoredModuleVersion(t *testing.T) {
	t.Parallel()

	_, err := Audit(context.Background(), AuditOptions{
		ModuleRoot:     filepath.Join("testdata", "mismatched-version"),
		GateStatusPath: filepath.Join("testdata", "mismatched-version", "docs", "gate-status.json"),
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
		ModuleRoot:     filepath.Join("testdata", "missing-vendor"),
		GateStatusPath: filepath.Join("testdata", "missing-vendor", "docs", "gate-status.json"),
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
		name     string
		root     string
		gatePath string
	}{
		{
			name:     "fixture",
			root:     filepath.Join("testdata", "valid"),
			gatePath: filepath.Join("testdata", "valid", "docs", "gate-status.json"),
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
				ModuleRoot:     test.root,
				GateStatusPath: test.gatePath,
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

func TestRunFailsAndNamesDependencyPolicyReasons(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	code := Run(
		context.Background(),
		[]string{"-root", filepath.Join("testdata", "invalid")},
		io.Discard,
		&stderr,
	)
	if code != 1 {
		t.Fatalf("Run exit code = %d, want 1; stderr:\n%s", code, stderr.String())
	}
	for _, text := range []string{"example.com/rogue", "GPL-3.0-only", "internal/transport", "R5", "vendor"} {
		if !strings.Contains(stderr.String(), text) {
			t.Errorf("stderr does not contain %q:\n%s", text, stderr.String())
		}
	}
}

func TestCurrentEmptyRuntimeTreePasses(t *testing.T) {
	got, err := AuditRepository(context.Background(), filepath.Clean(filepath.Join("..", "..")))
	if err != nil {
		t.Fatalf("AuditRepository: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("current repository has dependency findings:\n%s", formatAuditFindings(got))
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
