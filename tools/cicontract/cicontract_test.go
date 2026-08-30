package main

import (
	"bytes"
	"fmt"
	"os"
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
		t.Fatalf("protected context count = %d, want exactly 15; platform correctness jobs must remain unprotected", len(report.Contexts))
	}
	for _, mapping := range report.Contexts {
		if mapping.Job == "macos-correctness" || mapping.Job == "linux-arm64-correctness" {
			t.Fatalf("platform correctness job %q unexpectedly became a protected context", mapping.Job)
		}
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
		"action.pin",
		"artifact.retention",
		"job.command",
		"job.condition",
		"job.continue-on-error",
		"job.race",
		"job.timeout",
		"job.vendor-mode",
		"nightly.govulncheck",
		"platform.linux-arm64",
		"platform.macos",
		"protection.context",
		"step.condition",
		"step.continue-on-error",
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

func TestHostileWorkflowBypassesFailClosed(t *testing.T) {
	t.Parallel()

	const uploadPin = "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02"
	tests := []struct {
		name     string
		workflow string
		old      string
		new      string
		wantCode string
		wantPath string
	}{
		{
			name:     "job level condition",
			workflow: "pr",
			old:      "  lint:\n    name: lint\n    runs-on: ubuntu-latest",
			new:      "  lint:\n    name: lint\n    if: success()\n    runs-on: ubuntu-latest",
			wantCode: "job.condition",
			wantPath: "jobs.lint.if",
		},
		{
			name:     "job continue on error even false",
			workflow: "pr",
			old:      "  lint:\n    name: lint\n    runs-on: ubuntu-latest",
			new:      "  lint:\n    name: lint\n    continue-on-error: false\n    runs-on: ubuntu-latest",
			wantCode: "job.continue-on-error",
			wantPath: "jobs.lint.continue-on-error",
		},
		{
			name:     "ordinary step condition",
			workflow: "pr",
			old:      "      - run: GO=go ./scripts/check-format.sh",
			new:      "      - run: GO=go ./scripts/check-format.sh\n        if: success()",
			wantCode: "step.condition",
			wantPath: "jobs.lint.steps.0.if",
		},
		{
			name:     "step continue on error even false",
			workflow: "pr",
			old:      "      - run: GO=go ./scripts/check-format.sh",
			new:      "      - run: GO=go ./scripts/check-format.sh\n        continue-on-error: false",
			wantCode: "step.continue-on-error",
			wantPath: "jobs.lint.steps.0.continue-on-error",
		},
		{
			name:     "mutable known action",
			workflow: "pr",
			old:      "uses: " + uploadPin,
			new:      "uses: actions/upload-artifact@v4",
			wantCode: "action.pin",
			wantPath: "jobs.unit-race.steps.1.uses",
		},
		{
			name:     "unknown pinned action",
			workflow: "pr",
			old:      "uses: " + uploadPin,
			new:      "uses: attacker/example@0123456789abcdef0123456789abcdef01234567",
			wantCode: "action.pin",
			wantPath: "jobs.unit-race.steps.1.uses",
		},
		{
			name:     "job level reusable workflow",
			workflow: "pr",
			old:      "  lint:\n    name: lint\n    runs-on: ubuntu-latest",
			new:      "  lint:\n    name: lint\n    uses: attacker/example/.github/workflows/ci.yml@main\n    runs-on: ubuntu-latest",
			wantCode: "action.pin",
			wantPath: "jobs.lint.uses",
		},
		{
			name:     "narrowed unit race scope",
			workflow: "pr",
			old:      "      - run: go test -mod=vendor -race -shuffle=on ./...\n      - name: Retain failure evidence",
			new:      "      - run: go test -mod=vendor -race -shuffle=on ./internal/...\n      - name: Retain failure evidence",
			wantCode: "job.race",
			wantPath: "jobs.unit-race.steps",
		},
		{
			name:     "echoed unit race command",
			workflow: "pr",
			old:      "      - run: go test -mod=vendor -race -shuffle=on ./...\n      - name: Retain failure evidence",
			new:      "      - run: echo go test -mod=vendor -race -shuffle=on ./...\n      - name: Retain failure evidence",
			wantCode: "job.race",
			wantPath: "jobs.unit-race.steps",
		},
		{
			name:     "nightly scanner install omitted",
			workflow: "nightly",
			old:      "      - run: go install golang.org/x/vuln/cmd/govulncheck@v1.1.4\n",
			new:      "",
			wantCode: "nightly.govulncheck",
			wantPath: "jobs.fuzz.steps",
		},
		{
			name:     "nightly scanner scope narrowed",
			workflow: "nightly",
			old:      "      - run: '\"$(go env GOPATH)/bin/govulncheck\" ./...'",
			new:      "      - run: '\"$(go env GOPATH)/bin/govulncheck\" ./cmd/...'",
			wantCode: "nightly.govulncheck",
			wantPath: "jobs.fuzz.steps",
		},
		{
			name:     "linux arm job omitted",
			workflow: "pr",
			old:      "  linux-arm64-correctness:\n",
			new:      "  linux-arm64-disabled:\n",
			wantCode: "platform.linux-arm64",
			wantPath: "jobs.linux-arm64-correctness",
		},
		{
			name:     "linux arm runner substituted",
			workflow: "pr",
			old:      "    runs-on: ubuntu-24.04-arm",
			new:      "    runs-on: ubuntu-latest",
			wantCode: "platform.linux-arm64",
			wantPath: "jobs.linux-arm64-correctness",
		},
		{
			name:     "linux arm suite narrowed",
			workflow: "pr",
			old:      "    runs-on: ubuntu-24.04-arm\n    timeout-minutes: 15\n    steps:\n      - run: GO=go ./scripts/check-format.sh\n      - run: ./scripts/check-determinism.sh\n      - run: go vet -mod=vendor ./...\n      - run: go test -mod=vendor -race -shuffle=on ./...",
			new:      "    runs-on: ubuntu-24.04-arm\n    timeout-minutes: 15\n    steps:\n      - run: GO=go ./scripts/check-format.sh\n      - run: ./scripts/check-determinism.sh\n      - run: go vet -mod=vendor ./...\n      - run: go test -mod=vendor -race -shuffle=on ./internal/...",
			wantCode: "platform.linux-arm64",
			wantPath: "jobs.linux-arm64-correctness.steps",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := copyValidFixture(t)
			mutateWorkflow(t, root, test.workflow, test.old, test.new)
			report, err := Check(root)
			if err != nil {
				t.Fatalf("Check(hostile fixture): %v", err)
			}
			if !hasFinding(report.Findings, test.wantCode, test.wantPath) {
				t.Fatalf("hostile fixture did not report %s at %s; findings:\n%s", test.wantCode, test.wantPath, formatFindings(report.Findings))
			}
		})
	}
}

