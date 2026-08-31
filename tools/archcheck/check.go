package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Violation is one forbidden package edge, missing required edge, or forbidden
// language construct. Package is the module-qualified importing package.
type Violation struct {
	Rule    string
	Package string
	Target  string
	Reason  string
}

func (violation Violation) String() string {
	return fmt.Sprintf("%s: %s -> %s: %s", violation.Rule, violation.Package, violation.Target, violation.Reason)
}

// CheckModule loads and executes the checked-in architecture configuration.
func CheckModule(ctx context.Context, root string) ([]Violation, error) {
	rules, inventory, err := loadDefaultPolicy()
	if err != nil {
		return nil, err
	}
	return checkModuleWithPolicy(ctx, root, rules, &inventory)
}

func checkModuleWithRules(ctx context.Context, root string, rules []Rule) ([]Violation, error) {
	return checkModuleWithPolicy(ctx, root, rules, nil)
}

func checkModuleWithPolicy(ctx context.Context, root string, rules []Rule, inventory *sinkOwnerInventory) ([]Violation, error) {
	if err := validateRules(rules); err != nil {
		return nil, fmt.Errorf("invalid architecture rules: %w", err)
	}
	graph, err := loadModuleGraph(ctx, root)
	if err != nil {
		return nil, err
	}
	if inventory != nil {
		if err := validateSinkOwnerCompleteness(graph, *inventory); err != nil {
			return nil, err
		}
	}
	if err := validateRequiredPackages(graph, rules); err != nil {
		return nil, err
	}

	collector := newViolationCollector()
	for _, pkg := range graph.Packages {
		imports := make([]observedTarget, 0, len(pkg.Imports))
		for _, imported := range pkg.Imports {
			relative, moduleImport := moduleRelative(imported, graph.Module.Path)
			key := imported
			if moduleImport {
				key = relative
			}
			imports = append(imports, observedTarget{key: key, display: imported})
		}
		for _, target := range imports {
			evaluateTarget(pkg, target, rules, collector)
		}
		for _, target := range syntaxTargets(pkg) {
			evaluateTarget(pkg, target, rules, collector)
		}
		productionImports := make([]observedTarget, 0, len(pkg.ProductionImports))
		for _, imported := range pkg.ProductionImports {
			relative, moduleImport := moduleRelative(imported, graph.Module.Path)
			key := imported
			if moduleImport {
				key = relative
			}
			productionImports = append(productionImports, observedTarget{key: key, display: imported})
		}
		evaluateRequiredImports(pkg, productionImports, rules, collector)
	}
	return collector.sorted(), nil
}

func validateSinkOwnerCompleteness(graph moduleGraph, inventory sinkOwnerInventory) error {
	if err := validateSinkOwnerDefinition(inventory); err != nil {
		return fmt.Errorf("sink-owner inventory: %w", err)
	}
	packages := make(map[string]packageNode, len(graph.Packages))
	for _, pkg := range graph.Packages {
		packages[pkg.Relative] = pkg
	}
	owners := make(map[string]struct{}, len(inventory.Owners))
	for _, owner := range inventory.Owners {
		owners[owner] = struct{}{}
	}
	for _, candidate := range inventory.Candidates {
		pkg, exists := packages[candidate]
		if !exists {
			return fmt.Errorf("sink-owner inventory: candidate %q does not exist in the module graph", candidate)
		}
		_, declaredOwner := owners[candidate]
		active := productionPackageActive(pkg)
		if active && !declaredOwner {
			return fmt.Errorf("sink-owner inventory: active candidate %q is omitted from owners", candidate)
		}
		if !active && declaredOwner {
			return fmt.Errorf("sink-owner inventory: owner %q is doc.go-only and not an active sink", candidate)
		}
	}
	return nil
}

func productionPackageActive(pkg packageNode) bool {
	for _, name := range pkg.ProductionFiles {
		if filepath.Base(name) != "doc.go" {
			return true
		}
	}
	return false
}

type observedTarget struct {
	key     string
	display string
}

func evaluateTarget(pkg packageNode, target observedTarget, rules []Rule, collector *violationCollector) {
	for _, rule := range rules {
		packageMatches := mustMatchSelector(pkg.Relative, rule.Package)
		if matchesAny(target.key, rule.MayImport) && !packageMatches {
			collector.add(rule, pkg.ImportPath, target.display)
		}
		if packageMatches && matchesAny(target.key, rule.MustNotImport) {
			collector.add(rule, pkg.ImportPath, target.display)
		}
	}
}

