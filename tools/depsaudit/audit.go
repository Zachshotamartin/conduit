package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var spdxPattern = regexp.MustCompile(`(?im)SPDX-License-Identifier:[[:space:]]*([^[:space:]]+)`)

// AuditOptions names the module and gate-state inputs.
type AuditOptions struct {
	ModuleRoot     string
	GateStatusPath string
}

// Finding is one deterministic dependency-policy failure.
type Finding struct {
	Kind    string
	Module  string
	Package string
	Path    string
	Message string
}

func (finding Finding) String() string {
	return fmt.Sprintf("%s: %s %s (%s): %s", finding.Kind, finding.Module, finding.Package, finding.Path, finding.Message)
}

type moduleInfo struct {
	Path     string
	Version  string
	Main     bool
	Indirect bool
	Dir      string
	Replace  *moduleInfo
}

type listedPackage struct {
	ImportPath string
	Imports    []string
	Module     *moduleInfo
}

type moduleGraph struct {
	Main               moduleInfo
	Modules            map[string]moduleInfo
	RuntimeModules     map[string]bool
	TestModules        map[string]bool
	PackageModules     map[string]string
	MainPackageImports map[string][]string
}

// Audit inspects the real Go module/package graph, then applies direct
// runtime allowlisting, gate timing, package confinement, vendoring, and
// vendored-license policy.
func Audit(ctx context.Context, options AuditOptions) ([]Finding, error) {
	root, err := filepath.Abs(options.ModuleRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve module root: %w", err)
	}
	graph, err := loadGraph(ctx, root)
	if err != nil {
		return nil, err
	}
	statuses, err := readStatuses(options.GateStatusPath)
	if err != nil {
		return nil, err
	}
	highestStarted := startedFrontier(statuses)

	policies := make(map[string]DependencyPolicy, len(runtimePolicies))
	for _, policy := range runtimePolicies {
		policies[policy.Module] = policy
	}

	var findings []Finding
	for _, modulePath := range directRuntimeModules(graph) {
		policy, approved := policies[modulePath]
		if !approved {
			findings = append(findings, Finding{
				Kind: "unapproved-direct-runtime", Module: modulePath, Path: filepath.Join(root, "go.mod"),
				Message: "direct runtime module is not in the normative dependency budget",
			})
			continue
		}
		earliest, _ := gateNumber(policy.EarliestGate)
		if earliest > highestStarted {
			findings = append(findings, Finding{
				Kind: "dependency-before-gate", Module: modulePath, Path: filepath.Join(root, "go.mod"),
				Message: fmt.Sprintf("dependency is owned by %s, but the highest started gate is R%d", policy.EarliestGate, highestStarted),
			})
		}
	}

	mainPackages := make([]string, 0, len(graph.MainPackageImports))
	for packagePath := range graph.MainPackageImports {
		mainPackages = append(mainPackages, packagePath)
	}
	sort.Strings(mainPackages)
	for _, packagePath := range mainPackages {
		relative := relativePackage(packagePath, graph.Main.Path)
		for _, imported := range graph.MainPackageImports[packagePath] {
			modulePath := graph.PackageModules[imported]
			policy, approved := policies[modulePath]
			if !approved {
				continue
			}
			if !contains(policy.AllowedPackages, relative) {
				findings = append(findings, Finding{
					Kind: "wrong-package", Module: modulePath, Package: packagePath, Path: packagePath,
					Message: fmt.Sprintf("runtime module may be imported only by %s", strings.Join(policy.AllowedPackages, ", ")),
				})
			}
		}
	}

	manifestPath := filepath.Join(root, "vendor", "modules.txt")
	vendored, err := readVendorManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	modulePaths := auditedModules(graph)
	findings = append(findings, vendorVersionFindings(graph, vendored, manifestPath)...)
	for _, modulePath := range modulePaths {
		if _, ok := vendored[modulePath]; !ok {
			findings = append(findings, Finding{
				Kind: "missing-vendor", Module: modulePath, Path: manifestPath,
				Message: "module graph entry is not pinned in vendor/modules.txt",
			})
			continue
		}
		licensePath, licenseID, err := identifyVendoredLicense(filepath.Join(root, "vendor", filepath.FromSlash(modulePath)))
		if err != nil {
			findings = append(findings, Finding{
				Kind: "missing-license", Module: modulePath, Path: filepath.Join(root, "vendor", filepath.FromSlash(modulePath)),
				Message: err.Error(),
			})
			continue
		}
		if contains(licenseAllowlist, licenseID) {
			continue
		}
		if licenseID == "MPL-2.0" {
			findings = append(findings, Finding{
				Kind: "review-required-license", Module: modulePath, Path: licensePath,
				Message: "MPL-2.0 requires explicit maintainer review recorded in the dependency PR",
			})
			continue
		}
		findings = append(findings, Finding{
			Kind: "forbidden-license", Module: modulePath, Path: licensePath,
			Message: fmt.Sprintf("license %s is not on the allowlist", licenseID),
		})
	}

	sortFindings(findings)
	return deduplicateFindings(findings), nil
}

