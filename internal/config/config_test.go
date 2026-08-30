package config_test

import (
	stderrors "errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Zachshotamartin/conduit/internal/config"
	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
)

func TestUNIT001_LoadValidConfigurationThroughPhasesOneToThree(t *testing.T) {
	t.Parallel()

	got, err := config.Load(config.LoadOptions{FilePath: fixturePath(t, "valid.yaml")})
	if err != nil {
		t.Fatalf("Load(valid) error = %v", err)
	}

	if got.Listener.Client.Port != 7101 {
		t.Errorf("Listener.Client.Port = %d, want 7101", got.Listener.Client.Port)
	}
	if got.Listener.Client.Transport != "plaintext" {
		t.Errorf("Listener.Client.Transport = %q, want plaintext", got.Listener.Client.Transport)
	}
	if !got.Listener.Client.PlaintextAcknowledged {
		t.Error("Listener.Client.PlaintextAcknowledged = false, want true")
	}
	if got.Limits.MaxMessageBytes != 512*1024 {
		t.Errorf("Limits.MaxMessageBytes = %d, want %d", got.Limits.MaxMessageBytes, 512*1024)
	}
	if got.Limits.OutboundQueueBytes != 1024*1024 {
		t.Errorf("Limits.OutboundQueueBytes = %d, want %d", got.Limits.OutboundQueueBytes, 1024*1024)
	}
	if got.Connection.Keepalive != 25*time.Second {
		t.Errorf("Connection.Keepalive = %s, want 25s", got.Connection.Keepalive)
	}
	if got.Connection.IdleTimeout != 5*time.Minute {
		t.Errorf("Connection.IdleTimeout = %s, want 5m", got.Connection.IdleTimeout)
	}
	if got.Connection.DrainWindow != time.Minute {
		t.Errorf("Connection.DrainWindow = %s, want 1m", got.Connection.DrainWindow)
	}
	if got.Auth.Mode != "none" || !got.Auth.DevelopmentAcknowledged {
		t.Errorf("Auth = {Mode:%q DevelopmentAcknowledged:%t}, want none/true", got.Auth.Mode, got.Auth.DevelopmentAcknowledged)
	}
}

func TestUNIT001_LoadRejectsInvalidConfigurationsWithoutPartialResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fixture     string
		phase       config.ValidationPhase
		key         string
		expectation string
	}{
		{
			name:        "malformed YAML",
			fixture:     "malformed-yaml.yaml",
			phase:       config.PhaseFileParse,
			key:         "$",
			expectation: "valid YAML document",
		},
		{
			name:        "unknown top-level key",
			fixture:     "unknown-top-level.yaml",
			phase:       config.PhaseFileParse,
			key:         "lisener",
			expectation: "known top-level configuration key",
		},
		{
			name:        "file phase fails before schema and cross-field phases",
			fixture:     "phase-order-file-first.yaml",
			phase:       config.PhaseFileParse,
			key:         "unknown_root",
			expectation: "known top-level configuration key",
		},
		{
			name:        "wrong type",
			fixture:     "wrong-type.yaml",
			phase:       config.PhaseSchemaValidation,
			key:         "listener.client.port",
			expectation: "integer",
		},
		{
			name:        "out of range",
			fixture:     "out-of-range.yaml",
			phase:       config.PhaseSchemaValidation,
			key:         "listener.client.port",
			expectation: "integer between 1 and 65535",
		},
		{
			name:        "schema phase fails before cross-field phase",
			fixture:     "phase-order-schema-before-cross-field.yaml",
			phase:       config.PhaseSchemaValidation,
			key:         "listener.client.port",
			expectation: "integer between 1 and 65535",
		},
		{
			name:        "queue smaller than message",
			fixture:     "cross-field-queue.yaml",
			phase:       config.PhaseCrossFieldValidation,
			key:         "limits.outbound_queue_bytes",
			expectation: "greater than or equal to limits.max_message_bytes",
		},
		{
			name:        "zero drain window",
			fixture:     "cross-field-drain.yaml",
			phase:       config.PhaseCrossFieldValidation,
			key:         "connection.drain_window",
			expectation: "duration greater than zero",
		},
		{
			name:        "keepalive not below idle timeout",
			fixture:     "cross-field-keepalive.yaml",
			phase:       config.PhaseCrossFieldValidation,
			key:         "connection.keepalive",
			expectation: "less than connection.idle_timeout",
		},
		{
			name:        "warning not below token lifetime floor",
			fixture:     "cross-field-warning.yaml",
			phase:       config.PhaseCrossFieldValidation,
			key:         "auth.expiry_warning_window",
			expectation: "less than auth.token_lifetime_floor",
		},
		{
			name:        "tenant schema set while tenancy disabled",
			fixture:     "cross-field-tenancy.yaml",
			phase:       config.PhaseCrossFieldValidation,
			key:         "tenancy.schema_sets",
			expectation: "empty when tenancy.enabled is false",
		},
		{
			name:        "development auth not acknowledged",
			fixture:     "cross-field-auth-ack.yaml",
			phase:       config.PhaseCrossFieldValidation,
			key:         "auth.development_acknowledged",
			expectation: "true when auth.mode is none",
		},
		{
			name:        "plaintext listener not acknowledged",
			fixture:     "cross-field-plaintext-ack.yaml",
			phase:       config.PhaseCrossFieldValidation,
			key:         "listener.client.plaintext_acknowledged",
			expectation: "true when listener.client.transport is plaintext",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := fixturePath(t, tc.fixture)
			got, err := config.Load(config.LoadOptions{FilePath: path})
			if err == nil {
				t.Fatal("Load() error = nil, want validation failure")
			}
			if !reflect.DeepEqual(got, config.Config{}) {
				t.Fatalf("Load() returned partial Config on failure: %#v", got)
			}
			assertValidationError(t, err, tc.phase, tc.key, path, tc.expectation)
		})
	}
}

