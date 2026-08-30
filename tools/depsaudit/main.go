package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Run executes deps-audit without terminating the process.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("depsaudit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "Go module root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "depsaudit: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	findings, err := Audit(ctx, AuditOptions{
		ModuleRoot:     *root,
		GateStatusPath: filepath.Join(*root, "docs", "gate-status.json"),
	})
	if err != nil {
		fmt.Fprintf(stderr, "depsaudit: %v\n", err)
		return 2
	}
	if len(findings) != 0 {
		for _, finding := range findings {
			fmt.Fprintln(stderr, finding.String())
		}
		return 1
	}

	fmt.Fprintln(stdout, "depsaudit: no findings")
	return 0
}

func main() {
	os.Exit(Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
