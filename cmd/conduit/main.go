package main

import (
	"context"
	"os"
	"strings"
)

func main() {
	code := Run(
		context.Background(),
		os.Args[1:],
		environmentMap(os.Environ()),
		os.Stdout,
		os.Stderr,
	)
	os.Exit(int(code))
}

func environmentMap(entries []string) map[string]string {
	environment := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		environment[name] = value
	}
	return environment
}