func TestUNIT001_LoadReportsOverlaySourceErrorsWithoutPartialResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		options     config.LoadOptions
		key         string
		source      string
		expectation string
	}{
		{
			name: "environment wrong type",
			options: config.LoadOptions{Environment: map[string]string{
				"CONDUIT_LISTENER_CLIENT_PORT": "not-a-port",
			}},
			key:         "listener.client.port",
			source:      "environment CONDUIT_LISTENER_CLIENT_PORT",
			expectation: "integer",
		},
		{
			name: "flag out of range",
			options: config.LoadOptions{Flags: map[string]string{
				"listener.client.port": "70000",
			}},
			key:         "listener.client.port",
			source:      "flag listener.client.port",
			expectation: "integer between 1 and 65535",
		},
		{
			name: "unknown generated environment key",
			options: config.LoadOptions{Environment: map[string]string{
				"CONDUIT_NOT_IN_SCHEMA": "value",
			}},
			key:         "CONDUIT_NOT_IN_SCHEMA",
			source:      "environment CONDUIT_NOT_IN_SCHEMA",
			expectation: "environment key generated from the configuration schema",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(tc.options)
			if err == nil {
				t.Fatal("Load() error = nil, want validation failure")
			}
			if !reflect.DeepEqual(got, config.Config{}) {
				t.Fatalf("Load() returned partial Config on failure: %#v", got)
			}
			assertValidationError(t, err, config.PhaseSchemaValidation, tc.key, tc.source, tc.expectation)
		})
	}
}

func TestUNIT001_LoadAppliesExactPrecedence(t *testing.T) {
	t.Parallel()

	defaults := config.Defaults()
	file := fixturePath(t, "valid.yaml")
	tests := []struct {
		name     string
		options  config.LoadOptions
		wantPort int
	}{
		{name: "defaults", wantPort: defaults.Listener.Client.Port},
		{name: "file over defaults", options: config.LoadOptions{FilePath: file}, wantPort: 7101},
		{
			name: "environment over file",
			options: config.LoadOptions{
				FilePath: file,
				Environment: map[string]string{
					"CONDUIT_LISTENER_CLIENT_PORT": "7201",
				},
			},
			wantPort: 7201,
		},
		{
			name: "flags over environment and file",
			options: config.LoadOptions{
				FilePath: file,
				Environment: map[string]string{
					"CONDUIT_LISTENER_CLIENT_PORT": "7201",
				},
				Flags: map[string]string{
					"listener.client.port": "7301",
				},
			},
			wantPort: 7301,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(tc.options)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.Listener.Client.Port != tc.wantPort {
				t.Fatalf("Listener.Client.Port = %d, want %d", got.Listener.Client.Port, tc.wantPort)
			}
			if tc.options.FilePath != "" && got.Limits.MaxMessageBytes != 512*1024 {
				t.Fatalf("unoverridden file value was lost: Limits.MaxMessageBytes = %d", got.Limits.MaxMessageBytes)
			}
		})
	}
}

