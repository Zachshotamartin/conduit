package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const fixtureModule = "example.com/conduitfixture"

const (
	reasonWebSocket = "coder/websocket must be imported only by internal/transport"
	reasonGraphQL   = "gqlparser/v2 must be imported only by internal/graphql/ast"
	reasonNATS      = "nats.go must be imported only by internal/bus/nats"
	reasonPGX       = "pgx/v5 must be imported only by internal/datasource/postgres"
	reasonIntake    = "transport and protocol must pass operations upward instead of importing internal/graphql"
	reasonResults   = "internal/graphql must emit results instead of importing transport, protocol, or queue"
	reasonPlatform  = "build tags and runtime.GOOS checks must be confined to internal/platform"
	reasonAdmin     = "client transport and protocol must not depend on internal/admin"
	reasonPorts     = "consumers must import ports, never adapter implementations"
	reasonTelemetry = "OTel and Prometheus SDKs must be confined to internal/observability and cmd/conduit"
	reasonTestCode  = "internal packages must not import test packages"
	reasonClock     = "domain packages must use injected clocks and seeded randomness"
	reasonLoadgen   = "conduit-loadgen is a client and must not import registry, fanout, or queue"
	reasonProtocol  = "internal/protocol must not import internal/datasource"
	reasonBus       = "internal/queue and internal/registry must not import internal/bus"
	reasonAdminSide = "internal/admin must not import internal/transport because the admin listener stack is separate"
	reasonRedaction = "every declared sink owner must directly import internal/observability/redaction"
)

type expectedViolation struct {
	rule   string
	pkg    string
	target string
	reason string
}

func TestCheckModuleReportsEveryArchitectureBoundary(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "module")
	got, err := CheckModule(context.Background(), root)
	if err != nil {
		t.Fatalf("CheckModule(%q): %v", root, err)
	}

	want := []expectedViolation{
		{rule: "ARCH-01", pkg: "internal/fanout", target: "github.com/coder/websocket", reason: reasonWebSocket},
		{rule: "ARCH-02", pkg: "internal/fanout", target: "github.com/vektah/gqlparser/v2", reason: reasonGraphQL},
		{rule: "ARCH-03", pkg: "internal/fanout", target: "github.com/nats-io/nats.go", reason: reasonNATS},
		{rule: "ARCH-04", pkg: "internal/fanout", target: "github.com/jackc/pgx/v5", reason: reasonPGX},
		{rule: "ARCH-05", pkg: "internal/protocol", target: "internal/graphql/ast", reason: reasonIntake},
		{rule: "ARCH-05", pkg: "internal/transport", target: "internal/graphql/ast", reason: reasonIntake},
		{rule: "ARCH-06", pkg: "internal/graphql/executor", target: "internal/protocol", reason: reasonResults},
		{rule: "ARCH-06", pkg: "internal/graphql/executor", target: "internal/queue", reason: reasonResults},
		{rule: "ARCH-06", pkg: "internal/graphql/executor", target: "internal/transport", reason: reasonResults},
		{rule: "ARCH-07", pkg: "internal/fanout", target: "//go:build", reason: reasonPlatform},
		{rule: "ARCH-07", pkg: "internal/fanout", target: "runtime.GOOS", reason: reasonPlatform},
		{rule: "ARCH-08", pkg: "internal/protocol", target: "internal/admin", reason: reasonAdmin},
		{rule: "ARCH-08", pkg: "internal/transport", target: "internal/admin", reason: reasonAdmin},
		{rule: "ARCH-09", pkg: "internal/graphql/executor", target: "internal/datasource/postgres", reason: reasonPorts},
		{rule: "ARCH-10", pkg: "internal/fanout", target: "github.com/prometheus/client_golang/prometheus", reason: reasonTelemetry},
		{rule: "ARCH-10", pkg: "internal/fanout", target: "go.opentelemetry.io/otel", reason: reasonTelemetry},
		{rule: "ARCH-11", pkg: "internal/fanout", target: "test/fixtures", reason: reasonTestCode},
		{rule: "ARCH-12", pkg: "internal/fanout", target: "math/rand.Int", reason: reasonClock},
		{rule: "ARCH-12", pkg: "internal/fanout", target: "time.After", reason: reasonClock},
		{rule: "ARCH-12", pkg: "internal/fanout", target: "time.Now", reason: reasonClock},
		{rule: "ARCH-13", pkg: "cmd/conduit-loadgen", target: "internal/fanout", reason: reasonLoadgen},
		{rule: "ARCH-13", pkg: "cmd/conduit-loadgen", target: "internal/queue", reason: reasonLoadgen},
		{rule: "ARCH-13", pkg: "cmd/conduit-loadgen", target: "internal/registry", reason: reasonLoadgen},
		{rule: "ARCH-14", pkg: "internal/protocol", target: "internal/datasource", reason: reasonProtocol},
		{rule: "ARCH-15", pkg: "internal/queue", target: "internal/bus", reason: reasonBus},
		{rule: "ARCH-15", pkg: "internal/registry", target: "internal/bus", reason: reasonBus},
		{rule: "ARCH-16", pkg: "internal/admin", target: "internal/transport/client", reason: reasonAdminSide},
	}

	normalized := normalizeViolations(got)
	sortExpected(want)
	if len(normalized) != len(want) {
		t.Fatalf("got %d violations, want %d\ngot:\n%s", len(normalized), len(want), formatExpected(normalized))
	}
	for i := range want {
		if normalized[i] != want[i] {
			t.Errorf("violation[%d] = %#v, want %#v", i, normalized[i], want[i])
		}
	}
}

