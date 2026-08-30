package config

import (
	"errors"
	"os"
	"path/filepath"
)

// Configuration file selectors and default locations.
const (
	ConfigPathEnvironment = "CONDUIT_CONFIG"
	WorkingDirectoryFile  = "conduit.yaml"
	DefaultSystemPath     = "/etc/conduit/conduit.yaml"
)

// FileSource identifies which search-order entry selected a file.
type FileSource string

// Configuration file sources, in precedence order.
const (
	FileFromFlag             FileSource = "flag"
	FileFromEnvironment      FileSource = "environment"
	FileFromWorkingDirectory FileSource = "working_directory"
	FileFromSystem           FileSource = "system"
)

// FileSearchOptions makes the documented file search deterministic in tests
// while empty working/system fields retain production defaults.
type FileSearchOptions struct {
	FlagPath         string
	Environment      map[string]string
	WorkingDirectory string
	SystemPath       string
}

// FileLocation is the selected configuration path and its source.
type FileLocation struct {
	Path   string
	Source FileSource
}

// ResolveFile applies --config, CONDUIT_CONFIG, working-directory, then
// system search precedence. Explicit missing paths fail instead of falling
// through to a lower-precedence source.
func ResolveFile(options FileSearchOptions) (FileLocation, error) {
	if options.FlagPath != "" {
		return resolveExplicitFile(options.FlagPath, FileFromFlag, "flag --config")
	}
	if path := options.Environment[ConfigPathEnvironment]; path != "" {
		return resolveExplicitFile(path, FileFromEnvironment, "environment CONDUIT_CONFIG")
	}

	workingDirectory := options.WorkingDirectory
	if workingDirectory == "" {
		var err error
		workingDirectory, err = os.Getwd()
		if err != nil {
			return FileLocation{}, newValidationError(
				PhaseFileParse,
				"config",
				"working directory",
				"readable working directory",
				err,
			)
		}
	}
	workingPath := filepath.Join(workingDirectory, WorkingDirectoryFile)
	if readable, err := isReadableFile(workingPath); err != nil {
		return FileLocation{}, newValidationError(
			PhaseFileParse,
			"config",
			workingPath,
			"readable configuration file",
			err,
		)
	} else if readable {
		return FileLocation{Path: workingPath, Source: FileFromWorkingDirectory}, nil
	}

	systemPath := options.SystemPath
	if systemPath == "" {
		systemPath = DefaultSystemPath
	}
	if readable, err := isReadableFile(systemPath); err != nil {
		return FileLocation{}, newValidationError(
			PhaseFileParse,
			"config",
			systemPath,
			"readable configuration file",
			err,
		)
	} else if readable {
		return FileLocation{Path: systemPath, Source: FileFromSystem}, nil
	}

	return FileLocation{}, newValidationError(
		PhaseFileParse,
		"config",
		"configuration search",
		"configuration file at --config, CONDUIT_CONFIG, ./conduit.yaml, or /etc/conduit/conduit.yaml",
		nil,
	)
}

func resolveExplicitFile(path string, source FileSource, sourceName string) (FileLocation, error) {
	readable, err := isReadableFile(path)
	if err != nil || !readable {
		return FileLocation{}, newValidationError(
			PhaseFileParse,
			"config",
			sourceName,
			"existing readable configuration file",
			err,
		)
	}
	return FileLocation{Path: filepath.Clean(path), Source: source}, nil
}

func isReadableFile(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("configuration path is not a regular file")
	}
	return true, nil
}