func TestApprovedActionPinsAreExact(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"actions/checkout@11d5960a326750d5838078e36cf38b85af677262":        true,
		"actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff":        true,
		"actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02": true,
	}
	if diff := diffBoolMap(allowedActionUses, want); diff != "" {
		t.Fatalf("approved action pins mismatch (-got +want):\n%s", diff)
	}
}

func copyValidFixture(t *testing.T) string {
	t.Helper()

	source := filepath.Join("testdata", "valid")
	destination := filepath.Join(t.TempDir(), "repository")
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
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
	if err != nil {
		t.Fatalf("copy valid fixture: %v", err)
	}
	return destination
}

func mutateWorkflow(t *testing.T, root, workflowName, old, replacement string) {
	t.Helper()

	path := filepath.Join(root, ".github", "workflows", workflowName+".yml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workflow fixture: %v", err)
	}
	if count := strings.Count(string(contents), old); count == 0 {
		t.Fatalf("mutation source does not occur in %s: %q", path, old)
	}
	updated := strings.Replace(string(contents), old, replacement, 1)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
}

func hasFinding(findings []Finding, code, path string) bool {
	for _, finding := range findings {
		if finding.Code == code && finding.Path == path {
			return true
		}
	}
	return false
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

func diffBoolMap(got, want map[string]bool) string {
	gotStrings := make(map[string]string, len(got))
	wantStrings := make(map[string]string, len(want))
	for key, value := range got {
		gotStrings[key] = fmt.Sprint(value)
	}
	for key, value := range want {
		wantStrings[key] = fmt.Sprint(value)
	}
	return diffStringMap(gotStrings, wantStrings)
}