func evaluateRequiredImports(pkg packageNode, imports []observedTarget, rules []Rule, collector *violationCollector) {
	for _, rule := range rules {
		if len(rule.MustImport) == 0 || !mustMatchSelector(pkg.Relative, rule.Package) {
			continue
		}
		for _, required := range rule.MustImport {
			found := false
			for _, imported := range imports {
				if mustMatchPattern(imported.key, required) {
					found = true
					break
				}
			}
			if !found {
				collector.add(rule, pkg.ImportPath, required)
			}
		}
	}
}

func validateRequiredPackages(graph moduleGraph, rules []Rule) error {
	packages := make(map[string]struct{}, len(graph.Packages))
	for _, pkg := range graph.Packages {
		packages[pkg.Relative] = struct{}{}
	}
	for _, rule := range rules {
		if len(rule.MustImport) == 0 {
			continue
		}
		patterns, _ := expandAlternatives(rule.Package)
		for _, pattern := range patterns {
			if pattern == "!**" {
				continue
			}
			if strings.HasPrefix(pattern, "!") || strings.ContainsAny(pattern, "*?") {
				return fmt.Errorf("rule %s: MustImport requires exact package owners, got %q", rule.ID, pattern)
			}
			if _, ok := packages[pattern]; !ok {
				return fmt.Errorf("rule %s: declared package %q does not exist in the module graph", rule.ID, pattern)
			}
		}
	}
	return nil
}

