package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/Zachshotamartin/conduit/internal/config"
)

// Run executes the R0 command surface without terminating the process.
// validate performs only bounded file reads and pure phases 1 through 3; it
// never opens a listener or mutates external state.
func Run(
	ctx context.Context,
	args []string,
	environment map[string]string,
	stdout io.Writer,
	stderr io.Writer,
) ExitCode {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if err := ctx.Err(); err != nil {
		_, _ = fmt.Fprintf(stderr, "conduit: %v\n", err)
		return codeOrFallback("fatal", 1)
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: conduit validate [--config path]")
		return codeOrFallback("fatal", 1)
	}

	switch args[0] {
	case "validate":
		return runValidate(ctx, args[1:], environment, stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "conduit: unsupported command %q\n", args[0])
		return codeOrFallback("fatal", 1)
	}
}

func runValidate(
	ctx context.Context,
	args []string,
	environment map[string]string,
	stdout io.Writer,
	stderr io.Writer,
) ExitCode {
	failureCode := codeOrFallback("validate_failure", 2)
	flags := flag.NewFlagSet("conduit validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "configuration file path")
	if err := flags.Parse(args); err != nil {
		return failureCode
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "conduit validate: unexpected arguments: %v\n", flags.Args())
		return failureCode
	}
	if err := ctx.Err(); err != nil {
		_, _ = fmt.Fprintf(stderr, "conduit: %v\n", err)
		return codeOrFallback("fatal", 1)
	}

	location, err := config.ResolveFile(config.FileSearchOptions{
		FlagPath:    *configPath,
		Environment: environment,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return failureCode
	}
	if err := ctx.Err(); err != nil {
		_, _ = fmt.Fprintf(stderr, "conduit: %v\n", err)
		return codeOrFallback("fatal", 1)
	}

	if _, err := config.Load(config.LoadOptions{
		FilePath:    location.Path,
		Environment: environment,
	}); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return failureCode
	}
	if err := ctx.Err(); err != nil {
		_, _ = fmt.Fprintf(stderr, "conduit: %v\n", err)
		return codeOrFallback("fatal", 1)
	}

	_, _ = fmt.Fprintln(stdout, "configuration valid")
	return 0
}

func codeOrFallback(reason ExitReason, fallback ExitCode) ExitCode {
	if code, ok := ExitCodeFor(reason); ok {
		return code
	}
	return fallback
}
