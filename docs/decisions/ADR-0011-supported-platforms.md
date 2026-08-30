# ADR-0011: Supported Platforms — Linux Production, macOS Development

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-OPS-001, FR-OPS-002, NFR-COMPAT-004,
  NFR-SCALE-001, gate R10

## Context

Every supported platform multiplies the CI matrix, the file-descriptor and
socket-option code paths, the packaging work, and — most expensively — the
benchmark obligations, because a scale claim is only earned on the platform
it was measured on. The 50,000-connection target depends on Linux-specific
tuning (epoll behavior, `somaxconn`, ephemeral port and fd limits, TCP
keepalive socket options) that has no exact Windows or BSD equivalent.

## Decision

- **Tier 1 (production, benchmarked, release-blocking CI)**: Linux amd64 and
  Linux arm64, glibc and musl (static binaries make the distinction mostly
  moot), delivered as static binaries and OCI container images. The R9 scale
  claim is measured on a named Linux configuration only
  (BENCHMARK_PLAN §environment).
- **Tier 2 (development, tested, not benchmarked)**: macOS arm64. Unit,
  protocol, and integration suites run in CI; no performance or scale claim
  attaches to macOS results, and the kqueue-based connection ceiling is
  explicitly undocumented.
- **Unsupported in v1**: Windows (native), FreeBSD, 32-bit anything. Windows
  users run the Linux container. Recorded in OPEN_QUESTIONS with a reopen
  trigger (a supported-user request tied to a deployment that cannot run
  containers).

Platform-conditional code is confined to `internal/platform` behind build
tags; no other package may contain `runtime.GOOS` checks (architecture test
enforced from R0).

## Alternatives Considered

- **Windows native support**: rejected for v1; IOCP-tuned socket handling is
  real porting work, the self-hosted gateway audience overwhelmingly deploys
  on Linux, and an unbenchmarked Tier 1 platform would force either dishonest
  claims or a doubled benchmark program.
- **Benchmarking macOS**: rejected; developer laptops are not deployment
  targets and publishing numbers from them invites exactly the unearned
  claims the rules forbid.

## Consequences

The CI matrix is Linux amd64 + arm64 (release-blocking) and macOS arm64
(blocking for correctness suites only). Cross-compilation from any developer
machine produces all Tier 1 artifacts (Go toolchain property; verified in
R10 packaging tests). Adding a platform later is an ADR plus the full
benchmark obligation for any performance claim on it.