func TestEveryDeclaredArchitectureRuleHasFixtureCoverage(t *testing.T) {
	t.Parallel()

	violations, err := CheckModule(context.Background(), filepath.Join("testdata", "module"))
	if err != nil {
		t.Fatalf("CheckModule: %v", err)
	}
	redactionRules := DefaultRules()
	for index := range redactionRules {
		if redactionRules[index].ID == "ARCH-17" {
			redactionRules[index] = redactionRule([]string{"internal/sink"})
		}
	}
	redactionViolations, err := checkModuleWithRules(context.Background(), filepath.Join("testdata", "module"), redactionRules)
	if err != nil {
		t.Fatalf("checkModuleWithRules(redaction hostile fixture): %v", err)
	}
	violations = append(violations, redactionViolations...)

	rules := DefaultRules()
	declared := make(map[string]Rule, len(rules))
	for _, rule := range rules {
		if rule.ID == "" || rule.Reason == "" {
			t.Errorf("declared rule has an empty ID or reason: %#v", rule)
		}
		if _, duplicate := declared[rule.ID]; duplicate {
			t.Errorf("architecture rule %q is declared more than once", rule.ID)
		}
		declared[rule.ID] = rule
	}

	covered := make(map[string]bool, len(rules))
	for _, violation := range violations {
		rule, ok := declared[violation.Rule]
		if !ok {
			t.Errorf("fixture produced undeclared architecture rule %q", violation.Rule)
			continue
		}
		if violation.Reason != rule.Reason {
			t.Errorf("fixture violation for %q uses reason %q, want declared reason %q", violation.Rule, violation.Reason, rule.Reason)
		}
		covered[violation.Rule] = true
	}
	for _, rule := range rules {
		if !covered[rule.ID] {
			t.Errorf("architecture rule %q has no active violation fixture", rule.ID)
		}
	}
}

func TestRuleConfigurationDrivesEnforcement(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "module")
	tests := []struct {
		name       string
		rule       Rule
		wantTarget string
	}{
		{
			name:       "MayImport confines a target",
			rule:       Rule{ID: "TEST-MAY", Package: "internal/transport/**", MayImport: []string{"github.com/coder/websocket/**"}, Reason: "test"},
			wantTarget: "github.com/coder/websocket",
		},
		{
			name:       "MustNotImport denies a target",
			rule:       Rule{ID: "TEST-DENY", Package: "internal/fanout", MustNotImport: []string{"github.com/coder/websocket/**"}, Reason: "test"},
			wantTarget: "github.com/coder/websocket",
		},
		{
			name:       "MustImport requires a direct edge",
			rule:       Rule{ID: "TEST-REQUIRE", Package: "internal/sink", MustImport: []string{"internal/observability/redaction"}, Reason: "test"},
			wantTarget: "internal/observability/redaction",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := checkModuleWithRules(context.Background(), root, []Rule{test.rule})
			if err != nil {
				t.Fatalf("checkModuleWithRules: %v", err)
			}
			if len(got) != 1 || !strings.HasSuffix(got[0].Target, test.wantTarget) {
				t.Fatalf("violations = %#v, want one target ending in %q", got, test.wantTarget)
			}
		})
	}

	allowed := Rule{ID: "TEST-ALLOW", Package: "internal/sinkcompliant", MustImport: []string{"internal/observability/redaction"}, Reason: "test"}
	got, err := checkModuleWithRules(context.Background(), root, []Rule{allowed})
	if err != nil {
		t.Fatalf("checkModuleWithRules(compliant owner): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("compliant owner produced violations: %#v", got)
	}
}

