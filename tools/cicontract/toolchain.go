package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const toolchainPolicyCode = "toolchain.pin"

type toolPin struct {
	name       string
	variable   string
	version    string
	installRun string
}

var bootstrapToolPins = []toolPin{
	{
		name:       "staticcheck",
		variable:   "staticcheck_version",
		version:    "v0.8.1",
		installRun: `install_tool staticcheck honnef.co/go/tools/cmd/staticcheck honnef.co/go/tools "$staticcheck_version"`,
	},
	{
		name:       "golangci-lint",
		variable:   "golangci_lint_version",
		version:    "v2.13.2",
		installRun: `install_tool golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint github.com/golangci/golangci-lint/v2 "$golangci_lint_version"`,
	},
	{
		name:       "govulncheck",
		variable:   "govulncheck_version",
		version:    "v1.7.0",
		installRun: `install_tool govulncheck golang.org/x/vuln/cmd/govulncheck golang.org/x/vuln "$govulncheck_version"`,
	},
	{
		name:       "benchstat",
		variable:   "benchstat_version",
		version:    "v0.0.0-20260825160852-19be9d8e6c70",
		installRun: `install_tool benchstat golang.org/x/perf/cmd/benchstat golang.org/x/perf "$benchstat_version"`,
	},
	{
		name:       "syft",
		variable:   "syft_version",
		version:    "v1.51.1",
		installRun: `install_tool syft github.com/anchore/syft/cmd/syft github.com/anchore/syft "$syft_version"`,
	},
	{
		name:       "cosign",
		variable:   "cosign_version",
		version:    "v2.6.5",
		installRun: `install_tool cosign github.com/sigstore/cosign/v2/cmd/cosign github.com/sigstore/cosign/v2 "$cosign_version"`,
	},
}

var makeCheckGoTargets = []string{
	"bootstrap", "build", "test", "check", "lint", "staticcheck", "vet",
	"arch-check", "docs-status-lint", "metrics-contract", "deps-audit",
	"trace-check", "ci-contract", "unit-race", "proto-race", "authz-race",
	"index-race", "conformance", "integration", "bench-smoke",
}

func validateToolchainPolicy(report *Report, root string) {
	validateGoModuleToolchain(report, root)
	validateMakeToolchain(report, root)
	validateBootstrapToolchain(report, root)
}

func validateGoModuleToolchain(report *Report, root string) {
	const file = "go.mod"
	lines, ok := readPolicyLines(report, root, file)
	if !ok {
		return
	}

	validateDirective(report, file, lines, "go", "1.23.0")
	validateDirective(report, file, lines, "toolchain", "go1.26.7")
}

func validateDirective(report *Report, file string, lines []string, directive, want string) {
	var got []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != directive {
			continue
		}
		got = append(got, strings.Join(fields[1:], " "))
	}
	if len(got) == 1 && got[0] == want {
		return
	}
	report.add(toolchainPolicyCode, file, directive, fmt.Sprintf("directive values are %q, want exactly one %s %s directive", got, directive, want))
}

