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

func TestRealRepositoryPasses(t *testing.T) {
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
