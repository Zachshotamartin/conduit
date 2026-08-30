package errors_test

import (
	"encoding/json"
	stderrors "errors"
	"reflect"
	"strings"
	"testing"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
)

type categoryCase struct {
	category    conduiterrors.Category
	safeMessage string
	metricKey   string
}

var categoryCases = []categoryCase{
	{conduiterrors.InvalidRequest, "invalid request", "invalid_request"},
	{conduiterrors.InvalidConfiguration, "invalid configuration", "invalid_configuration"},
	{conduiterrors.Unauthenticated, "authentication required", "unauthenticated"},
	{conduiterrors.PermissionDenied, "permission denied", "permission_denied"},
	{conduiterrors.TokenExpired, "authentication token expired", "token_expired"},
	{conduiterrors.GrantRevoked, "authorization grant revoked", "grant_revoked"},
	{conduiterrors.QuotaExceeded, "quota exceeded", "quota_exceeded"},
	{conduiterrors.RateLimited, "rate limit exceeded", "rate_limited"},
	{conduiterrors.ComplexityExceeded, "operation complexity limit exceeded", "complexity_exceeded"},
	{conduiterrors.SourceUnavailable, "data source unavailable", "source_unavailable"},
	{conduiterrors.SourceTimeout, "data source timed out", "source_timeout"},
	{conduiterrors.SourceInvalidResponse, "data source returned an invalid response", "source_invalid_response"},
	{conduiterrors.BusUnavailable, "event bus unavailable", "bus_unavailable"},
	{conduiterrors.BusDegraded, "event bus degraded", "bus_degraded"},
	{conduiterrors.PublishRejected, "publish rejected", "publish_rejected"},
	{conduiterrors.ResumeRejected, "resume rejected", "resume_rejected"},
	{conduiterrors.ResumeGap, "resume gap", "resume_gap"},
	{conduiterrors.Overloaded, "service overloaded", "overloaded"},
	{conduiterrors.Draining, "service draining", "draining"},
	{conduiterrors.Timeout, "operation timed out", "timeout"},
	{conduiterrors.Cancelled, "operation cancelled", "cancelled"},
	{conduiterrors.InternalInvariant, "internal error", "internal_invariant"},
}

func TestCategoriesAreExhaustiveAndConstructSafely(t *testing.T) {
	t.Parallel()

	wantCategories := make(map[conduiterrors.Category]struct{}, len(categoryCases))
	for _, tc := range categoryCases {
		tc := tc
		if _, duplicate := wantCategories[tc.category]; duplicate {
			t.Fatalf("test table contains duplicate category %q", tc.category)
		}
		wantCategories[tc.category] = struct{}{}

		t.Run(string(tc.category), func(t *testing.T) {
			t.Parallel()

			got := conduiterrors.New(tc.category)
			if got == nil {
				t.Fatal("New returned nil")
			}
			if got.Category() != tc.category {
				t.Fatalf("Category() = %q, want %q", got.Category(), tc.category)
			}
			if got.SafeMessage() != tc.safeMessage {
				t.Fatalf("SafeMessage() = %q, want %q", got.SafeMessage(), tc.safeMessage)
			}
			if got.MetricKey() != tc.metricKey {
				t.Fatalf("MetricKey() = %q, want %q", got.MetricKey(), tc.metricKey)
			}
			if got.Error() != tc.safeMessage {
				t.Fatalf("Error() = %q, want the client-safe message %q", got.Error(), tc.safeMessage)
			}

			wire, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			var payload map[string]string
			if err := json.Unmarshal(wire, &payload); err != nil {
				t.Fatalf("Unmarshal(%s) error = %v", wire, err)
			}
			wantPayload := map[string]string{
				"category": string(tc.category),
				"message":  tc.safeMessage,
			}
			if !reflect.DeepEqual(payload, wantPayload) {
				t.Fatalf("serialized error = %#v, want %#v", payload, wantPayload)
			}
		})
	}

	gotCategories := conduiterrors.Categories()
	if len(gotCategories) != len(wantCategories) {
		t.Fatalf("Categories() returned %d categories, want %d: %v", len(gotCategories), len(wantCategories), gotCategories)
	}
	seen := make(map[conduiterrors.Category]struct{}, len(gotCategories))
	for _, category := range gotCategories {
		if _, duplicate := seen[category]; duplicate {
			t.Fatalf("Categories() contains duplicate category %q", category)
		}
		seen[category] = struct{}{}
		if _, known := wantCategories[category]; !known {
			t.Errorf("Categories() contains undocumented category %q", category)
		}
	}
	for category := range wantCategories {
		if _, ok := seen[category]; !ok {
			t.Errorf("Categories() omits documented category %q", category)
		}
	}
}

