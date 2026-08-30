package main

import (
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

func TestCurrentDocumentationPasses(t *testing.T) {
	violations, err := lintDocs(filepath.Join("..", "..", "docs"))
	if err != nil {
		t.Fatalf("lint docs: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("current documentation violations: %v", violations)
	}
}
