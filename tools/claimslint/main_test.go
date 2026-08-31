package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintClaimsRejectsMarkerForUnacceptedGate(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t)
	writeFixtureFile(t, root, "README.md", "# Fixture\n\nProtocol conformance is proven. <!-- claim:R2 -->\n")
	assertViolation(t, mustLint(t, root), "forbidden while gate status", "R2")
}

func TestLintClaimsAcceptsMarkerForAcceptedGate(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t, "R2")
	writeFixtureFile(t, root, "README.md", "# Fixture\n\nProtocol conformance is proven. <!-- claim:R2 -->\n")
	assertNoViolations(t, mustLint(t, root))
}

func TestLintClaimsRejectsRegisterDrift(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t)
	rewriteMarketingPlan(t, root, func(content string) string {
		return strings.Replace(content,
			claimRow("C2", "R2", "unearned", "—"),
			claimRow("C2", "R2", "earned", "[run](https://example.test/run/2)"), 1)
	})
	assertViolation(t, mustLint(t, root), "claim C2 status", "unearned")
}

func TestLintClaimsScansDocumentationBeyondMarketingPlan(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t)
	writeFixtureFile(t, root, filepath.Join("docs", "extra.rst"), "Public documentation\n\nProtocol conformance is proven. <!-- claim:R2 -->\n")
	item := assertViolation(t, mustLint(t, root), "claim marker for R2")
	if !strings.HasSuffix(item.Path, filepath.Join("docs", "extra.rst")) {
		t.Fatalf("violation path = %q, want docs/extra.rst", item.Path)
	}
}

func TestLintClaimsRequiresRegisterEvidenceColumn(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t)
	rewriteMarketingPlan(t, root, func(content string) string {
		return strings.Replace(content,
			"| # | Claim (exact public sentence) | Ladder level | Gate | Status | Evidence |",
			"| # | Claim | Ladder level | Gate | Status |", 1)
	})
	assertViolation(t, mustLint(t, root), "Evidence")
}

func TestLintClaimsRequiresExactRegisterInventory(t *testing.T) {
	t.Parallel()

	t.Run("missing", func(t *testing.T) {
		root := newFixtureRepository(t)
		rewriteMarketingPlan(t, root, func(content string) string {
			return strings.Replace(content, claimRow("C7", "R7", "unearned", "—")+"\n", "", 1)
		})
		assertViolation(t, mustLint(t, root), "required claim C7", "missing")
	})

	t.Run("extra", func(t *testing.T) {
		root := newFixtureRepository(t)
		rewriteMarketingPlan(t, root, func(content string) string {
			return content + claimRow("C12", "R10", "unearned", "—") + "\n"
		})
		assertViolation(t, mustLint(t, root), "claim C12", "not in the required C1-C11 inventory")
	})

	t.Run("duplicate", func(t *testing.T) {
		root := newFixtureRepository(t)
		rewriteMarketingPlan(t, root, func(content string) string {
			return content + claimRow("C3", "R3", "unearned", "—") + "\n"
		})
		assertViolation(t, mustLint(t, root), "claim C3 is duplicated")
	})

	t.Run("wrong gate", func(t *testing.T) {
		root := newFixtureRepository(t)
		rewriteMarketingPlan(t, root, func(content string) string {
			return strings.Replace(content,
				claimRow("C10", "R9", "unearned", "—"),
				claimRow("C10", "R10", "unearned", "—"), 1)
		})
		assertViolation(t, mustLint(t, root), "claim C10 must map to R9", "got R10")
	})
}

func TestLintClaimsRejectsForbiddenPromotions(t *testing.T) {
	t.Parallel()
	phrases := []string{
		"Conduit provides guaranteed delivery.",
		"Conduit provides exactly-once delivery.",
		"Conduit is infinitely scalable.",
		"Conduit offers zero downtime.",
		"Conduit provides real-time updates.",
	}
	for _, phrase := range phrases {
		phrase := phrase
		t.Run(phrase, func(t *testing.T) {
			t.Parallel()
			root := newFixtureRepository(t)
			writeFixtureFile(t, root, filepath.Join("marketing", "copy.txt"), phrase+"\n")
			assertViolation(t, mustLint(t, root), "forbidden promotional claim")
		})
	}
}

func TestLintClaimsAllowsNormativeForbiddenClaimList(t *testing.T) {
	t.Parallel()
	assertNoViolations(t, mustLint(t, newFixtureRepository(t)))
}

func TestLintClaimsRequiresInlineMarkerForNumericPerformanceClaim(t *testing.T) {
	t.Parallel()
	for _, claim := range []string{
		"Conduit sustains 50,000 concurrent connections.",
		"Conduit sustains 50K concurrent connections.",
		"Conduit delivers 2.5x the baseline throughput.",
	} {
		claim := claim
		t.Run(claim, func(t *testing.T) {
			t.Parallel()
			root := newFixtureRepository(t, "R9")
			writeFixtureFile(t, root, filepath.Join("marketing", "site.txt"), claim+"\n")
			assertViolation(t, mustLint(t, root), "numeric performance or scale claim", "same line")
		})
	}
}

