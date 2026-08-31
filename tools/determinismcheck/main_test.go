package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRejectsAliasedAndDotImportedNondeterminism(t *testing.T) {
	t.Parallel()

	violations, err := Check(filepath.Join("testdata", "invalid"))
	if err != nil {
		t.Fatalf("Check(invalid): %v", err)
	}
	want := []string{
		"crypto/rand.Read",
		"math/rand.Int",
		"time.AfterFunc",
		"time.NewTicker",
		"time.Now",
		"time.Sleep",
	}
	for _, symbol := range want {
		if !hasSymbol(violations, symbol) {
			t.Errorf("missing violation for %s: %#v", symbol, violations)
		}
	}
}

func TestCheckAllowsSeededLocalRandomnessAndExplicitTimeValues(t *testing.T) {
	t.Parallel()

	violations, err := Check(filepath.Join("testdata", "valid"))
	if err != nil {
		t.Fatalf("Check(valid): %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("Check(valid) violations = %#v, want none", violations)
	}
}

func TestRunReportsViolationsAndUsageErrors(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"-root", filepath.Join("testdata", "invalid")}, &stdout, &stderr); code != 1 {
		t.Fatalf("Run(invalid) = %d, want 1; stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "time.Sleep") {
		t.Fatalf("Run(invalid) stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	stderr.Reset()
	if code := Run([]string{"unexpected"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run(unexpected) = %d, want 2", code)
	}
}

func TestUNIT022_CurrentRepositoryTestsAreDeterministic(t *testing.T) {
	violations, err := Check(filepath.Clean(filepath.Join("..", "..")))
	if err != nil {
		t.Fatalf("Check(repository): %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("repository determinism violations = %#v", violations)
	}
}

func hasSymbol(violations []Violation, symbol string) bool {
	for _, violation := range violations {
		if violation.Symbol == symbol && violation.Path != "" && violation.Line > 0 {
			return true
		}
	}
	return false
}
