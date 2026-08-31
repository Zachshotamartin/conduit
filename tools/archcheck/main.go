package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

// Run executes the command without terminating the process, making the exact
// CI behavior available to tests.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("archcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "Go module root to inspect")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "archcheck: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	violations, err := CheckModule(ctx, *root)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archcheck: %v\n", err)
		return 2
	}
	if len(violations) != 0 {
		for _, violation := range violations {
			_, _ = fmt.Fprintln(stderr, violation.String())
		}
		return 1
	}

	_, _ = fmt.Fprintln(stdout, "archcheck: no violations")
	return 0
}

func main() {
	os.Exit(Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