// AuditRepository applies conventional repository paths.
func AuditRepository(ctx context.Context, root string) ([]Finding, error) {
	return Audit(ctx, AuditOptions{
		ModuleRoot:     root,
		GateStatusPath: filepath.Join(root, "docs", "gate-status.json"),
	})
}

func loadGraph(ctx context.Context, root string) (moduleGraph, error) {
	mainModule, modules, err := readGoMod(filepath.Join(root, "go.mod"))
	if err != nil {
		return moduleGraph{}, err
	}

	mode, err := packageGraphModuleMode(root)
	if err != nil {
		return moduleGraph{}, err
	}
	packageOutput, stderr, err := runGoList(ctx, root, mode, false)
	if err != nil {
		return moduleGraph{}, fmt.Errorf("load runtime package graph: %w%s", err, stderrSuffix(stderr))
	}
	graph := moduleGraph{
		Main:               mainModule,
		Modules:            modules,
		RuntimeModules:     make(map[string]bool),
		TestModules:        make(map[string]bool),
		PackageModules:     make(map[string]string),
		MainPackageImports: make(map[string][]string),
	}
	decoder := json.NewDecoder(bytes.NewReader(packageOutput))
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			return moduleGraph{}, fmt.Errorf("decode runtime package graph: %w", err)
		}
		if pkg.Module != nil {
			graph.PackageModules[pkg.ImportPath] = pkg.Module.Path
			if !pkg.Module.Main {
				graph.RuntimeModules[pkg.Module.Path] = true
				ensureModule(graph.Modules, *pkg.Module)
			}
			if pkg.Module.Main {
				imports := append([]string(nil), pkg.Imports...)
				sort.Strings(imports)
				graph.MainPackageImports[pkg.ImportPath] = imports
			}
		}
	}

	testOutput, testStderr, err := runGoList(ctx, root, mode, true)
	if err != nil {
		return moduleGraph{}, fmt.Errorf("load test package graph: %w%s", err, stderrSuffix(testStderr))
	}
	decoder = json.NewDecoder(bytes.NewReader(testOutput))
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			return moduleGraph{}, fmt.Errorf("decode test package graph: %w", err)
		}
		if pkg.Module != nil && !pkg.Module.Main {
			graph.TestModules[pkg.Module.Path] = true
			ensureModule(graph.Modules, *pkg.Module)
		}
	}
	return graph, nil
}

func packageGraphModuleMode(root string) (string, error) {
	manifest := filepath.Join(root, "vendor", "modules.txt")
	info, err := os.Stat(manifest)
	if err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("vendor manifest %q is not a regular file", manifest)
		}
		return "vendor", nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat vendor manifest %q: %w", manifest, err)
	}
	return "readonly", nil
}

func runGoList(ctx context.Context, root, mode string, tests bool) ([]byte, []byte, error) {
	args := []string{"list", "-mod=" + mode}
	if tests {
		args = append(args, "-test")
	}
	args = append(args, "-deps", "-json", "./...")
	return runGo(ctx, root, args...)
}

