// Command determinismcheck rejects direct wall-clock timers and unseeded
// package-level randomness from Go tests.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var forbiddenTimeFunctions = map[string]bool{
	"After":     true,
	"AfterFunc": true,
	"NewTicker": true,
	"NewTimer":  true,
	"Now":       true,
	"Since":     true,
	"Sleep":     true,
	"Tick":      true,
	"Until":     true,
}

var allowedMathRandConstructors = map[string]bool{
	"New":        true,
	"NewChaCha8": true,
	"NewPCG":     true,
	"NewSource":  true,
	"NewZipf":    true,
}

// Violation identifies one forbidden nondeterministic symbol in a test.
type Violation struct {
	Path   string
	Line   int
	Symbol string
}

func (violation Violation) String() string {
	return fmt.Sprintf("%s:%d: deterministic tests must not call %s", violation.Path, violation.Line, violation.Symbol)
}

// Check inspects every Go test below root using Go syntax, so import aliases
// and dot imports cannot bypass the policy.
func Check(root string) ([]Violation, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root %q is not a directory", absRoot)
	}

	var violations []Violation
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != absRoot && skippedDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		fileViolations, err := checkFile(path)
		if err != nil {
			return err
		}
		violations = append(violations, fileViolations...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		if violations[i].Line != violations[j].Line {
			return violations[i].Line < violations[j].Line
		}
		return violations[i].Symbol < violations[j].Symbol
	})
	return deduplicate(violations), nil
}

func checkFile(path string) ([]Violation, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	aliases := make(map[string]string)
	dotImports := make(map[string]bool)
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("parse import in %s: %w", path, err)
		}
		name := filepath.Base(imported)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		switch name {
		case "_":
			continue
		case ".":
			dotImports[imported] = true
		default:
			aliases[name] = imported
		}
	}

	seen := make(map[string]bool)
	var violations []Violation
	add := func(node ast.Node, symbol string) {
		line := fileSet.Position(node.Pos()).Line
		key := strconv.Itoa(line) + "\x00" + symbol
		if seen[key] {
			return
		}
		seen[key] = true
		violations = append(violations, Violation{Path: path, Line: line, Symbol: symbol})
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch expression := node.(type) {
		case *ast.SelectorExpr:
			identifier, ok := expression.X.(*ast.Ident)
			if !ok || identifier.Obj != nil {
				return true
			}
			imported := aliases[identifier.Name]
			symbol := expression.Sel.Name
			switch imported {
			case "time":
				if forbiddenTimeFunctions[symbol] {
					add(expression, "time."+symbol)
				}
			case "math/rand", "math/rand/v2":
				if !allowedMathRandConstructors[symbol] {
					add(expression, imported+"."+symbol)
				}
			case "crypto/rand":
				add(expression, "crypto/rand."+symbol)
			}
		case *ast.CallExpr:
			identifier, ok := expression.Fun.(*ast.Ident)
			if !ok || identifier.Obj != nil {
				return true
			}
			name := identifier.Name
			if dotImports["time"] && forbiddenTimeFunctions[name] {
				add(expression, "time."+name)
			}
			for _, imported := range []string{"math/rand", "math/rand/v2"} {
				if dotImports[imported] && !allowedMathRandConstructors[name] {
					add(expression, imported+"."+name)
				}
			}
			if dotImports["crypto/rand"] {
				add(expression, "crypto/rand."+name)
			}
		case *ast.Ident:
			if expression.Obj == nil && expression.Name == "Reader" && dotImports["crypto/rand"] {
				add(expression, "crypto/rand.Reader")
			}
		}
		return true
	})
	return violations, nil
}

func skippedDirectory(name string) bool {
	switch name {
	case ".git", "bin", "dist", "node_modules", "testdata", "vendor":
		return true
	default:
		return false
	}
}

func deduplicate(violations []Violation) []Violation {
	if len(violations) < 2 {
		return violations
	}
	result := violations[:1]
	for _, violation := range violations[1:] {
		if violation == result[len(result)-1] {
			continue
		}
		result = append(result, violation)
	}
	return result
}

// Run executes the checker without terminating the process.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("determinismcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "determinismcheck: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	violations, err := Check(*root)
	if err != nil {
		fmt.Fprintf(stderr, "determinismcheck: %v\n", err)
		return 2
	}
	for _, violation := range violations {
		fmt.Fprintln(stderr, violation.String())
	}
	if len(violations) != 0 {
		return 1
	}
	fmt.Fprintln(stdout, "determinismcheck: no violations")
	return 0
}

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
