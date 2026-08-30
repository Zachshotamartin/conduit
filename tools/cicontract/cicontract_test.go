package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestRunReportsSuccessAndSemanticFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		root       string
		wantCode   int
		wantOutput string
	}{
		{
			name:       "valid",
			root:       filepath.Join("testdata", "valid"),
			wantCode:   0,
			wantOutput: "15 protected contexts verified",
		},
		{
			name:       "invalid",
			root:       filepath.Join("testdata", "invalid"),
			wantCode:   1,
			wantOutput: "protection.context",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			got := Run([]string{"-root", test.root}, &stdout, &stderr)
			if got != test.wantCode {
				t.Fatalf("Run() = %d, want %d; stdout=%q stderr=%q", got, test.wantCode, stdout.String(), stderr.String())
			}
			combined := stdout.String() + stderr.String()
			if !strings.Contains(combined, test.wantOutput) {
				t.Fatalf("Run() output = %q, want substring %q", combined, test.wantOutput)
			}
		})
	}
}

func TestRunRejectsUnexpectedArguments(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	if got := Run([]string{"repository"}, &bytes.Buffer{}, &stderr); got != 2 {
		t.Fatalf("Run(unexpected argument) = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "unexpected arguments") {
		t.Fatalf("Run(unexpected argument) stderr = %q, want actionable usage error", stderr.String())
	}
}

func TestValidFixtureSatisfiesCIContract(t *testing.T) {
	t.Parallel()

	report, err := Check(filepath.Join("testdata", "valid"))
	if err != nil {
		t.Fatalf("Check(valid fixture): %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("valid fixture findings:\n%s", formatFindings(report.Findings))
	}
	if len(report.Contexts) != 15 {
		t.Fatalf("protected context count = %d, want exactly 15; macos-correctness must remain unprotected", len(report.Contexts))
	}

	want := map[string]string{
		"pr / lint":                          "pr/lint",
		"pr / vet":                           "pr/vet",
		"pr / arch-check":                    "pr/arch-check",
		"pr / unit-race":                     "pr/unit-race",
		"pr / proto-race":                    "pr/proto-race",
		"pr / authz-race":                    "pr/authz-race",
		"pr / index-race":                    "pr/index-race",
		"pr / docs-status-lint":              "pr/docs-status-lint",
		"pr / metrics-contract":              "pr/metrics-contract",
		"pr / deps-audit":                    "pr/deps-audit",
		"pr / trace-check":                   "pr/trace-check",
		"integration / conformance-node":     "integration/conformance-node",
		"integration / integration-nats":     "integration/integration-nats",
		"integration / integration-postgres": "integration/integration-postgres",
		"integration / socket-hostile":       "integration/socket-hostile",
	}
	got := make(map[string]string, len(report.Contexts))
	for _, mapping := range report.Contexts {
		if _, duplicate := got[mapping.Context]; duplicate {
			t.Fatalf("duplicate derived context mapping for %q", mapping.Context)
		}
		got[mapping.Context] = mapping.Workflow + "/" + mapping.Job
	}
	if diff := diffStringMap(got, want); diff != "" {
		t.Fatalf("required context mapping mismatch (-got +want):\n%s", diff)
	}
}

func TestRepositorySatisfiesCIContract(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	report, err := Check(repositoryRoot)
	if err != nil {
		t.Fatalf("Check(repository): %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("repository CI contract findings:\n%s", formatFindings(report.Findings))
	}
}

func TestInvalidFixtureProvesEveryContractClassCanFail(t *testing.T) {
	t.Parallel()

	report, err := Check(filepath.Join("testdata", "invalid"))
	if err != nil {
		t.Fatalf("Check(invalid fixture): %v", err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("invalid fixture passed; CI contract checker cannot prove failure")
	}

	wantCodes := []string{
		"artifact.retention",
		"job.command",
		"job.race",
		"job.timeout",
		"job.vendor-mode",
		"platform.macos",
		"protection.context",
		"workflow.jobs",
		"workflow.permissions",
		"workflow.trigger",
	}
	gotCodes := make(map[string]bool, len(report.Findings))
	for _, finding := range report.Findings {
		if finding.Code == "" || finding.File == "" || finding.Message == "" {
			t.Fatalf("finding lacks stable diagnostics: %#v", finding)
		}
		gotCodes[finding.Code] = true
	}
	for _, code := range wantCodes {
		if !gotCodes[code] {
			t.Errorf("invalid fixture did not produce %q; findings:\n%s", code, formatFindings(report.Findings))
		}
	}
}

func formatFindings(findings []Finding) string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		location := finding.File
		if finding.Path != "" {
			location += ":" + finding.Path
		}
		lines = append(lines, fmt.Sprintf("%s: %s: %s", finding.Code, location, finding.Message))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func diffStringMap(got, want map[string]string) string {
	keys := make(map[string]struct{}, len(got)+len(want))
	for key := range got {
		keys[key] = struct{}{}
	}
	for key := range want {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)

	var lines []string
	for _, key := range ordered {
		gotValue, gotOK := got[key]
		wantValue, wantOK := want[key]
		if gotOK && wantOK && gotValue == wantValue {
			continue
		}
		if gotOK {
			lines = append(lines, fmt.Sprintf("- %s=%s", key, gotValue))
		}
		if wantOK {
			lines = append(lines, fmt.Sprintf("+ %s=%s", key, wantValue))
		}
	}
	return strings.Join(lines, "\n")
}