func ensureModule(modules map[string]moduleInfo, module moduleInfo) {
	if existing, ok := modules[module.Path]; ok {
		module.Indirect = existing.Indirect
		if module.Version == "" {
			module.Version = existing.Version
		}
	}
	modules[module.Path] = module
}

func readGoMod(path string) (moduleInfo, map[string]moduleInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return moduleInfo{}, nil, fmt.Errorf("read go.mod %s: %w", path, err)
	}
	modules := make(map[string]moduleInfo)
	var mainModule moduleInfo
	inRequireBlock := false
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			mainModule = moduleInfo{Path: fields[1], Main: true, Dir: filepath.Dir(path)}
			continue
		}
		if line == "require (" {
			inRequireBlock = true
			continue
		}
		if inRequireBlock && line == ")" {
			inRequireBlock = false
			continue
		}
		if !inRequireBlock {
			if len(fields) < 3 || fields[0] != "require" {
				continue
			}
			fields = fields[1:]
		}
		if len(fields) < 2 {
			return moduleInfo{}, nil, fmt.Errorf("parse go.mod %s: malformed require line %q", path, rawLine)
		}
		modules[fields[0]] = moduleInfo{
			Path: fields[0], Version: fields[1], Indirect: strings.Contains(line, "// indirect"),
		}
	}
	if mainModule.Path == "" {
		return moduleInfo{}, nil, fmt.Errorf("go.mod %s has no module directive", path)
	}
	modules[mainModule.Path] = mainModule
	return mainModule, modules, nil
}

func runGo(ctx context.Context, root string, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func stderrSuffix(stderr []byte) string {
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		return ""
	}
	return ": " + message
}

func readStatuses(path string) (map[int]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read gate statuses %s: %w", path, err)
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode gate statuses %s: %w", path, err)
	}
	statuses := make(map[int]string, 11)
	for gate := 0; gate <= 10; gate++ {
		name := fmt.Sprintf("R%d", gate)
		status, ok := raw[name]
		if !ok {
			return nil, fmt.Errorf("gate statuses %s omit %s", path, name)
		}
		switch status {
		case "planned", "in progress", "accepted", "deferred":
		default:
			return nil, fmt.Errorf("gate statuses %s give %s invalid status %q", path, name, status)
		}
		statuses[gate] = status
	}
	return statuses, nil
}

func startedFrontier(statuses map[int]string) int {
	highest := -1
	for gate, status := range statuses {
		if (status == "accepted" || status == "in progress") && gate > highest {
			highest = gate
		}
	}
	return highest
}

func gateNumber(value string) (int, bool) {
	if len(value) < 2 || value[0] != 'R' {
		return 0, false
	}
	number, err := strconv.Atoi(value[1:])
	return number, err == nil && number >= 0 && number <= 10
}

func readVendorManifest(path string) (map[string]string, error) {
	vendored := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return vendored, nil
		}
		return nil, fmt.Errorf("read vendor manifest %s: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "#" || !looksLikeVersion(fields[2]) {
			continue
		}
		modulePath := fields[1]
		version := fields[2]
		if previous, duplicate := vendored[modulePath]; duplicate && previous != version {
			return nil, fmt.Errorf("vendor manifest %s records conflicting versions for %s: %s and %s", path, modulePath, previous, version)
		}
		vendored[modulePath] = version
	}
	return vendored, nil
}

func looksLikeVersion(value string) bool {
	return strings.HasPrefix(value, "v") && len(value) > 1
}

func identifyVendoredLicense(moduleRoot string) (string, string, error) {
	entries, err := os.ReadDir(moduleRoot)
	if err != nil {
		return "", "", fmt.Errorf("vendored module directory is unreadable: %w", err)
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		upper := strings.ToUpper(entry.Name())
		if strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "COPYING") {
			candidates = append(candidates, filepath.Join(moduleRoot, entry.Name()))
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("vendored module has no root LICENSE or COPYING file")
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", "", fmt.Errorf("read vendored license %s: %w", path, err)
		}
		if id := classifyLicense(data); id != "" {
			return path, id, nil
		}
	}
	return candidates[0], "Unknown", nil
}

