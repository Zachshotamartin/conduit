#!/bin/sh
set -eu

go_command="${GO:-go}"
repository_root="$(git rev-parse --show-toplevel)"
binary_directory="$repository_root/bin"

staticcheck_version="v0.5.1"
golangci_lint_version="v1.62.2"
govulncheck_version="v1.1.4"
benchstat_version="v0.0.0-20241204221936-711ff2ab7231"
syft_version="v1.18.1"
cosign_version="v2.4.1"

require_go_version() {
  actual_version="$($go_command version | awk '{print $3}')"
  if [ "$actual_version" != "go1.23.12" ]; then
    printf 'Go toolchain mismatch: got %s, require go1.23.12\n' "$actual_version" >&2
    exit 1
  fi
}

embedded_module_version() {
  binary_path="$1"
  module_path="$2"
  "$go_command" version -m "$binary_path" | awk -v module="$module_path" '$1 == "mod" && $2 == module { print $3 }'
}

install_tool() {
  binary_name="$1"
  package_path="$2"
  module_path="$3"
  module_version="$4"
  binary_path="$binary_directory/$binary_name"

  installed_version=""
  if [ -x "$binary_path" ]; then
    installed_version="$(embedded_module_version "$binary_path" "$module_path")"
  fi
  if [ "$installed_version" != "$module_version" ]; then
    printf 'installing %s@%s\n' "$package_path" "$module_version"
    GOBIN="$binary_directory" "$go_command" install "$package_path@$module_version"
  fi

  installed_version="$(embedded_module_version "$binary_path" "$module_path")"
  if [ "$installed_version" != "$module_version" ]; then
    printf '%s embedded module version is %s, require %s\n' "$binary_name" "$installed_version" "$module_version" >&2
    exit 1
  fi
  printf 'verified %s (%s@%s)\n' "$binary_name" "$module_path" "$module_version"
}

report_container_runtime() {
  if command -v docker >/dev/null 2>&1; then
    printf 'container runtime: %s\n' "$(docker --version)"
    return
  fi
  if command -v podman >/dev/null 2>&1; then
    printf 'container runtime: %s\n' "$(podman --version)"
    return
  fi
  printf '%s\n' 'container runtime: unavailable (required only for conformance and integration suites)'
}

report_file_limit() {
  descriptor_limit="$(ulimit -n)"
  printf 'file descriptor soft limit: %s' "$descriptor_limit"
  case "$descriptor_limit" in
    unlimited)
      printf '%s\n' ' (meets socket-harness prerequisite)'
      ;;
    *[!0-9]*|'')
      printf '%s\n' ' (could not evaluate numeric socket-harness prerequisite)'
      ;;
    *)
      if [ "$descriptor_limit" -ge 8192 ]; then
        printf '%s\n' ' (meets socket-harness prerequisite)'
      else
        printf '%s\n' ' (below 8192; deterministic suites remain available)'
      fi
      ;;
  esac
}

require_go_version
mkdir -p "$binary_directory"
"$go_command" mod download
install_tool staticcheck honnef.co/go/tools/cmd/staticcheck honnef.co/go/tools "$staticcheck_version"
install_tool golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint github.com/golangci/golangci-lint "$golangci_lint_version"
install_tool govulncheck golang.org/x/vuln/cmd/govulncheck golang.org/x/vuln "$govulncheck_version"
install_tool benchstat golang.org/x/perf/cmd/benchstat golang.org/x/perf "$benchstat_version"
install_tool syft github.com/anchore/syft/cmd/syft github.com/anchore/syft "$syft_version"
install_tool cosign github.com/sigstore/cosign/v2/cmd/cosign github.com/sigstore/cosign/v2 "$cosign_version"
report_container_runtime
report_file_limit
