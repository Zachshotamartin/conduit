package errors

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
)

// Category is a stable, bounded classification for a Conduit failure.
type Category string

// Conduit's top-level error categories. These values are wire- and
// metrics-stable; changing one requires a compatibility review.
const (
	InvalidRequest        Category = "invalid_request"
	InvalidConfiguration  Category = "invalid_configuration"
	Unauthenticated       Category = "unauthenticated"
	PermissionDenied      Category = "permission_denied"
	TokenExpired          Category = "token_expired"
	GrantRevoked          Category = "grant_revoked"
	QuotaExceeded         Category = "quota_exceeded"
	RateLimited           Category = "rate_limited"
	ComplexityExceeded    Category = "complexity_exceeded"
	SourceUnavailable     Category = "source_unavailable"
	SourceTimeout         Category = "source_timeout"
	SourceInvalidResponse Category = "source_invalid_response"
	BusUnavailable        Category = "bus_unavailable"
	BusDegraded           Category = "bus_degraded"
	PublishRejected       Category = "publish_rejected"
	ResumeRejected        Category = "resume_rejected"
	ResumeGap             Category = "resume_gap"
	Overloaded            Category = "overloaded"
	Draining              Category = "draining"
	Timeout               Category = "timeout"
	Cancelled             Category = "cancelled"
	InternalInvariant     Category = "internal_invariant"
)

type definition struct {
	safeMessage string
	metricKey   string
}

var categoryOrder = []Category{
	InvalidRequest,
	InvalidConfiguration,
	Unauthenticated,
	PermissionDenied,
	TokenExpired,
	GrantRevoked,
	QuotaExceeded,
	RateLimited,
	ComplexityExceeded,
	SourceUnavailable,
	SourceTimeout,
	SourceInvalidResponse,
	BusUnavailable,
	BusDegraded,
	PublishRejected,
	ResumeRejected,
	ResumeGap,
	Overloaded,
	Draining,
	Timeout,
	Cancelled,
	InternalInvariant,
}

var definitions = map[Category]definition{
	InvalidRequest:        {safeMessage: "invalid request", metricKey: "invalid_request"},
	InvalidConfiguration:  {safeMessage: "invalid configuration", metricKey: "invalid_configuration"},
	Unauthenticated:       {safeMessage: "authentication required", metricKey: "unauthenticated"},
	PermissionDenied:      {safeMessage: "permission denied", metricKey: "permission_denied"},
	TokenExpired:          {safeMessage: "authentication token expired", metricKey: "token_expired"},
	GrantRevoked:          {safeMessage: "authorization grant revoked", metricKey: "grant_revoked"},
	QuotaExceeded:         {safeMessage: "quota exceeded", metricKey: "quota_exceeded"},
	RateLimited:           {safeMessage: "rate limit exceeded", metricKey: "rate_limited"},
	ComplexityExceeded:    {safeMessage: "operation complexity limit exceeded", metricKey: "complexity_exceeded"},
	SourceUnavailable:     {safeMessage: "data source unavailable", metricKey: "source_unavailable"},
	SourceTimeout:         {safeMessage: "data source timed out", metricKey: "source_timeout"},
	SourceInvalidResponse: {safeMessage: "data source returned an invalid response", metricKey: "source_invalid_response"},
	BusUnavailable:        {safeMessage: "event bus unavailable", metricKey: "bus_unavailable"},
	BusDegraded:           {safeMessage: "event bus degraded", metricKey: "bus_degraded"},
	PublishRejected:       {safeMessage: "publish rejected", metricKey: "publish_rejected"},
	ResumeRejected:        {safeMessage: "resume rejected", metricKey: "resume_rejected"},
	ResumeGap:             {safeMessage: "resume gap", metricKey: "resume_gap"},
	Overloaded:            {safeMessage: "service overloaded", metricKey: "overloaded"},
	Draining:              {safeMessage: "service draining", metricKey: "draining"},
	Timeout:               {safeMessage: "operation timed out", metricKey: "timeout"},
	Cancelled:             {safeMessage: "operation cancelled", metricKey: "cancelled"},
	InternalInvariant:     {safeMessage: "internal error", metricKey: "internal_invariant"},
}

