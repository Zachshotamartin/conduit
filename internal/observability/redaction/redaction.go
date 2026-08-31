// Package redaction owns the allowlisted transformations used at output
// sinks. Diagnostic causes are retained for operator-side classification but
// are never interpolated into client-visible messages.
package redaction

import conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"

// ClientErrorMessage returns the bounded canonical message for category and
// deliberately discards diagnostic. Accepting the cause at this boundary
// makes accidental formatting of upstream bodies, statements, addresses, or
// stack traces a testable violation instead of a caller convention.
func ClientErrorMessage(category conduiterrors.Category, _ error) string {
	return conduiterrors.New(category).SafeMessage()
}