func TestCategoriesReturnsAnIndependentSnapshot(t *testing.T) {
	t.Parallel()

	first := conduiterrors.Categories()
	if len(first) == 0 {
		t.Fatal("Categories() returned no categories")
	}
	original := first[0]
	first[0] = conduiterrors.Category("caller_mutation")

	second := conduiterrors.Categories()
	if second[0] != original {
		t.Fatalf("Categories() exposed mutable package state: first category = %q, want %q", second[0], original)
	}
}

func TestWrapRetainsDiagnosticCauseWithoutClientLeakage(t *testing.T) {
	t.Parallel()

	const canary = "postgres://admin:secret@10.0.0.8 SELECT token FROM users upstream-body stack-frame"
	cause := stderrors.New(canary)
	got := conduiterrors.Wrap(conduiterrors.SourceInvalidResponse, cause)

	if !stderrors.Is(got, cause) {
		t.Fatal("errors.Is did not find the wrapped diagnostic cause")
	}
	var typed *conduiterrors.Error
	if !stderrors.As(got, &typed) {
		t.Fatal("errors.As did not find *errors.Error")
	}
	if typed != got {
		t.Fatal("errors.As returned a different typed error")
	}
	if got.Category() != conduiterrors.SourceInvalidResponse {
		t.Fatalf("Category() = %q, want %q", got.Category(), conduiterrors.SourceInvalidResponse)
	}

	clientSurfaces := map[string]string{
		"Error()":       got.Error(),
		"SafeMessage()": got.SafeMessage(),
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	clientSurfaces["JSON"] = string(wire)
	for surface, value := range clientSurfaces {
		if strings.Contains(value, canary) {
			t.Errorf("%s leaked the diagnostic cause: %q", surface, value)
		}
	}

	var payload map[string]string
	if err := json.Unmarshal(wire, &payload); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", wire, err)
	}
	wantPayload := map[string]string{
		"category": "source_invalid_response",
		"message":  "data source returned an invalid response",
	}
	if !reflect.DeepEqual(payload, wantPayload) {
		t.Fatalf("serialized wrapped error = %#v, want %#v", payload, wantPayload)
	}
}

func TestWrapUnknownConvertsExactlyOnce(t *testing.T) {
	t.Parallel()

	const canary = "internal SQL and stack trace must not reach the client"
	unknown := stderrors.New(canary)
	got := conduiterrors.WrapUnknown(unknown)
	if got == nil {
		t.Fatal("WrapUnknown returned nil for a non-nil error")
	}
	if got == unknown {
		t.Fatal("WrapUnknown returned the unclassified error unchanged")
	}
	if !stderrors.Is(got, unknown) {
		t.Fatal("errors.Is did not find the original unknown error")
	}

	var typed *conduiterrors.Error
	if !stderrors.As(got, &typed) {
		t.Fatal("errors.As did not find *errors.Error")
	}
	if typed.Category() != conduiterrors.InternalInvariant {
		t.Fatalf("Category() = %q, want %q", typed.Category(), conduiterrors.InternalInvariant)
	}
	if typed.SafeMessage() != "internal error" {
		t.Fatalf("SafeMessage() = %q, want %q", typed.SafeMessage(), "internal error")
	}
	if typed.MetricKey() != "internal_invariant" {
		t.Fatalf("MetricKey() = %q, want %q", typed.MetricKey(), "internal_invariant")
	}
	if strings.Contains(got.Error(), canary) {
		t.Fatalf("Error() leaked unknown cause: %q", got.Error())
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(wire), canary) {
		t.Fatalf("JSON leaked unknown cause: %s", wire)
	}

	if again := conduiterrors.WrapUnknown(got); again != got {
		t.Fatal("WrapUnknown wrapped an already-typed error a second time")
	}
}

func TestWrapUnknownNilIsNil(t *testing.T) {
	t.Parallel()

	if got := conduiterrors.WrapUnknown(nil); got != nil {
		t.Fatalf("WrapUnknown(nil) = %v, want nil", got)
	}
}