func TestLintClaimsDoesNotAcceptInlineMarkerForPlannedGate(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t)
	writeFixtureFile(t, root, filepath.Join("marketing", "site.txt"), "Conduit sustains 50,000 concurrent connections. <!-- claim:R9 -->\n")
	violations := mustLint(t, root)
	assertViolation(t, violations, "numeric performance or scale claim", "same line")
	assertViolation(t, violations, "claim marker for R9 is forbidden")
}

func TestLintClaimsScansNonMarkdownMarketingAndReleaseAssets(t *testing.T) {
	t.Parallel()
	for _, name := range []string{filepath.Join("marketing", "site.html"), "RELEASE_NOTES.rst"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := newFixtureRepository(t)
			writeFixtureFile(t, root, name, "<p>Conduit provides exactly-once delivery.</p>\n")
			item := assertViolation(t, mustLint(t, root), "forbidden promotional claim")
			if !strings.HasSuffix(item.Path, filepath.FromSlash(name)) {
				t.Fatalf("violation path = %q, want suffix %q", item.Path, name)
			}
		})
	}
}

func TestLintClaimsDoesNotLetMarkerAuthorizeAnotherLine(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t, "R9")
	writeFixtureFile(t, root, filepath.Join("marketing", "site.txt"), "Conduit sustains 50,000 concurrent connections.\n<!-- claim:R9 -->\n")
	violations := mustLint(t, root)
	assertViolation(t, violations, "numeric performance or scale claim", "same line")
	assertViolation(t, violations, "claim marker must be inline", "same line")
}

func TestLintClaimsAcceptsQualifiedMarkedNumericClaim(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t, "R9")
	writeFixtureFile(t, root, filepath.Join("marketing", "site.txt"), "Real-time delivery with measured latency p95 40 ms. <!-- claim:R9 -->\n")
	assertNoViolations(t, mustLint(t, root))
}

func TestLintClaimsDoesNotTreatVersionsAsPerformanceClaims(t *testing.T) {
	t.Parallel()
	root := newFixtureRepository(t)
	writeFixtureFile(t, root, "README.md", "Conduit 1.0 is planned for 2026.\n")
	assertNoViolations(t, mustLint(t, root))
}

func TestUNIT020_CurrentClaimsPass(t *testing.T) {
	violations, err := lintClaims(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("lint claims: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("current claim violations: %v", violations)
	}
}

func newFixtureRepository(t *testing.T, acceptedGates ...string) string {
	t.Helper()
	root := t.TempDir()
	accepted := make(map[string]bool, len(acceptedGates))
	for _, gate := range acceptedGates {
		accepted[gate] = true
	}
	statuses := make(map[string]string, 10)
	for gate := 1; gate <= 10; gate++ {
		id := fmt.Sprintf("R%d", gate)
		statuses[id] = "planned"
		if accepted[id] {
			statuses[id] = "accepted"
		}
	}
	encoded, err := json.Marshal(statuses)
	if err != nil {
		t.Fatalf("marshal fixture statuses: %v", err)
	}
	writeFixtureFile(t, root, "gate-status.json", string(encoded)+"\n")
	writeFixtureFile(t, root, "README.md", "# Fixture\n")

	var plan strings.Builder
	plan.WriteString("# Claims\n\n")
	plan.WriteString("Normative rule: never publish the phrases guaranteed delivery, exactly-once, infinitely scalable, zero downtime, or unqualified real-time.\n\n")
	plan.WriteString("| # | Claim (exact public sentence) | Ladder level | Gate | Status | Evidence |\n")
	plan.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, claim := range requiredClaims {
		status := "unearned"
		evidence := "—"
		if accepted[claim.Gate] {
			status = "earned"
			evidence = fmt.Sprintf("[run](https://example.test/%s)", strings.ToLower(claim.Gate))
		}
		plan.WriteString(claimRow(claim.ID, claim.Gate, status, evidence))
		plan.WriteByte('\n')
	}
	writeFixtureFile(t, root, filepath.Join("docs", "MARKETING_PLAN.md"), plan.String())
	return root
}

func claimRow(id, gate, status, evidence string) string {
	return fmt.Sprintf("| %s | Claim %s. | L1 | %s | %s | %s |", id, id, gate, status, evidence)
}

func rewriteMarketingPlan(t *testing.T, root string, rewrite func(string) string) {
	t.Helper()
	path := filepath.Join(root, "docs", "MARKETING_PLAN.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture marketing plan: %v", err)
	}
	if err := os.WriteFile(path, []byte(rewrite(string(content))), 0o600); err != nil {
		t.Fatalf("rewrite fixture marketing plan: %v", err)
	}
}

func writeFixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}

func mustLint(t *testing.T, root string) []violation {
	t.Helper()
	violations, err := lintClaims(root)
	if err != nil {
		t.Fatalf("lint claims: %v", err)
	}
	return violations
}

func assertNoViolations(t *testing.T, violations []violation) {
	t.Helper()
	if len(violations) != 0 {
		t.Fatalf("violations = %v, want none", violations)
	}
}

func assertViolation(t *testing.T, violations []violation, fragments ...string) violation {
	t.Helper()
	for _, item := range violations {
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(item.Message, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return item
		}
	}
	t.Fatalf("violations = %v, want message containing %q", violations, fragments)
	return violation{}
}
