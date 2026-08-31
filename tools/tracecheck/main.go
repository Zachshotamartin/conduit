package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Run executes trace-check without terminating the process.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tracecheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	testPaths := flags.String("tests", "", "comma-separated test roots (defaults to repository root)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "tracecheck: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	tests := []string{*root}
	if strings.TrimSpace(*testPaths) != "" {
		tests = nil
		for _, value := range strings.Split(*testPaths, ",") {
			value = strings.TrimSpace(value)
			if value != "" {
				tests = append(tests, value)
			}
		}
	}
	options := CheckOptions{
		ProductRequirementsPath: filepath.Join(*root, "docs", "PRODUCT_REQUIREMENTS.md"),
		BuildPlanPath:           filepath.Join(*root, "docs", "BUILD_PLAN.md"),
		OperationsTestPlanPath:  filepath.Join(*root, "docs", "OPERATIONS_TEST_PLAN.md"),
		GateStatusPath:          filepath.Join(*root, "docs", "gate-status.json"),
		OwnershipPath:           filepath.Join(*root, "docs", "requirement-ownership.json"),
		TestRoots:               tests,
	}
	violations, err := Check(ctx, options)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "tracecheck: %v\n", err)
		return 2
	}
	if len(violations) != 0 {
		for _, violation := range violations {
			_, _ = fmt.Fprintln(stderr, violation.String())
		}
		return 1
	}

	_, _ = fmt.Fprintln(stdout, "tracecheck: no violations")
	return 0
}

func main() {
	os.Exit(Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
