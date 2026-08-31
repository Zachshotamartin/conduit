package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestExtractRequirementIDsUsesOnlyPRDSectionsSevenAndNine(t *testing.T) {
	t.Parallel()

	file, err := os.Open(filepath.Join("testdata", "repository", "docs", "PRODUCT_REQUIREMENTS.md"))
	if err != nil {
		t.Fatalf("Open fixture PRD: %v", err)
	}
	defer func() { _ = file.Close() }()

	got, err := ExtractRequirementIDs(file)
	if err != nil {
		t.Fatalf("ExtractRequirementIDs: %v", err)
	}
	want := []string{"FR-OPS-002", "NFR-MAINT-005"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requirement IDs = %v, want %v", got, want)
	}
}

func TestRealPRDRequirementInventoryIsExact(t *testing.T) {
	t.Parallel()

	file, err := os.Open(filepath.Join("..", "..", "docs", "PRODUCT_REQUIREMENTS.md"))
	if err != nil {
		t.Fatalf("Open real PRD: %v", err)
	}
	defer func() { _ = file.Close() }()

	got, err := ExtractRequirementIDs(file)
	if err != nil {
		t.Fatalf("ExtractRequirementIDs: %v", err)
	}
	want := expectedRequirementIDs()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("real PRD inventory drifted\ngot (%d): %v\nwant (%d): %v", len(got), got, len(want), want)
	}
}

func TestCheckAcceptsValidInProgressGateFixture(t *testing.T) {
	t.Parallel()

	got, err := Check(context.Background(), validOptions())
	if err != nil {
		t.Fatalf("Check(valid): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Check(valid) returned violations:\n%s", formatTraceViolations(got))
	}
}

func TestCheckRejectsInventedRequirementCitations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*CheckOptions)
		reference string
	}{
		{
			name: "Go test comment",
			configure: func(options *CheckOptions) {
				options.TestRoots = []string{filepath.Join(fixtureRoot(), "tests", "invented")}
			},
			reference: "FR-FAKE-999",
		},
		{
			name: "operations matrix row",
			configure: func(options *CheckOptions) {
				options.OperationsTestPlanPath = filepath.Join(fixtureRoot(), "docs", "OPERATIONS_TEST_PLAN_INVENTED.md")
			},
			reference: "NFR-FAKE-999",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			options := validOptions()
			tc.configure(&options)
			got, err := Check(context.Background(), options)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			assertTraceViolation(t, got, "invented-requirement", tc.reference)
		})
	}
}

func TestCheckRejectsOwnershipMirrorErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		file      string
		kind      string
		reference string
	}{
		{name: "terminal gate disagrees with BUILD_PLAN", file: "ownership-drift.json", kind: "ownership-drift", reference: "NFR-MAINT-005"},
		{name: "real requirement is missing", file: "ownership-missing.json", kind: "missing-ownership", reference: "NFR-MAINT-005"},
		{name: "mirror invents a requirement", file: "ownership-invented.json", kind: "invented-ownership", reference: "FR-FAKE-999"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			options := validOptions()
			options.OwnershipPath = filepath.Join(fixtureRoot(), "ownership", tc.file)
			got, err := Check(context.Background(), options)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			assertTraceViolation(t, got, tc.kind, tc.reference)
		})
	}
}

func TestCheckRequiresTestFunctionsWhenEarliestGateStarts(t *testing.T) {
	t.Parallel()

	options := validOptions()
	options.TestRoots = []string{filepath.Join(fixtureRoot(), "tests", "missing")}
	got, err := Check(context.Background(), options)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	assertTraceViolation(t, got, "missing-test-row", "UNIT-001")
	assertTraceViolation(t, got, "missing-test-row", "UNIT-016")
	assertNoTraceViolation(t, got, "missing-test-row", "UNIT-002")
}

