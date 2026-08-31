package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Zachshotamartin/conduit/internal/config"
)

func TestUNIT001_FileSearchContractConstants(t *testing.T) {
	t.Parallel()

	if config.ConfigPathEnvironment != "CONDUIT_CONFIG" {
		t.Errorf("ConfigPathEnvironment = %q, want CONDUIT_CONFIG", config.ConfigPathEnvironment)
	}
	if config.WorkingDirectoryFile != "conduit.yaml" {
		t.Errorf("WorkingDirectoryFile = %q, want conduit.yaml", config.WorkingDirectoryFile)
	}
	if config.DefaultSystemPath != "/etc/conduit/conduit.yaml" {
		t.Errorf("DefaultSystemPath = %q, want /etc/conduit/conduit.yaml", config.DefaultSystemPath)
	}
}

func TestUNIT001_ResolveFileUsesDocumentedSearchPrecedence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	working := filepath.Join(root, "working")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	flagPath := writeConfigCandidate(t, root, "flag.yaml")
	environmentPath := writeConfigCandidate(t, root, "environment.yaml")
	workingPath := writeConfigCandidate(t, working, "conduit.yaml")
	systemPath := writeConfigCandidate(t, root, "system.yaml")
	emptyWorking := filepath.Join(root, "empty-working")
	if err := os.MkdirAll(emptyWorking, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	tests := []struct {
		name       string
		options    config.FileSearchOptions
		wantPath   string
		wantSource config.FileSource
	}{
		{
			name: "flag before every other candidate",
			options: config.FileSearchOptions{
				FlagPath:         flagPath,
				Environment:      map[string]string{"CONDUIT_CONFIG": environmentPath},
				WorkingDirectory: working,
				SystemPath:       systemPath,
			},
			wantPath:   flagPath,
			wantSource: config.FileFromFlag,
		},
		{
			name: "environment before working directory and system",
			options: config.FileSearchOptions{
				Environment:      map[string]string{"CONDUIT_CONFIG": environmentPath},
				WorkingDirectory: working,
				SystemPath:       systemPath,
			},
			wantPath:   environmentPath,
			wantSource: config.FileFromEnvironment,
		},
		{
			name: "working directory before system",
			options: config.FileSearchOptions{
				WorkingDirectory: working,
				SystemPath:       systemPath,
			},
			wantPath:   workingPath,
			wantSource: config.FileFromWorkingDirectory,
		},
		{
			name: "system fallback",
			options: config.FileSearchOptions{
				WorkingDirectory: emptyWorking,
				SystemPath:       systemPath,
			},
			wantPath:   systemPath,
			wantSource: config.FileFromSystem,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.ResolveFile(tc.options)
			if err != nil {
				t.Fatalf("ResolveFile() error = %v", err)
			}
			if got.Path != tc.wantPath || got.Source != tc.wantSource {
				t.Fatalf("ResolveFile() = %#v, want path %q source %q", got, tc.wantPath, tc.wantSource)
			}
		})
	}
}

func TestUNIT001_ResolveFileDoesNotFallThroughAnExplicitMissingPath(t *testing.T) {
	t.Parallel()

	working := t.TempDir()
	writeConfigCandidate(t, working, "conduit.yaml")
	missing := filepath.Join(working, "explicitly-missing.yaml")
	tests := []struct {
		name    string
		options config.FileSearchOptions
		source  string
	}{
		{
			name: "flag",
			options: config.FileSearchOptions{
				FlagPath:         missing,
				WorkingDirectory: working,
			},
			source: "flag --config",
		},
		{
			name: "environment",
			options: config.FileSearchOptions{
				Environment:      map[string]string{"CONDUIT_CONFIG": missing},
				WorkingDirectory: working,
			},
			source: "environment CONDUIT_CONFIG",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.ResolveFile(tc.options)
			if err == nil {
				t.Fatal("ResolveFile() error = nil, want missing explicit file error")
			}
			if !reflect.DeepEqual(got, config.FileLocation{}) {
				t.Fatalf("ResolveFile() returned partial location on failure: %#v", got)
			}
			assertValidationError(t, err, config.PhaseFileParse, "config", tc.source, "existing readable configuration file")
		})
	}
}

func TestUNIT001_ResolveFileReportsExhaustedSearchWithoutPartialResult(t *testing.T) {
	t.Parallel()

	emptyWorking := t.TempDir()
	missingSystem := filepath.Join(emptyWorking, "missing-system.yaml")
	got, err := config.ResolveFile(config.FileSearchOptions{
		WorkingDirectory: emptyWorking,
		SystemPath:       missingSystem,
	})
	if err == nil {
		t.Fatal("ResolveFile() error = nil, want exhausted search error")
	}
	if !reflect.DeepEqual(got, config.FileLocation{}) {
		t.Fatalf("ResolveFile() returned partial location on failure: %#v", got)
	}
	assertValidationError(
		t,
		err,
		config.PhaseFileParse,
		"config",
		"configuration search",
		"configuration file at --config, CONDUIT_CONFIG, ./conduit.yaml, or /etc/conduit/conduit.yaml",
	)
}

func writeConfigCandidate(t *testing.T, directory, name string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("listener: {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
