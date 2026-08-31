package redaction_test

import (
	stderrors "errors"
	"strings"
	"testing"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	"github.com/Zachshotamartin/conduit/internal/observability/redaction"
)

func TestUNIT006_ClientMessagesDiscardDiagnosticCauses(t *testing.T) {
	t.Parallel()
	const canary = "SELECT r1_canary_secret FROM internal.table at 10.1.2.3:5432"
	for _, category := range conduiterrors.Categories() {
		message := redaction.ClientErrorMessage(category, stderrors.New(canary))
		if strings.Contains(message, canary) || strings.Contains(message, "SELECT") || strings.Contains(message, "10.1.2.3") {
			t.Fatalf("category %q leaked diagnostic cause in %q", category, message)
		}
		if message != conduiterrors.New(category).SafeMessage() {
			t.Fatalf("category %q message = %q, want canonical safe message", category, message)
		}
		if repeated := redaction.ClientErrorMessage(category, stderrors.New(message)); repeated != message {
			t.Fatalf("category %q redaction is not idempotent: %q then %q", category, message, repeated)
		}
	}
	if got := redaction.ClientErrorMessage("unreviewed_category", stderrors.New(canary)); got != "internal error" {
		t.Fatalf("unknown category message = %q, want fail-closed internal error", got)
	}
}