func TestCheckRequiresEvidenceForRequirementsOwnedByAcceptedGates(t *testing.T) {
	t.Parallel()

	options := validOptions()
	options.GateStatusPath = filepath.Join(fixtureRoot(), "status", "r0-accepted.json")
	got, err := Check(context.Background(), options)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	assertTraceViolation(t, got, "missing-requirement-evidence", "NFR-MAINT-005")
	assertNoTraceViolation(t, got, "missing-requirement-evidence", "FR-OPS-002")
}

func TestRunNamesViolationsAndReturnsFailure(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	code := Run(
		context.Background(),
		[]string{"-root", filepath.Join("testdata", "repository"), "-tests", filepath.Join("testdata", "repository", "tests", "missing")},
		io.Discard,
		&stderr,
	)
	if code != 1 {
		t.Fatalf("Run exit code = %d, want 1; stderr:\n%s", code, stderr.String())
	}
	for _, row := range []string{"UNIT-001", "UNIT-016"} {
		if !strings.Contains(stderr.String(), row) {
			t.Errorf("stderr does not name %s:\n%s", row, stderr.String())
		}
	}
}

func TestCurrentRepositoryPasses(t *testing.T) {
	got, err := CheckRepository(context.Background(), filepath.Clean(filepath.Join("..", "..")))
	if err != nil {
		t.Fatalf("CheckRepository: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("current repository has traceability violations:\n%s", formatTraceViolations(got))
	}
}

func validOptions() CheckOptions {
	root := fixtureRoot()
	return CheckOptions{
		ProductRequirementsPath: filepath.Join(root, "docs", "PRODUCT_REQUIREMENTS.md"),
		BuildPlanPath:           filepath.Join(root, "docs", "BUILD_PLAN.md"),
		OperationsTestPlanPath:  filepath.Join(root, "docs", "OPERATIONS_TEST_PLAN.md"),
		GateStatusPath:          filepath.Join(root, "docs", "gate-status.json"),
		OwnershipPath:           filepath.Join(root, "docs", "requirement-ownership.json"),
		TestRoots:               []string{filepath.Join(root, "tests", "valid")},
	}
}

func fixtureRoot() string {
	return filepath.Join("testdata", "repository")
}

func expectedRequirementIDs() []string {
	counts := map[string]int{
		"FR-ADMIN":   8,
		"FR-AUTH":    18,
		"FR-CONN":    14,
		"FR-FAN":     12,
		"FR-FILT":    10,
		"FR-GQL":     15,
		"FR-OPS":     13,
		"FR-RESUME":  9,
		"FR-SUB":     12,
		"NFR-COMPAT": 6,
		"NFR-MAINT":  6,
		"NFR-PERF":   6,
		"NFR-SCALE":  6,
		"NFR-SEC":    10,
	}
	var ids []string
	for namespace, count := range counts {
		for number := 1; number <= count; number++ {
			ids = append(ids, fmt.Sprintf("%s-%03d", namespace, number))
		}
	}
	sort.Strings(ids)
	return ids
}

func assertTraceViolation(t *testing.T, violations []Violation, kind, reference string) {
	t.Helper()
	for _, violation := range violations {
		if violation.Kind == kind && violation.Reference == reference {
			if violation.Message == "" {
				t.Fatalf("violation %#v has no actionable message", violation)
			}
			return
		}
	}
	t.Fatalf("missing violation kind=%q reference=%q\ngot:\n%s", kind, reference, formatTraceViolations(violations))
}

func assertNoTraceViolation(t *testing.T, violations []Violation, kind, reference string) {
	t.Helper()
	for _, violation := range violations {
		if violation.Kind == kind && violation.Reference == reference {
			t.Fatalf("unexpected violation %#v\nall:\n%s", violation, formatTraceViolations(violations))
		}
	}
}

func formatTraceViolations(violations []Violation) string {
	lines := make([]string, 0, len(violations))
	for _, violation := range violations {
		lines = append(lines, violation.Kind+" "+violation.Reference+" "+violation.Path+": "+violation.Message)
	}
	return strings.Join(lines, "\n")
}