func TestSinkOwnerInventoryIsExactAndFailsClosed(t *testing.T) {
	t.Parallel()

	inventory, err := parseSinkOwnerInventory(sinkOwnerInventoryJSON)
	if err != nil {
		t.Fatalf("checked-in inventory: %v", err)
	}
	if len(inventory.Owners) != 0 {
		t.Fatalf("R0 sink-owner inventory = %v, want honestly empty", inventory.Owners)
	}
	wantCandidates := []string{"internal/admin", "internal/graphql/executor", "internal/observability"}
	if strings.Join(inventory.Candidates, ",") != strings.Join(wantCandidates, ",") {
		t.Fatalf("sink-owner candidates = %v, want normative set %v", inventory.Candidates, wantCandidates)
	}

	invalid := map[string]string{
		"malformed":           `{`,
		"wrong schema":        `{"schema_version":2,"candidates":["internal/a"],"owners":[]}`,
		"missing candidates":  `{"schema_version":1,"owners":[]}`,
		"null candidates":     `{"schema_version":1,"candidates":null,"owners":[]}`,
		"empty candidates":    `{"schema_version":1,"candidates":[],"owners":[]}`,
		"missing owners":      `{"schema_version":1,"candidates":["internal/a"]}`,
		"null owners":         `{"schema_version":1,"candidates":["internal/a"],"owners":null}`,
		"unknown field":       `{"schema_version":1,"candidates":["internal/a"],"owners":[],"extra":true}`,
		"duplicate candidate": `{"schema_version":1,"candidates":["internal/a","internal/a"],"owners":[]}`,
		"unsorted candidates": `{"schema_version":1,"candidates":["internal/z","internal/a"],"owners":[]}`,
		"wildcard candidate":  `{"schema_version":1,"candidates":["internal/a/*"],"owners":[]}`,
		"external candidate":  `{"schema_version":1,"candidates":["cmd/conduit"],"owners":[]}`,
		"duplicate owner":     `{"schema_version":1,"candidates":["internal/a"],"owners":["internal/a","internal/a"]}`,
		"owner not candidate": `{"schema_version":1,"candidates":["internal/a"],"owners":["internal/b"]}`,
		"trailing value":      `{"schema_version":1,"candidates":["internal/a"],"owners":[]} {}`,
	}
	for name, document := range invalid {
		name, document := name, document
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseSinkOwnerInventory([]byte(document)); err == nil {
				t.Fatalf("parseSinkOwnerInventory(%s) succeeded, want failure", document)
			}
		})
	}
}

