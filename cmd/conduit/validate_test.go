package main

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUNIT001_RunValidateAcceptsValidConfiguration(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	got := Run(
		context.Background(),
		[]string{"validate", "--config", commandFixturePath(t, "valid.yaml")},
		map[string]string{},
		&stdout,
		&stderr,
	)
	if got != 0 {
		t.Fatalf("Run(validate valid) = %d, want 0; stderr = %q", got, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run(validate valid) stderr = %q, want empty", stderr.String())
	}
}

func TestUNIT016_RunValidateReturnsStableFailureCodeAndActionableError(t *testing.T) {
	t.Parallel()

	wantCode, ok := ExitCodeFor("validate_failure")
	if !ok {
		t.Fatal("exit table has no validate_failure entry")
	}
	tests := []struct {
		name        string
		fixture     string
		key         string
		expectation string
	}{
		{
			name:        "unknown top-level key",
			fixture:     "unknown-top-level.yaml",
			key:         "lisener",
			expectation: "known top-level configuration key",
		},
		{
			name:        "out of range",
			fixture:     "out-of-range.yaml",
			key:         "listener.client.port",
			expectation: "integer between 1 and 65535",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := commandFixturePath(t, tc.fixture)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			got := Run(
				context.Background(),
				[]string{"validate", "--config", path},
				map[string]string{},
				&stdout,
				&stderr,
			)
			if got != wantCode {
				t.Fatalf("Run(validate invalid) = %d, want %d", got, wantCode)
			}
			if stdout.Len() != 0 {
				t.Errorf("Run(validate invalid) stdout = %q, want empty", stdout.String())
			}
			for label, value := range map[string]string{
				"key":         tc.key,
				"source":      path,
				"expectation": tc.expectation,
			} {
				if !strings.Contains(stderr.String(), value) {
					t.Errorf("stderr = %q; does not name %s %q", stderr.String(), label, value)
				}
			}
		})
	}
}

func commandFixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate validate_test.go")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "test", "fixtures", "config", name))
}
