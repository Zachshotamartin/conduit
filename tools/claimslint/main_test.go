package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLintClaimsRejectsMarkerForUnacceptedGate(t *testing.T) {
	t.Parallel()

	violations, err := lintClaims(filepath.Join("testdata", "unearned"))
	if err != nil {
		t.Fatalf("lint claims: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one", violations)
	}
	if !strings.Contains(violations[0].Message, "R2") {
		t.Fatalf("message = %q, want R2", violations[0].Message)
	}
}

func TestLintClaimsAcceptsMarkerForAcceptedGate(t *testing.T) {
	t.Parallel()

	violations, err := lintClaims(filepath.Join("testdata", "earned"))
	if err != nil {
		t.Fatalf("lint claims: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %v, want none", violations)
	}
}

func TestLintClaimsRejectsRegisterDrift(t *testing.T) {
	t.Parallel()

	violations, err := lintClaims(filepath.Join("testdata", "drift"))
	if err != nil {
		t.Fatalf("lint claims: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one", violations)
	}
	if !strings.Contains(violations[0].Message, "unearned") {
		t.Fatalf("message = %q, want unearned", violations[0].Message)
	}
}

func TestLintClaimsScansDocumentationBeyondMarketingPlan(t *testing.T) {
	t.Parallel()

	violations, err := lintClaims(filepath.Join("testdata", "scope"))
	if err != nil {
		t.Fatalf("lint claims: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one", violations)
	}
	if !strings.HasSuffix(violations[0].Path, filepath.Join("docs", "extra.md")) {
		t.Fatalf("violation path = %q, want docs/extra.md", violations[0].Path)
	}
}

func TestLintClaimsRequiresRegisterEvidenceColumn(t *testing.T) {
	t.Parallel()

	violations, err := lintClaims(filepath.Join("testdata", "missing-evidence"))
	if err != nil {
		t.Fatalf("lint claims: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one", violations)
	}
	if !strings.Contains(violations[0].Message, "Evidence") {
		t.Fatalf("message = %q, want Evidence column", violations[0].Message)
	}
}

func TestCurrentClaimsPass(t *testing.T) {
	violations, err := lintClaims(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("lint claims: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("current claim violations: %v", violations)
	}
}
