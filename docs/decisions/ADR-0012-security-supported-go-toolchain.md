# ADR-0012: Pin a Security-Supported Go Toolchain

Document status: in progress.

- Decision state: accepted
- Supersedes: ADR-0001 only for the exact Go toolchain version and
  version-specific runtime facts
- Date: 2026-08-30
- Related findings or requirements: NFR-SEC-010, NFR-MAINT-005, FR-OPS-012,
  gate R0

## Context

R0 originally pinned Go 1.23.12. The pinned vulnerability scanner reported
[GO-2026-4602](https://pkg.go.dev/vuln/GO-2026-4602) as reachable through the
Go standard library. Go's [release policy](https://go.dev/doc/devel/release#policy)
supports a major release only until two newer major releases exist. The
[official downloads API](https://go.dev/dl/?mode=json) identifies Go 1.27 as
the current line and Go 1.26.7 as the latest patch of the supported previous
line, so Go 1.23 is outside the security-maintenance window. Keeping the old
pin would make a green dependency audit impossible and would knowingly build
Conduit with an affected, unsupported standard library.

The source does not require newer language semantics. The `go` directive and
the `toolchain` directive serve different purposes: retaining `go 1.23.0`
preserves the module's language-version contract, while
`toolchain go1.26.7` records the preferred compiler, runtime, and standard
library. The directive participates in Go's toolchain selection when that
mechanism is enabled; it is a preference, not by itself an exact-version
enforcement boundary.

This migration also changes the pinned developer tools. Staticcheck v0.5.1
does not build with Go 1.26.7 because its `x/tools` compatibility assertion
fails during compilation. All bootstrap tools therefore need a coordinated,
reviewable pin update rather than an implicit local substitution.

## Decision

Conduit keeps the BUILD_PLAN-required `go 1.23.0` module language version and
records `toolchain go1.26.7` as its preferred toolchain. Exact-version
enforcement belongs to the supported entrypoints: the Makefile and
`scripts/bootstrap.sh` force `GOTOOLCHAIN=go1.26.7`, Go-using Make targets
including `bootstrap` depend on `check-go`, the bootstrap script independently
performs the same exact check, and CI installs Go 1.26.7 under an executable
workflow contract. These boundaries require `go version` to report Go 1.26.7
and fail closed on drift. The GitHub Actions setup action remains pinned by
commit SHA and has `check-latest: false` and `cache: false`.

The developer-tool set is migrated as one coordinated compatibility unit:

- Staticcheck (`honnef.co/go/tools`) v0.8.1;
- golangci-lint (`github.com/golangci/golangci-lint/v2`) v2.13.2;
- govulncheck (`golang.org/x/vuln`) v1.7.0;
- benchstat (`golang.org/x/perf`) v0.0.0-20260825160852-19be9d8e6c70;
- syft (`github.com/anchore/syft`) v1.51.1; and
- cosign (`github.com/sigstore/cosign/v2`) v2.6.5.

ADR-0001 remains controlling for the choice of Go, the connection concurrency
model, and the R9 measurement obligations. This ADR supersedes only its Go
1.23 toolchain assumption and any Go-1.23-specific runtime fact; current
runtime properties must be measured with the pinned toolchain instead of
copied from the earlier version.

## Evidence

- The selected darwin/arm64 archive is `go1.26.7.darwin-arm64.tar.gz`, SHA-256
  `020a1e8224811be75163e920bc77e0926a1390a6aeea19bdcf23f74b9d749f6d`.
- `make bootstrap` must install or verify all six exact tool pins and must
  verify that every resulting binary was compiled by Go 1.26.7.
- The bootstrap/toolchain contract tests must reject drift in any of the six
  version pins, forced `GOTOOLCHAIN` selection, exact `go version` check, or
  embedded module/compiler-version verification. The executable CI contract
  must also reject workflow `setup-go` or direct analyzer/scanner command
  drift.
- `make check`, `make test`, and `govulncheck ./...` must pass under Go 1.26.7
  before R0 can be accepted; the GitHub Actions run URL and vulnerability
  output belong in the R0 evidence bundle.

## Alternatives Considered

- **Keep Go 1.23.12 until a feature requires a language upgrade**: rejected
  because language compatibility does not justify an affected, unsupported
  compiler and standard library.
- **Patch or vendor the affected standard-library implementation**: rejected
  because it creates an unmaintainable private Go distribution and invalidates
  upstream security-version evidence.
- **Raise the module `go` directive to 1.26**: rejected because no source
  requires Go 1.26 language behavior. It would unnecessarily narrow source
  compatibility and couple two independent contracts.
- **Update only Staticcheck**: rejected because every locally installed tool
  is compiled by the pinned Go toolchain. A single reviewed compatibility set
  is reproducible; ad hoc substitutions are not.

## Consequences

Compiler, runtime, analyzer, SBOM, signing, and vulnerability-scanner changes
land together, so the R0 validation chain is larger but reproducible. Existing
benchmark baselines are not portable across this compiler/runtime boundary;
future performance evidence must record Go 1.26.7 and start a new comparison
series.

A rollback may move only to another Go release that is inside the official
support window, is not affected by GO-2026-4602, passes `govulncheck ./...`,
and passes the complete R0 validation chain with compatible exact tool pins.
It requires a new accepted ADR and new CI evidence. Rolling back to Go 1.23.12
or silently weakening the vulnerability check is forbidden. If Go 1.26.7
causes a release-blocking regression, development pauses on the gate while a
security-supported roll-forward or qualified rollback is validated.