func validateRules(rules []Rule) error {
	seen := make(map[string]struct{}, len(rules))
	for index, rule := range rules {
		if rule.ID == "" || rule.Package == "" || rule.Reason == "" {
			return fmt.Errorf("rule %d has an empty ID, Package, or Reason", index)
		}
		if _, duplicate := seen[rule.ID]; duplicate {
			return fmt.Errorf("duplicate rule ID %q", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if len(rule.MayImport)+len(rule.MustNotImport)+len(rule.MustImport) == 0 {
			return fmt.Errorf("rule %s has no executable targets", rule.ID)
		}
		selectorPatterns, err := expandAlternatives(rule.Package)
		if err != nil {
			return fmt.Errorf("rule %s Package: %w", rule.ID, err)
		}
		positiveSelectors := 0
		for _, pattern := range selectorPatterns {
			if !strings.HasPrefix(pattern, "!") {
				positiveSelectors++
			}
		}
		if positiveSelectors == 0 && (len(selectorPatterns) != 1 || selectorPatterns[0] != "!**") {
			return fmt.Errorf("rule %s Package must contain a positive selector", rule.ID)
		}
		if _, err := matchSelector("internal/example", rule.Package); err != nil {
			return fmt.Errorf("rule %s Package: %w", rule.ID, err)
		}
		for _, target := range append(append(append([]string(nil), rule.MayImport...), rule.MustNotImport...), rule.MustImport...) {
			if strings.HasPrefix(target, "!") {
				return fmt.Errorf("rule %s target %q must not be negated", rule.ID, target)
			}
			patterns, err := expandAlternatives(target)
			if err != nil {
				return fmt.Errorf("rule %s target %q: %w", rule.ID, target, err)
			}
			for _, pattern := range patterns {
				if _, err := matchPattern("validation/probe", pattern); err != nil {
					return fmt.Errorf("rule %s target %q: %w", rule.ID, target, err)
				}
			}
		}
	}
	return nil
}

type violationCollector struct {
	byKey map[string]Violation
}

func newViolationCollector() *violationCollector {
	return &violationCollector{byKey: make(map[string]Violation)}
}

func (collector *violationCollector) add(rule Rule, packagePath, target string) {
	violation := Violation{Rule: rule.ID, Package: packagePath, Target: target, Reason: rule.Reason}
	key := violation.Rule + "\x00" + violation.Package + "\x00" + violation.Target
	collector.byKey[key] = violation
}

func (collector *violationCollector) sorted() []Violation {
	violations := make([]Violation, 0, len(collector.byKey))
	for _, violation := range collector.byKey {
		violations = append(violations, violation)
	}
	sort.Slice(violations, func(i, j int) bool {
		left, right := violations[i], violations[j]
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

func syntaxTargets(pkg packageNode) []observedTarget {
	observed := make(map[string]string)
	for _, file := range pkg.Files {
		aliases, dotImports := importAliases(file)
		if hasBuildConstraint(file) {
			observed["syntax:build-constraint"] = "//go:build"
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.SelectorExpr:
				identifier, ok := expression.X.(*ast.Ident)
				if !ok {
					return true
				}
				if imported := aliases[identifier.Name]; imported != "" {
					name := imported + "." + expression.Sel.Name
					observed["syntax:selector:"+name] = name
				}
			case *ast.CallExpr:
				selector, ok := expression.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				if imported := aliases[identifier.Name]; imported != "" {
					name := imported + "." + selector.Sel.Name
					observed["syntax:call:"+name] = name
				}
			}
			return true
		})
		for imported := range dotImports {
			for _, name := range unresolvedIdentifiers(file) {
				qualified := imported + "." + name
				observed["syntax:selector:"+qualified] = qualified
			}
			for _, name := range unresolvedCalls(file) {
				qualified := imported + "." + name
				observed["syntax:call:"+qualified] = qualified
			}
		}
	}
	keys := make([]string, 0, len(observed))
	for key := range observed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]observedTarget, 0, len(keys))
	for _, key := range keys {
		result = append(result, observedTarget{key: key, display: observed[key]})
	}
	return result
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

func unresolvedIdentifiers(file *ast.File) []string {
	used := make(map[string]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok && identifier.Obj == nil {
			used[identifier.Name] = struct{}{}
		}
		return true
	})
	return sortedSet(used)
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
	return sortedSet(called)
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
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

func matchesAny(value string, patterns []string) bool {
	for _, pattern := range patterns {
		for _, expanded := range mustExpand(pattern) {
			if mustMatchPattern(value, expanded) {
				return true
			}
		}
	}
	return false
}

func matchSelector(value, selector string) (bool, error) {
	patterns, err := expandAlternatives(selector)
	if err != nil {
		return false, err
	}
	hasPositive := false
	matched := false
	for _, candidate := range patterns {
		negated := strings.HasPrefix(candidate, "!")
		pattern := strings.TrimPrefix(candidate, "!")
		if pattern == "" {
			return false, fmt.Errorf("empty negated pattern")
		}
		ok, err := matchPattern(value, pattern)
		if err != nil {
			return false, err
		}
		if negated {
			if ok {
				return false, nil
			}
			continue
		}
		hasPositive = true
		matched = matched || ok
	}
	if !hasPositive {
		return false, nil
	}
	return matched, nil
}

func matchPattern(value, pattern string) (bool, error) {
	if pattern == "**" {
		return true, nil
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if prefix == "" {
			return false, fmt.Errorf("invalid /** pattern")
		}
		return value == prefix || strings.HasPrefix(value, prefix+"/"), nil
	}
	return path.Match(pattern, value)
}

func expandAlternatives(expression string) ([]string, error) {
	open := strings.IndexByte(expression, '{')
	if open < 0 {
		if strings.ContainsRune(expression, '}') || expression == "" {
			return nil, fmt.Errorf("malformed pattern %q", expression)
		}
		return []string{expression}, nil
	}
	depth, close := 0, -1
	for index := open; index < len(expression); index++ {
		switch expression[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				close = index
				index = len(expression)
			}
		}
	}
	if close < 0 {
		return nil, fmt.Errorf("unclosed brace in pattern %q", expression)
	}
	inner := expression[open+1 : close]
	parts := strings.Split(inner, ",")
	if len(parts) < 2 {
		return nil, fmt.Errorf("brace in pattern %q must contain alternatives", expression)
	}
	var expanded []string
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty alternative in pattern %q", expression)
		}
		values, err := expandAlternatives(expression[:open] + part + expression[close+1:])
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, values...)
	}
	return expanded, nil
}

func mustExpand(pattern string) []string {
	expanded, err := expandAlternatives(pattern)
	if err != nil {
		panic(err)
	}
	return expanded
}

func mustMatchPattern(value, pattern string) bool {
	matched, err := matchPattern(value, pattern)
	if err != nil {
		panic(err)
	}
	return matched
}

func mustMatchSelector(value, selector string) bool {
	matched, err := matchSelector(value, selector)
	if err != nil {
		panic(err)
	}
	return matched
}
