package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Violation is one forbidden package edge or forbidden language construct.
// Package is the module-qualified importing package; Target is either an
// import path or the construct that crossed the boundary.
type Violation struct {
	Rule    string
	Package string
	Target  string
	Reason  string
}

// String includes the stable rule ID and its human-readable reason so CI
// output explains the design constraint rather than merely naming an edge.
func (violation Violation) String() string {
	return fmt.Sprintf("%s: %s -> %s: %s", violation.Rule, violation.Package, violation.Target, violation.Reason)
}

// CheckModule inspects the Go package graph rooted at root and applies every
// normative architecture rule. Package discovery and direct import edges come
// from go list metadata. Go syntax is parsed only for constraints that cannot
// be represented as import edges: build constraints and selected direct calls.
func CheckModule(ctx context.Context, root string) ([]Violation, error) {
	graph, err := loadModuleGraph(ctx, root)
	if err != nil {
		return nil, err
	}

	collector := newViolationCollector()
	for _, pkg := range graph.Packages {
		for _, imported := range pkg.Imports {
			checkImport(graph.Module.Path, pkg, imported, collector)
		}
		checkSyntax(pkg, collector)
	}
	return collector.sorted(), nil
}

type violationCollector struct {
	byKey map[string]Violation
}

func newViolationCollector() *violationCollector {
	return &violationCollector{byKey: make(map[string]Violation)}
}

func (collector *violationCollector) add(ruleID, packagePath, target string) {
	rule := rulesByID[ruleID]
	violation := Violation{
		Rule:    rule.ID,
		Package: packagePath,
		Target:  target,
		Reason:  rule.Reason,
	}
	key := violation.Rule + "\x00" + violation.Package + "\x00" + violation.Target
	collector.byKey[key] = violation
}

func (collector *violationCollector) sorted() []Violation {
	violations := make([]Violation, 0, len(collector.byKey))
	for _, violation := range collector.byKey {
		violations = append(violations, violation)
	}
	sort.Slice(violations, func(i, j int) bool {
		left := violations[i]
		right := violations[j]
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		if left.Package != right.Package {
			return left.Package < right.Package
		}
		return left.Target < right.Target
	})
	return violations
}

func checkImport(modulePath string, pkg packageNode, imported string, collector *violationCollector) {
	pkgRelative := pkg.Relative
	importRelative, moduleImport := moduleRelative(imported, modulePath)
	testPackage := within(pkgRelative, "test")

	if !testPackage && importPrefix(imported, "github.com/coder/websocket") && pkgRelative != "internal/transport" {
		collector.add("ARCH-01", pkg.ImportPath, imported)
	}
	if !testPackage && importPrefix(imported, "github.com/vektah/gqlparser/v2") && pkgRelative != "internal/graphql/ast" {
		collector.add("ARCH-02", pkg.ImportPath, imported)
	}
	if !testPackage && importPrefix(imported, "github.com/nats-io/nats.go") && pkgRelative != "internal/bus/nats" {
		collector.add("ARCH-03", pkg.ImportPath, imported)
	}
	if !testPackage && importPrefix(imported, "github.com/jackc/pgx/v5") && pkgRelative != "internal/datasource/postgres" {
		collector.add("ARCH-04", pkg.ImportPath, imported)
	}

	if moduleImport && (within(pkgRelative, "internal/transport") || within(pkgRelative, "internal/protocol")) && within(importRelative, "internal/graphql") {
		collector.add("ARCH-05", pkg.ImportPath, imported)
	}
	if moduleImport && within(pkgRelative, "internal/graphql") && anyWithin(importRelative, "internal/transport", "internal/protocol", "internal/queue") {
		collector.add("ARCH-06", pkg.ImportPath, imported)
	}
	if moduleImport && (within(pkgRelative, "internal/transport") || within(pkgRelative, "internal/protocol")) && within(importRelative, "internal/admin") {
		collector.add("ARCH-08", pkg.ImportPath, imported)
	}
	if moduleImport && isAdapter(importRelative) && !adapterImportAllowed(pkgRelative, importRelative) {
		collector.add("ARCH-09", pkg.ImportPath, imported)
	}

	telemetryImport := importPrefix(imported, "go.opentelemetry.io/otel") ||
		importPrefix(imported, "go.opentelemetry.io/contrib") ||
		importPrefix(imported, "github.com/prometheus/client_golang")
	if !testPackage && telemetryImport && pkgRelative != "internal/observability" && pkgRelative != "cmd/conduit" {
		collector.add("ARCH-10", pkg.ImportPath, imported)
	}
	if moduleImport && within(pkgRelative, "internal") && within(importRelative, "test") {
		collector.add("ARCH-11", pkg.ImportPath, imported)
	}
	if moduleImport && within(pkgRelative, "cmd/conduit-loadgen") && anyWithin(importRelative, "internal/registry", "internal/fanout", "internal/queue") {
		collector.add("ARCH-13", pkg.ImportPath, imported)
	}
	if moduleImport && within(pkgRelative, "internal/protocol") && within(importRelative, "internal/datasource") {
		collector.add("ARCH-14", pkg.ImportPath, imported)
	}
	if moduleImport && anyWithin(pkgRelative, "internal/queue", "internal/registry") && within(importRelative, "internal/bus") {
		collector.add("ARCH-15", pkg.ImportPath, imported)
	}
	if moduleImport && within(pkgRelative, "internal/admin") && within(importRelative, "internal/transport") {
		collector.add("ARCH-16", pkg.ImportPath, imported)
	}
}

