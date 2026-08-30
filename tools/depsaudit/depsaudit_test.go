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
	assertAuditFinding(t, got, "missing-vendor", "github.com/nats-io/nats.go", "")
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

func formatAuditFindings(findings []Finding) string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, finding.Kind+" "+finding.Module+" "+finding.Package+" "+finding.Path+": "+finding.Message)
	}
	return strings.Join(lines, "\n")
}