func validateMakeToolchain(report *Report, root string) {
	const file = "Makefile"
	lines, ok := readPolicyLines(report, root, file)
	if !ok {
		return
	}

	assignment := "override GOTOOLCHAIN := go1.26.7"
	export := "export GOTOOLCHAIN"
	assignmentIndex := exactLineIndex(lines, assignment)
	exportIndex := exactLineIndex(lines, export)
	if exactLineCount(lines, assignment) != 1 || makeVariableAssignmentCount(lines, "GOTOOLCHAIN") != 1 {
		report.add(toolchainPolicyCode, file, "GOTOOLCHAIN", "GOTOOLCHAIN must have exactly one active assignment: override GOTOOLCHAIN := go1.26.7")
	}
	if exactLineCount(lines, export) != 1 || assignmentIndex < 0 || exportIndex != assignmentIndex+1 {
		report.add(toolchainPolicyCode, file, "GOTOOLCHAIN.export", "export GOTOOLCHAIN must appear exactly once immediately after its immutable assignment")
	}
	for _, target := range makeCheckGoTargets {
		if !makeTargetHasPrerequisite(lines, target, "check-go") {
			report.add(toolchainPolicyCode, file, target, fmt.Sprintf("%s must depend on check-go", target))
		}
	}
	if !makeTargetHasExactRecipe(lines, "bootstrap", `GO="$(GO)" ./scripts/bootstrap.sh`) {
		report.add(toolchainPolicyCode, file, "bootstrap.recipe", `bootstrap must invoke GO="$(GO)" ./scripts/bootstrap.sh exactly once`)
	}
	checkGoBody, ok := makeTargetBody(lines, "check-go")
	checkGoSequence := []string{
		`@actual_version="$$($(GO) version | awk '{print $$3}')"; \`,
		`test "$$actual_version" = "$(GOTOOLCHAIN)" || { \`,
		`printf 'Go toolchain must be $(GOTOOLCHAIN); got %s\n' "$$actual_version" >&2; \`,
		`exit 1; \`,
		"}",
	}
	if !ok || !containsExactSequence(checkGoBody, checkGoSequence) {
		report.add(toolchainPolicyCode, file, "check-go", "check-go must resolve the Go version once and fail unless it exactly matches GOTOOLCHAIN")
	}
}

func validateBootstrapToolchain(report *Report, root string) {
	const file = "scripts/bootstrap.sh"
	lines, ok := readPolicyLines(report, root, file)
	if !ok {
		return
	}

	requireExactShellAssignment(report, file, lines, "required_go_version", "go1.26.7", "go.version")
	requireExactShellAssignment(report, file, lines, "GOTOOLCHAIN", "$required_go_version", "GOTOOLCHAIN")
	forceIndex := exactLineIndex(lines, `GOTOOLCHAIN="$required_go_version"`)
	exportIndex := exactLineIndex(lines, "export GOTOOLCHAIN")
	commandIndex := exactLineIndex(lines, `go_command="${GO:-go}"`)
	if exactLineCount(lines, "export GOTOOLCHAIN") != 1 || forceIndex < 0 || exportIndex != forceIndex+1 || commandIndex < 0 || exportIndex >= commandIndex {
		report.add(toolchainPolicyCode, file, "GOTOOLCHAIN.export", "the exact GOTOOLCHAIN pin must be exported before the Go command is resolved")
	}

	for _, pin := range bootstrapToolPins {
		requireExactShellAssignment(report, file, lines, pin.variable, pin.version, "tool."+pin.name+".version")
		if exactLineCount(lines, pin.installRun) != 1 {
			report.add(toolchainPolicyCode, file, "tool."+pin.name+".install", fmt.Sprintf("tool installation must occur exactly once as %q", pin.installRun))
		}
	}

	requireGoVersionLines := []string{
		`actual_version="$($go_command version | awk '{print $3}')"`,
		`if [ "$actual_version" != "$required_go_version" ]; then`,
		`printf 'Go toolchain mismatch: got %s, require %s\n' "$actual_version" "$required_go_version" >&2`,
		"exit 1",
	}
	validateFunctionContains(report, file, lines, "require_go_version", "go.verify", requireGoVersionLines, nil)
	requireBody, ok := shellFunctionBody(lines, "require_go_version")
	if !ok || !containsExactSequence(requireBody, requireGoVersionLines[1:]) {
		report.add(toolchainPolicyCode, file, "go.verify", "require_go_version must fail immediately when the resolved compiler is not exact")
	}

	embeddedModuleLines := []string{
		`binary_path="$1"`,
		`module_path="$2"`,
		`"$go_command" version -m "$binary_path" | awk -v module="$module_path" '$1 == "mod" && $2 == module { print $3 }'`,
	}
	validateFunctionContains(report, file, lines, "embedded_module_version", "module.extract", embeddedModuleLines, nil)

	embeddedCompilerLines := []string{
		`binary_path="$1"`,
		`"$go_command" version -m "$binary_path" | awk 'NR == 1 { print $2 }'`,
	}
	validateFunctionContains(report, file, lines, "embedded_compiler_version", "compiler.extract", embeddedCompilerLines, nil)

	moduleRead := `installed_version="$(embedded_module_version "$binary_path" "$module_path")"`
	compilerRead := `installed_compiler_version="$(embedded_compiler_version "$binary_path")"`
	reinstallCondition := `if [ "$installed_version" != "$module_version" ] || [ "$installed_compiler_version" != "$required_go_version" ]; then`
	moduleCondition := `if [ "$installed_version" != "$module_version" ]; then`
	compilerCondition := `if [ "$installed_compiler_version" != "$required_go_version" ]; then`
	installRequired := []string{
		`installed_version=""`,
		`installed_compiler_version=""`,
		`GOBIN="$binary_directory" "$go_command" install "$package_path@$module_version"`,
		"exit 1",
	}
	installCounts := map[string]int{
		moduleRead:         2,
		compilerRead:       2,
		reinstallCondition: 1,
		moduleCondition:    1,
		compilerCondition:  1,
		"exit 1":           2,
	}
	validateFunctionContains(report, file, lines, "install_tool", "compiler.verify", installRequired, installCounts)
	installBody, ok := shellFunctionBody(lines, "install_tool")
	installSequences := [][]string{
		{
			`if [ -x "$binary_path" ]; then`,
			moduleRead,
			compilerRead,
			"fi",
		},
		{
			reinstallCondition,
			`printf 'installing %s@%s\n' "$package_path" "$module_version"`,
			`GOBIN="$binary_directory" "$go_command" install "$package_path@$module_version"`,
			"fi",
		},
		{moduleRead, compilerRead},
		{
			moduleCondition,
			`printf '%s embedded module version is %s, require %s\n' "$binary_name" "$installed_version" "$module_version" >&2`,
			"exit 1",
			"fi",
		},
		{
			compilerCondition,
			`printf '%s compiler build version is %s, require %s\n' "$binary_name" "$installed_compiler_version" "$required_go_version" >&2`,
			"exit 1",
			"fi",
		},
	}
	if !ok || !containsAllExactSequences(installBody, installSequences) {
		report.add(toolchainPolicyCode, file, "compiler.verify", "install_tool must reinstall and fail closed on either module-pin or embedded-compiler drift")
	}

	requireIndex := exactLineIndex(lines, "require_go_version")
	moduleDownloadIndex := exactLineIndex(lines, `"$go_command" mod download`)
	if exactLineCount(lines, "require_go_version") != 1 || requireIndex < 0 || moduleDownloadIndex < 0 || requireIndex >= moduleDownloadIndex {
		report.add(toolchainPolicyCode, file, "go.verify.invoke", "require_go_version must run exactly once before module download or tool installation")
	}
}

func requireExactShellAssignment(report *Report, file string, lines []string, variable, want, path string) {
	expected := variable + `="` + want + `"`
	if exactLineCount(lines, expected) == 1 && shellVariableAssignmentCount(lines, variable) == 1 {
		return
	}
	report.add(toolchainPolicyCode, file, path, fmt.Sprintf("%s must have exactly one active assignment to %q", variable, want))
}

func validateFunctionContains(report *Report, file string, lines []string, function, path string, required []string, counts map[string]int) {
	body, ok := shellFunctionBody(lines, function)
	if !ok {
		report.add(toolchainPolicyCode, file, path, fmt.Sprintf("required shell function %s is missing or duplicated", function))
		return
	}
	for _, line := range required {
		if exactLineCount(body, line) == 0 {
			report.add(toolchainPolicyCode, file, path, fmt.Sprintf("%s must contain exact command %q", function, line))
		}
	}
	for line, want := range counts {
		if got := exactLineCount(body, line); got != want {
			report.add(toolchainPolicyCode, file, path, fmt.Sprintf("%s contains %d copies of %q, want exactly %d", function, got, line, want))
		}
	}
}

func containsAllExactSequences(lines []string, sequences [][]string) bool {
	for _, sequence := range sequences {
		if !containsExactSequence(lines, sequence) {
			return false
		}
	}
	return true
}

func containsExactSequence(lines, sequence []string) bool {
	if len(sequence) == 0 {
		return true
	}
	for start := 0; start+len(sequence) <= len(lines); start++ {
		matched := true
		for offset := range sequence {
			if lines[start+offset] != sequence[offset] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func readPolicyLines(report *Report, root, file string) ([]string, bool) {
	contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		report.add(toolchainPolicyCode, file, "", fmt.Sprintf("required policy file cannot be read: %v", err))
		return nil, false
	}
	return activePolicyLines(string(contents)), true
}

func activePolicyLines(contents string) []string {
	rawLines := strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func exactLineCount(lines []string, want string) int {
	count := 0
	for _, line := range lines {
		if line == want {
			count++
		}
	}
	return count
}

func exactLineIndex(lines []string, want string) int {
	for index, line := range lines {
		if line == want {
			return index
		}
	}
	return -1
}

func makeVariableAssignmentCount(lines []string, variable string) int {
	count := 0
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "override" && len(fields) >= 3 && fields[1] == variable {
			count++
			continue
		}
		if fields[0] == variable {
			count++
		}
	}
	return count
}

func shellVariableAssignmentCount(lines []string, variable string) int {
	prefix := variable + "="
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}

func makeTargetHasPrerequisite(lines []string, target, prerequisite string) bool {
	prefix := target + ":"
	for _, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		for _, got := range strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, prefix))) {
			if got == prerequisite {
				return true
			}
		}
	}
	return false
}

func makeTargetHasExactRecipe(lines []string, target, recipe string) bool {
	body, ok := makeTargetBody(lines, target)
	return ok && exactLineCount(body, recipe) == 1
}

func makeTargetBody(lines []string, target string) ([]string, bool) {
	targetPrefix := target + ":"
	start := -1
	for index, line := range lines {
		if !strings.HasPrefix(line, targetPrefix) {
			continue
		}
		if start >= 0 {
			return nil, false
		}
		start = index
	}
	if start < 0 {
		return nil, false
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		line := lines[index]
		colon := strings.IndexByte(line, ':')
		if colon > 0 && !strings.ContainsAny(line[:colon], " \t=\"'") {
			end = index
			break
		}
	}
	return lines[start+1 : end], true
}

func shellFunctionBody(lines []string, function string) ([]string, bool) {
	declaration := function + "() {"
	start := -1
	for index, line := range lines {
		if line != declaration {
			continue
		}
		if start >= 0 {
			return nil, false
		}
		start = index
	}
	if start < 0 {
		return nil, false
	}
	for index := start + 1; index < len(lines); index++ {
		if lines[index] == "}" {
			return lines[start+1 : index], true
		}
	}
	return nil, false
}