func checkSyntax(pkg packageNode, collector *violationCollector) {
	platformPackage := within(pkg.Relative, "internal/platform")
	domainPackage := within(pkg.Relative, "internal") && !within(pkg.Relative, "internal/clock")

	for _, file := range pkg.Files {
		aliases, dotImports := importAliases(file)
		if !platformPackage && hasBuildConstraint(file) {
			collector.add("ARCH-07", pkg.ImportPath, "//go:build")
		}

		ast.Inspect(file, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.SelectorExpr:
				identifier, ok := expression.X.(*ast.Ident)
				if !ok {
					return true
				}
				imported := aliases[identifier.Name]
				if !platformPackage && imported == "runtime" && expression.Sel.Name == "GOOS" {
					collector.add("ARCH-07", pkg.ImportPath, "runtime.GOOS")
				}
				if domainPackage && imported == "time" && (expression.Sel.Name == "Now" || expression.Sel.Name == "After") {
					collector.add("ARCH-12", pkg.ImportPath, "time."+expression.Sel.Name)
				}
			case *ast.CallExpr:
				selector, ok := expression.Fun.(*ast.SelectorExpr)
				if !ok || !domainPackage {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				imported := aliases[identifier.Name]
				if imported == "math/rand" || imported == "crypto/rand" {
					collector.add("ARCH-12", pkg.ImportPath, imported+"."+selector.Sel.Name)
				}
			}
			return true
		})

		if !platformPackage && dotImports["runtime"] && unresolvedIdentifierUsed(file, "GOOS") {
			collector.add("ARCH-07", pkg.ImportPath, "runtime.GOOS")
		}
		if domainPackage && dotImports["time"] {
			if unresolvedIdentifierUsed(file, "Now") {
				collector.add("ARCH-12", pkg.ImportPath, "time.Now")
			}
			if unresolvedIdentifierUsed(file, "After") {
				collector.add("ARCH-12", pkg.ImportPath, "time.After")
			}
		}
		if domainPackage {
			for _, imported := range []string{"math/rand", "crypto/rand"} {
				if !dotImports[imported] {
					continue
				}
				for _, function := range unresolvedCalls(file) {
					collector.add("ARCH-12", pkg.ImportPath, imported+"."+function)
				}
			}
		}
	}
}

func importAliases(file *ast.File) (map[string]string, map[string]bool) {
	aliases := make(map[string]string)
	dotImports := make(map[string]bool)
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if spec.Name != nil {
			switch spec.Name.Name {
			case "_":
				continue
			case ".":
				dotImports[imported] = true
				continue
			default:
				aliases[spec.Name.Name] = imported
				continue
			}
		}
		aliases[path.Base(imported)] = imported
	}
	return aliases, dotImports
}

func hasBuildConstraint(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := comment.Text
			if !strings.HasPrefix(text, "//go:build ") && !strings.HasPrefix(text, "// +build ") {
				continue
			}
			if _, err := constraint.Parse(text); err == nil {
				return true
			}
		}
	}
	return false
}

func unresolvedIdentifierUsed(file *ast.File, name string) bool {
	used := false
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && identifier.Name == name && identifier.Obj == nil {
			used = true
			return false
		}
		return !used
	})
	return used
}

func unresolvedCalls(file *ast.File) []string {
	called := make(map[string]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if identifier, ok := call.Fun.(*ast.Ident); ok && identifier.Obj == nil {
			called[identifier.Name] = struct{}{}
		}
		return true
	})
	result := make([]string, 0, len(called))
	for name := range called {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func moduleRelative(imported, modulePath string) (string, bool) {
	if imported == modulePath {
		return ".", true
	}
	if strings.HasPrefix(imported, modulePath+"/") {
		return strings.TrimPrefix(imported, modulePath+"/"), true
	}
	return imported, false
}

func importPrefix(imported, prefix string) bool {
	return imported == prefix || strings.HasPrefix(imported, prefix+"/")
}

func within(value, prefix string) bool {
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}

func anyWithin(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if within(value, prefix) {
			return true
		}
	}
	return false
}

func isAdapter(relative string) bool {
	return anyWithin(
		relative,
		"internal/datasource/postgres",
		"internal/datasource/http",
		"internal/datasource/function",
		"internal/bus/nats",
		"internal/bus/memory",
		"internal/auth/oidc",
		"internal/auth/apikey",
		"internal/auth/custom",
	)
}

func adapterImportAllowed(importer, adapter string) bool {
	if within(importer, "test") || within(importer, "cmd/conduit") {
		return true
	}
	return within(importer, "cmd/conduit-loadgen") && within(adapter, "internal/bus/memory")
}