func TestSinkOwnerCandidateCompleteness(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "module")
	tests := []struct {
		name      string
		inventory sinkOwnerInventory
		wantError string
		wantRule  string
	}{
		{
			name:      "active candidate omitted",
			inventory: sinkOwnerInventory{SchemaVersion: 1, Candidates: []string{"internal/sink"}, Owners: []string{}},
			wantError: "active candidate \"internal/sink\" is omitted from owners",
		},
		{
			name:      "unknown candidate",
			inventory: sinkOwnerInventory{SchemaVersion: 1, Candidates: []string{"internal/missing-sink"}, Owners: []string{}},
			wantError: "candidate \"internal/missing-sink\" does not exist",
		},
		{
			name:      "inactive owner is stale",
			inventory: sinkOwnerInventory{SchemaVersion: 1, Candidates: []string{"internal/admin"}, Owners: []string{"internal/admin"}},
			wantError: "owner \"internal/admin\" is doc.go-only",
		},
		{
			name:      "active owner missing redaction",
			inventory: sinkOwnerInventory{SchemaVersion: 1, Candidates: []string{"internal/sink"}, Owners: []string{"internal/sink"}},
			wantRule:  "ARCH-17",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rules := []Rule{redactionRule(test.inventory.Owners)}
			got, err := checkModuleWithPolicy(context.Background(), root, rules, &test.inventory)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("checkModuleWithPolicy: %v", err)
			}
			if len(got) != 1 || got[0].Rule != test.wantRule {
				t.Fatalf("violations = %#v, want one %s violation", got, test.wantRule)
			}
		})
	}

	inactive := sinkOwnerInventory{SchemaVersion: 1, Candidates: []string{"internal/admin"}, Owners: []string{}}
	got, err := checkModuleWithPolicy(context.Background(), root, []Rule{redactionRule(nil)}, &inactive)
	if err != nil {
		t.Fatalf("doc.go-only candidate: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("doc.go-only candidate produced violations: %#v", got)
	}

	compliant := sinkOwnerInventory{SchemaVersion: 1, Candidates: []string{"internal/sinkcompliant"}, Owners: []string{"internal/sinkcompliant"}}
	got, err = checkModuleWithPolicy(context.Background(), root, []Rule{redactionRule(compliant.Owners)}, &compliant)
	if err != nil {
		t.Fatalf("compliant active owner: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("compliant active owner produced violations: %#v", got)
	}
}

func TestMissingSinkOwnerPackageFailsClosed(t *testing.T) {
	t.Parallel()

	_, err := checkModuleWithRules(
		context.Background(),
		filepath.Join("testdata", "module"),
		[]Rule{redactionRule([]string{"internal/missing-sink"})},
	)
	if err == nil || !strings.Contains(err.Error(), "does not exist in the module graph") {
		t.Fatalf("missing owner error = %v, want fail-closed inventory error", err)
	}
}

func TestMalformedRuleConfigurationFailsClosed(t *testing.T) {
	t.Parallel()

	valid := Rule{ID: "TEST", Package: "internal/sink", MustNotImport: []string{"internal/queue"}, Reason: "test"}
	tests := []struct {
		name  string
		rules []Rule
	}{
		{name: "duplicate ID", rules: []Rule{valid, valid}},
		{name: "malformed selector", rules: []Rule{{ID: "TEST", Package: "internal/{sink", MustNotImport: []string{"internal/queue"}, Reason: "test"}}},
		{name: "negative-only selector", rules: []Rule{{ID: "TEST", Package: "!internal/sink", MustNotImport: []string{"internal/queue"}, Reason: "test"}}},
		{name: "no targets", rules: []Rule{{ID: "TEST", Package: "internal/sink", Reason: "test"}}},
		{name: "wildcard required owner", rules: []Rule{{ID: "TEST", Package: "internal/sink/**", MustImport: []string{"internal/observability/redaction"}, Reason: "test"}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := checkModuleWithRules(context.Background(), filepath.Join("testdata", "module"), test.rules); err == nil {
				t.Fatalf("checkModuleWithRules accepted malformed rules: %#v", test.rules)
			}
		})
	}
}

func TestTestOnlyImportDoesNotSatisfySinkOwnerRule(t *testing.T) {
	t.Parallel()

	inventory := sinkOwnerInventory{SchemaVersion: 1, Candidates: []string{"internal/sinktestonly"}, Owners: []string{"internal/sinktestonly"}}
	rule := redactionRule(inventory.Owners)
	got, err := checkModuleWithPolicy(context.Background(), filepath.Join("testdata", "module"), []Rule{rule}, &inventory)
	if err != nil {
		t.Fatalf("checkModuleWithRules: %v", err)
	}
	if len(got) != 1 || got[0].Rule != "ARCH-17" || got[0].Reason != reasonRedaction {
		t.Fatalf("test-only redaction import violations = %#v, want one ARCH-17 violation", got)
	}
}

func TestRunFailsAndPrintsEveryRuleReason(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	code := Run(
		context.Background(),
		[]string{"-root", filepath.Join("testdata", "module")},
		io.Discard,
		&stderr,
	)
	if code != 1 {
		t.Fatalf("Run exit code = %d, want 1; stderr:\n%s", code, stderr.String())
	}

	for _, reason := range []string{
		reasonWebSocket,
		reasonGraphQL,
		reasonNATS,
		reasonPGX,
		reasonIntake,
		reasonResults,
		reasonPlatform,
		reasonAdmin,
		reasonPorts,
		reasonTelemetry,
		reasonTestCode,
		reasonClock,
		reasonLoadgen,
	} {
		if !strings.Contains(stderr.String(), reason) {
			t.Errorf("stderr does not name rule reason %q\nstderr:\n%s", reason, stderr.String())
		}
	}
}

func TestCheckModuleUsesGoPackageMetadataNotForbiddenText(t *testing.T) {
	t.Parallel()

	got, err := CheckModule(context.Background(), filepath.Join("testdata", "module"))
	if err != nil {
		t.Fatalf("CheckModule: %v", err)
	}
	for _, violation := range got {
		if strings.HasSuffix(violation.Package, "/internal/safe") {
			t.Fatalf("comment and string decoys produced a violation: %#v", violation)
		}
	}
}

func TestCheckModuleAllowsEveryDocumentedException(t *testing.T) {
	t.Parallel()

	got, err := CheckModule(context.Background(), filepath.Join("testdata", "module"))
	if err != nil {
		t.Fatalf("CheckModule: %v", err)
	}
	allowed := []string{
		"/cmd/conduit",
		"/internal/bus/nats",
		"/internal/clock",
		"/internal/datasource/postgres",
		"/internal/graphql/ast",
		"/internal/observability",
		"/internal/platform",
		"/test/conformance",
	}
	for _, violation := range got {
		for _, suffix := range allowed {
			if strings.HasSuffix(violation.Package, suffix) {
				t.Errorf("allowed package %q produced a violation: %#v", suffix, violation)
			}
		}
	}
}

func TestUNIT019_RealRepositoryPasses(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	got, err := CheckModule(context.Background(), root)
	if err != nil {
		t.Fatalf("CheckModule(real repository): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("real repository has architecture violations:\n%s", formatExpected(normalizeViolations(got)))
	}

	var stderr bytes.Buffer
	if code := Run(context.Background(), []string{"-root", root}, io.Discard, &stderr); code != 0 {
		t.Fatalf("Run(real repository) exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
}

func TestCheckModuleUsesVendorWithAnEmptyOfflineModuleCache(t *testing.T) {
	moduleCache := t.TempDir()
	t.Setenv("GOMODCACHE", moduleCache)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")

	root := filepath.Join("testdata", "vendor-module")
	got, err := CheckModule(context.Background(), root)
	if err != nil {
		t.Fatalf("CheckModule(%q) with an empty offline module cache: %v", root, err)
	}
	if len(got) != 0 {
		t.Fatalf("vendored offline fixture has architecture violations:\n%s", formatExpected(normalizeViolations(got)))
	}
}

func normalizeViolations(violations []Violation) []expectedViolation {
	normalized := make([]expectedViolation, 0, len(violations))
	for _, violation := range violations {
		pkg := strings.TrimPrefix(violation.Package, fixtureModule+"/")
		target := strings.TrimPrefix(violation.Target, fixtureModule+"/")
		normalized = append(normalized, expectedViolation{
			rule:   violation.Rule,
			pkg:    pkg,
			target: target,
			reason: violation.Reason,
		})
	}
	sortExpected(normalized)
	return normalized
}

func sortExpected(violations []expectedViolation) {
	sort.Slice(violations, func(i, j int) bool {
		left := violations[i].rule + "\x00" + violations[i].pkg + "\x00" + violations[i].target
		right := violations[j].rule + "\x00" + violations[j].pkg + "\x00" + violations[j].target
		return left < right
	})
}

func formatExpected(violations []expectedViolation) string {
	var lines []string
	for _, violation := range violations {
		lines = append(lines, violation.rule+" "+violation.pkg+" -> "+violation.target+": "+violation.reason)
	}
	return strings.Join(lines, "\n")
}