func TestUNIT001_EnvironmentMappingIsGeneratedFromSchema(t *testing.T) {
	t.Parallel()

	requiredPaths := map[string]bool{
		"listener.client.port":                   false,
		"listener.client.transport":              false,
		"listener.client.plaintext_acknowledged": false,
		"limits.max_message_bytes":               false,
		"limits.outbound_queue_bytes":            false,
		"connection.keepalive":                   false,
		"connection.idle_timeout":                false,
		"connection.drain_window":                false,
		"auth.mode":                              false,
		"auth.development_acknowledged":          false,
		"auth.expiry_warning_window":             false,
		"auth.token_lifetime_floor":              false,
		"tenancy.enabled":                        false,
		"tenancy.schema_sets":                    false,
	}
	seenPaths := make(map[string]struct{})
	seenEnvironment := make(map[string]struct{})
	fields := config.SchemaFields()
	if len(fields) == 0 {
		t.Fatal("SchemaFields() returned no fields")
	}
	for _, field := range fields {
		if field.Path == "" {
			t.Error("schema contains an empty path")
			continue
		}
		if _, duplicate := seenPaths[field.Path]; duplicate {
			t.Errorf("schema contains duplicate path %q", field.Path)
		}
		seenPaths[field.Path] = struct{}{}
		if _, duplicate := seenEnvironment[field.Environment]; duplicate {
			t.Errorf("schema contains duplicate environment key %q", field.Environment)
		}
		seenEnvironment[field.Environment] = struct{}{}

		wantEnvironment := "CONDUIT_" + strings.ToUpper(strings.ReplaceAll(field.Path, ".", "_"))
		if field.Environment != wantEnvironment {
			t.Errorf("schema field %q environment = %q, want generated %q", field.Path, field.Environment, wantEnvironment)
		}
		if _, required := requiredPaths[field.Path]; required {
			requiredPaths[field.Path] = true
		}
	}
	for path, found := range requiredPaths {
		if !found {
			t.Errorf("SchemaFields() omits R0 field %q", path)
		}
	}

	if got, ok := config.PathForEnvironment("CONDUIT_LISTENER_CLIENT_PORT"); !ok || got != "listener.client.port" {
		t.Fatalf("PathForEnvironment(CONDUIT_LISTENER_CLIENT_PORT) = %q, %t; want listener.client.port, true", got, ok)
	}
	if _, ok := config.PathForEnvironment("CONDUIT_NOT_IN_SCHEMA"); ok {
		t.Fatal("PathForEnvironment accepted an environment key absent from the schema")
	}

	firstPath := fields[0].Path
	fields[0].Path = "caller.mutation"
	if got := config.SchemaFields()[0].Path; got != firstPath {
		t.Fatalf("SchemaFields() exposed mutable package state: got %q, want %q", got, firstPath)
	}
}

func TestUNIT001_ValidationPhaseFrameworkIsOrderedAndStable(t *testing.T) {
	t.Parallel()

	want := []config.ValidationPhase{
		config.PhaseFileParse,
		config.PhaseSchemaValidation,
		config.PhaseCrossFieldValidation,
	}
	got := config.ValidationPhases()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ValidationPhases() = %v, want %v", got, want)
	}
	wantNames := []string{"file_parse", "schema_validation", "cross_field_validation"}
	for i, phase := range got {
		if phase.String() != wantNames[i] {
			t.Errorf("phase %d String() = %q, want %q", i, phase.String(), wantNames[i])
		}
	}

	got[0] = config.ValidationPhase(255)
	if next := config.ValidationPhases(); !reflect.DeepEqual(next, want) {
		t.Fatalf("ValidationPhases() exposed mutable package state: %v", next)
	}
}

func assertValidationError(
	t *testing.T,
	err error,
	wantPhase config.ValidationPhase,
	wantKey string,
	wantSource string,
	wantExpectation string,
) {
	t.Helper()

	var validation *config.ValidationError
	if !stderrors.As(err, &validation) {
		t.Fatalf("error type = %T, want *config.ValidationError: %v", err, err)
	}
	if validation.Phase() != wantPhase {
		t.Errorf("Phase() = %v, want %v", validation.Phase(), wantPhase)
	}
	if validation.Key() != wantKey {
		t.Errorf("Key() = %q, want %q", validation.Key(), wantKey)
	}
	if validation.Source() != wantSource {
		t.Errorf("Source() = %q, want %q", validation.Source(), wantSource)
	}
	if validation.Expectation() != wantExpectation {
		t.Errorf("Expectation() = %q, want %q", validation.Expectation(), wantExpectation)
	}
	for label, value := range map[string]string{
		"key":         wantKey,
		"source":      wantSource,
		"expectation": wantExpectation,
	} {
		if !strings.Contains(err.Error(), value) {
			t.Errorf("Error() = %q; does not name %s %q", err, label, value)
		}
	}

	var classified *conduiterrors.Error
	if !stderrors.As(err, &classified) {
		t.Fatalf("error chain has no *errors.Error classification: %v", err)
	}
	if classified.Category() != conduiterrors.InvalidConfiguration {
		t.Errorf("error category = %q, want %q", classified.Category(), conduiterrors.InvalidConfiguration)
	}
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate config_test.go")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "test", "fixtures", "config", name))
}
