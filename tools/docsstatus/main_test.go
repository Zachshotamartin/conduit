package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintDocsRejectsForbiddenPhrase(t *testing.T) {
	t.Parallel()

	violations, err := lintDocs(filepath.Join("testdata", "forbidden", "docs"))
	if err != nil {
		t.Fatalf("lint docs: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one", violations)
	}
	if !strings.Contains(violations[0].Message, "forbidden phrase") {
		t.Fatalf("message = %q, want forbidden phrase", violations[0].Message)
	}
}

func TestLintDocsRejectsMissingStatus(t *testing.T) {
	t.Parallel()

	violations, err := lintDocs(filepath.Join("testdata", "unstatused", "docs"))
	if err != nil {
		t.Fatalf("lint docs: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one", violations)
	}
	if !strings.Contains(violations[0].Message, "status") {
		t.Fatalf("message = %q, want status", violations[0].Message)
	}
}

func TestLintDocsRejectsQuotedForbiddenPhraseInTemplate(t *testing.T) {
	t.Parallel()

	violations, err := lintDocs(filepath.Join("testdata", "quoted-template", "docs"))
	if err != nil {
		t.Fatalf("lint docs: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one", violations)
	}
	if !strings.Contains(violations[0].Message, "forbidden phrase") {
		t.Fatalf("message = %q, want forbidden phrase", violations[0].Message)
	}
}

func TestLintDocsRequiresAnExplicitStatusDeclaration(t *testing.T) {
	t.Parallel()

	violations, err := lintDocs(filepath.Join("testdata", "prose-status", "docs"))
	if err != nil {
		t.Fatalf("lint docs: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v, want one", violations)
	}
	if !strings.Contains(violations[0].Message, "status declaration") {
		t.Fatalf("message = %q, want explicit status declaration", violations[0].Message)
	}
}

func TestLintDocsAllowsMetaLanguageAndOpenQuestions(t *testing.T) {
	t.Parallel()

	violations, err := lintDocs(filepath.Join("testdata", "valid", "docs"))
	if err != nil {
		t.Fatalf("lint docs: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %v, want none", violations)
	}
}

func TestUNIT020_CurrentDocumentationPasses(t *testing.T) {
	violations, err := lintDocs(filepath.Join("..", "..", "docs"))
	if err != nil {
		t.Fatalf("lint docs: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("current documentation violations: %v", violations)
	}
}

func TestLintDocumentRejectsAmbiguousOrNonLifecycleStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
	}{
		{name: "negated", status: "Document status: not accepted."},
		{name: "multiple", status: "Document status: accepted | planned."},
		{name: "role instead of lifecycle", status: "Document status: normative specification."},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			violations := lintTemporaryDocument(t, "# Status fixture\n\n"+test.status+"\n")
			assertViolationMessageContains(t, violations, "status")
		})
	}
}

func TestLintDocumentDoesNotAcceptStatusInsideFence(t *testing.T) {
	t.Parallel()

	violations := lintTemporaryDocument(t, "# Fenced status\n\n```text\nDocument status: accepted.\n```\n")
	assertViolationMessageContains(t, violations, "status")
}

func TestLintDocumentRejectsForbiddenPhraseInsideFence(t *testing.T) {
	t.Parallel()

	content := "# Fenced placeholder\n\nDocument status: planned.\n\n```text\n" + "TO" + "DO: unsafe placeholder\n```\n"
	violations := lintTemporaryDocument(t, content)
	assertViolationMessageContains(t, violations, "forbidden phrase")
}

func lintTemporaryDocument(t *testing.T, content string) []violation {
	t.Helper()

	path := filepath.Join(t.TempDir(), "document.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temporary document: %v", err)
	}
	violations, err := lintDocument(path, false)
	if err != nil {
		t.Fatalf("lint temporary document: %v", err)
	}
	return violations
}

func assertViolationMessageContains(t *testing.T, violations []violation, fragment string) {
	t.Helper()

	for _, item := range violations {
		if strings.Contains(item.Message, fragment) {
			return
		}
	}
	t.Fatalf("violations = %v, want message containing %q", violations, fragment)
}