func classifyLicense(data []byte) string {
	text := string(data)
	if match := spdxPattern.FindStringSubmatch(text); match != nil {
		return strings.TrimSpace(match[1])
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "gnu affero general public license"):
		return "AGPL"
	case strings.Contains(lower, "gnu lesser general public license"):
		return "LGPL"
	case strings.Contains(lower, "gnu general public license"):
		return "GPL"
	case strings.Contains(lower, "server side public license"):
		return "SSPL"
	case strings.Contains(lower, "mozilla public license") && strings.Contains(lower, "version 2.0"):
		return "MPL-2.0"
	case strings.Contains(lower, "apache license") && strings.Contains(lower, "version 2.0"):
		return "Apache-2.0"
	case strings.Contains(lower, "permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(lower, "permission to use, copy, modify, and/or distribute this software for any purpose with or without fee"):
		return "ISC"
	case strings.Contains(lower, "redistribution and use in source and binary forms") && strings.Contains(lower, "neither the name"):
		return "BSD-3-Clause"
	case strings.Contains(lower, "redistribution and use in source and binary forms"):
		return "BSD-2-Clause"
	default:
		return "Unknown"
	}
}

func auditedModules(graph moduleGraph) []string {
	set := make(map[string]bool, len(graph.RuntimeModules)+len(graph.TestModules))
	for path := range graph.RuntimeModules {
		set[path] = true
	}
	for path := range graph.TestModules {
		set[path] = true
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func vendorVersionFindings(graph moduleGraph, vendored map[string]string, manifestPath string) []Finding {
	var findings []Finding
	for _, modulePath := range auditedModules(graph) {
		vendoredVersion, ok := vendored[modulePath]
		if !ok {
			continue
		}
		selected, ok := graph.Modules[modulePath]
		if !ok || selected.Version == "" {
			findings = append(findings, Finding{
				Kind: "vendor-version-unresolved", Module: modulePath, Path: manifestPath,
				Message: "runtime/test package graph did not report a selected module version",
			})
			continue
		}
		if vendoredVersion != selected.Version {
			findings = append(findings, Finding{
				Kind: "vendor-version-mismatch", Module: modulePath, Path: manifestPath,
				Message: fmt.Sprintf("module graph selects %s, but vendor/modules.txt records %s", selected.Version, vendoredVersion),
			})
		}
	}
	return findings
}

// directRuntimeModules derives the direct dependency boundary from package
// edges rather than go.mod comments. The // indirect marker is maintained by
// the Go command and is not a trustworthy authorization boundary: a source
// package can import such a module directly while the stale marker remains.
func directRuntimeModules(graph moduleGraph) []string {
	set := make(map[string]bool)
	for _, imports := range graph.MainPackageImports {
		for _, imported := range imports {
			modulePath := graph.PackageModules[imported]
			if modulePath == "" || modulePath == graph.Main.Path {
				continue
			}
			set[modulePath] = true
		}
	}
	paths := make([]string, 0, len(set))
	for modulePath := range set {
		paths = append(paths, modulePath)
	}
	sort.Strings(paths)
	return paths
}

func relativePackage(packagePath, modulePath string) string {
	if packagePath == modulePath {
		return "."
	}
	return strings.TrimPrefix(packagePath, modulePath+"/")
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Module != right.Module {
			return left.Module < right.Module
		}
		if left.Package != right.Package {
			return left.Package < right.Package
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Message < right.Message
	})
}

func deduplicateFindings(findings []Finding) []Finding {
	if len(findings) < 2 {
		return findings
	}
	result := findings[:1]
	for _, finding := range findings[1:] {
		if finding == result[len(result)-1] {
			continue
		}
		result = append(result, finding)
	}
	return result
}