// Error is a classified Conduit failure. Its public string and JSON forms
// contain only client-safe data; Unwrap retains the diagnostic cause.
type Error struct {
	category Category
	cause    error
}

// Categories returns a snapshot of every supported top-level category.
func Categories() []Category {
	return append([]Category(nil), categoryOrder...)
}

// New constructs an error in category. An unknown category fails closed as
// InternalInvariant so unreviewed values cannot enter wire output or metrics.
func New(category Category) *Error {
	return &Error{category: normalize(category)}
}

// Wrap constructs an error in category while retaining cause for errors.Is,
// errors.As, and operator-side diagnostics. The cause is never rendered by
// Error, SafeMessage, or MarshalJSON.
func Wrap(category Category, cause error) *Error {
	return &Error{category: normalize(category), cause: cause}
}

// WrapUnknown classifies an untyped failure once at its owning boundary.
// A top-level Error is returned unchanged. If an Error is hidden beneath an
// arbitrary wrapper, WrapUnknown preserves that diagnostic chain behind a
// new client-safe Error carrying the existing category.
func WrapUnknown(err error) error {
	if err == nil {
		return nil
	}

	var typed *Error
	if stderrors.As(err, &typed) {
		if err == typed {
			return typed
		}
		return Wrap(typed.Category(), err)
	}

	return Wrap(InternalInvariant, err)
}

// Category returns the stable classification of e.
func (e *Error) Category() Category {
	return normalize(e.rawCategory())
}

// SafeMessage returns the canonical client-safe message for e.
func (e *Error) SafeMessage() string {
	return definitionFor(e.Category()).safeMessage
}

// MetricKey returns the bounded-cardinality key used to count e.
func (e *Error) MetricKey() string {
	return definitionFor(e.Category()).metricKey
}

// Error implements error without exposing the diagnostic cause.
func (e *Error) Error() string {
	return e.SafeMessage()
}

// Unwrap returns the diagnostic cause, if any.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// MarshalJSON emits the stable client-safe representation of e.
func (e *Error) MarshalJSON() ([]byte, error) {
	payload := struct {
		Category Category `json:"category"`
		Message  string   `json:"message"`
	}{
		Category: e.Category(),
		Message:  e.SafeMessage(),
	}
	return json.Marshal(payload)
}

// UnmarshalJSON accepts only the canonical client-safe representation. It
// rejects unknown categories, noncanonical messages, and extra fields so a
// decoded Error cannot smuggle unbounded or sensitive text onto a boundary.
func (e *Error) UnmarshalJSON(data []byte) error {
	if e == nil {
		return fmt.Errorf("decode conduit error into nil receiver")
	}

	payload := struct {
		Category Category `json:"category"`
		Message  string   `json:"message"`
	}{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("decode conduit error: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode conduit error: multiple JSON values")
		}
		return fmt.Errorf("decode conduit error trailing data: %w", err)
	}

	definition, known := definitions[payload.Category]
	if !known {
		return fmt.Errorf("decode conduit error: unknown category %q", payload.Category)
	}
	if payload.Message != definition.safeMessage {
		return fmt.Errorf("decode conduit error: noncanonical message for category %q", payload.Category)
	}

	*e = Error{category: payload.Category}
	return nil
}

func (e *Error) rawCategory() Category {
	if e == nil {
		return InternalInvariant
	}
	return e.category
}

func normalize(category Category) Category {
	if _, ok := definitions[category]; !ok {
		return InternalInvariant
	}
	return category
}

func definitionFor(category Category) definition {
	return definitions[normalize(category)]
}
