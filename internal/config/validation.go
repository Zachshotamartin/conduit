package config

import (
	"fmt"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
)

// ValidationPhase identifies an ordered R0 configuration-validation phase.
type ValidationPhase uint8

// R0 validation phases, in fail-fast execution order.
const (
	PhaseFileParse ValidationPhase = iota + 1
	PhaseSchemaValidation
	PhaseCrossFieldValidation
)

var validationPhases = []ValidationPhase{
	PhaseFileParse,
	PhaseSchemaValidation,
	PhaseCrossFieldValidation,
}

// String returns the stable machine name for a validation phase.
func (phase ValidationPhase) String() string {
	switch phase {
	case PhaseFileParse:
		return "file_parse"
	case PhaseSchemaValidation:
		return "schema_validation"
	case PhaseCrossFieldValidation:
		return "cross_field_validation"
	default:
		return "unknown"
	}
}

// ValidationPhases returns an immutable snapshot of phases 1 through 3.
func ValidationPhases() []ValidationPhase {
	return append([]ValidationPhase(nil), validationPhases...)
}

// ValidationError is an actionable configuration failure. Its classification
// unwraps to errors.InvalidConfiguration; diagnostic causes remain further
// down that chain without being interpolated into the operator message.
type ValidationError struct {
	phase       ValidationPhase
	key         string
	source      string
	expectation string
	classified  *conduiterrors.Error
}

// Error names the phase, key, source, and expectation deterministically.
func (err *ValidationError) Error() string {
	if err == nil {
		return "configuration validation failed"
	}
	return fmt.Sprintf(
		"configuration %s: key %q from %s: expected %s",
		err.phase.String(),
		err.key,
		err.source,
		err.expectation,
	)
}

// Unwrap exposes the stable InvalidConfiguration classification.
func (err *ValidationError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.classified
}

// Phase returns the phase that rejected the configuration.
func (err *ValidationError) Phase() ValidationPhase {
	if err == nil {
		return 0
	}
	return err.phase
}

// Key returns the schema path or input selector that failed.
func (err *ValidationError) Key() string {
	if err == nil {
		return ""
	}
	return err.key
}

// Source returns the exact file, environment variable, flag, or default
// source that supplied the rejected value.
func (err *ValidationError) Source() string {
	if err == nil {
		return ""
	}
	return err.source
}

// Expectation returns the violated type, range, or coherence rule.
func (err *ValidationError) Expectation() string {
	if err == nil {
		return ""
	}
	return err.expectation
}

func newValidationError(phase ValidationPhase, key, source, expectation string, cause error) *ValidationError {
	return &ValidationError{
		phase:       phase,
		key:         key,
		source:      source,
		expectation: expectation,
		classified:  conduiterrors.Wrap(conduiterrors.InvalidConfiguration, cause),
	}
}

type loadState struct {
	config     Config
	provenance map[string]string
}

func newLoadState() *loadState {
	provenance := make(map[string]string, len(schemaPaths))
	for _, path := range schemaPaths {
		provenance[path] = "built-in defaults"
	}
	return &loadState{config: Defaults(), provenance: provenance}
}

func (state *loadState) crossFieldError(key, expectation string) *ValidationError {
	source := state.provenance[key]
	if source == "" {
		source = "built-in defaults"
	}
	return newValidationError(PhaseCrossFieldValidation, key, source, expectation, nil)
}

func (state *loadState) validateCrossFields() error {
	config := state.config
	if config.Limits.OutboundQueueBytes < config.Limits.MaxMessageBytes {
		return state.crossFieldError(
			"limits.outbound_queue_bytes",
			"greater than or equal to limits.max_message_bytes",
		)
	}
	if config.Connection.DrainWindow <= 0 {
		return state.crossFieldError("connection.drain_window", "duration greater than zero")
	}
	if config.Connection.Keepalive >= config.Connection.IdleTimeout {
		return state.crossFieldError("connection.keepalive", "less than connection.idle_timeout")
	}
	if config.Auth.ExpiryWarningWindow >= config.Auth.TokenLifetimeFloor {
		return state.crossFieldError("auth.expiry_warning_window", "less than auth.token_lifetime_floor")
	}
	if !config.Tenancy.Enabled && len(config.Tenancy.SchemaSets) != 0 {
		return state.crossFieldError("tenancy.schema_sets", "empty when tenancy.enabled is false")
	}
	if config.Auth.Mode == "none" && !config.Auth.DevelopmentAcknowledged {
		return state.crossFieldError("auth.development_acknowledged", "true when auth.mode is none")
	}
	if config.Listener.Client.Transport == "plaintext" && !config.Listener.Client.PlaintextAcknowledged {
		return state.crossFieldError(
			"listener.client.plaintext_acknowledged",
			"true when listener.client.transport is plaintext",
		)
	}
	return nil
}
