# R0 local macOS validation evidence

Document status: in progress.

- Date: 2026-08-30
- Validation SHA: `380806dba4797fccd828c1124a7bc4b31722c651`
- Host: macOS 26.6.2 (25G83), arm64
- Scope: local developer-machine evidence for R0 only; this is not the clean
  macOS/Linux CI evidence required for gate acceptance

## Toolchain

The validation used Go 1.26.7 on `darwin/arm64`. `make bootstrap` verified
each repository-local tool's module pin and embedded compiler version:

| Tool | Module version | Compiler |
| --- | --- | --- |
| Staticcheck | `honnef.co/go/tools v0.8.1` | `go1.26.7` |
| golangci-lint | `github.com/golangci/golangci-lint/v2 v2.13.2` | `go1.26.7` |
| govulncheck | `golang.org/x/vuln v1.7.0` | `go1.26.7` |
| benchstat | `golang.org/x/perf v0.0.0-20260825160852-19be9d8e6c70` | `go1.26.7` |
| syft | `github.com/anchore/syft v1.51.1` | `go1.26.7` |
| cosign | `github.com/sigstore/cosign/v2 v2.6.5` | `go1.26.7` |

Bootstrap also reported Docker 28.1.1 and a file-descriptor soft limit of
1,048,575, which exceeds the 8,192 socket-harness prerequisite.

## Passing local commands

The following commands exited zero at the validation SHA:

```sh
make check-gh
make bootstrap
make check
make test
make build
make conformance
make integration
make bench-smoke
go test -mod=vendor -race -count=10 ./tools/cicontract
actionlint .github/workflows/*.yml
GOFLAGS=-mod=vendor ./bin/govulncheck ./...
git diff --check
```

`make check` reported no architecture, determinism, dependency, trace, or
CI-contract findings; golangci-lint reported zero issues; and the CI contract
verified 15 protected contexts. `make test` ran the current deterministic
suite with the race detector and shuffle enabled. The pinned vulnerability
scanner reported `No vulnerabilities found.`

The R0 `conformance`, `integration`, and `bench-smoke` targets currently
exercise scaffold packages only. Their zero exits are command-wiring smoke
evidence, not protocol conformance, external-service integration, performance,
or product-capability evidence.

## Outstanding acceptance evidence

R0 remains `in progress`. The following acceptance conditions are not yet
satisfied:

- private-repository branch protection cannot be applied on the current
  account tier; the exact HTTP 403 and unblock commands are recorded in
  [branch-protection.md](branch-protection.md);
- `nightly.yml` has not completed a scheduled run. GitHub evaluates scheduled
  workflows from the default branch, while this workflow is still on the
  unmerged `gate/r0` branch. A manual dispatch can validate the workflow but
  cannot substitute for the required scheduled-run evidence.

The clean cross-platform runs and deliberate-failure/recovery proof are
recorded in [github-actions.md](github-actions.md).

No R0 claim is earned and no later gate may start from this evidence.
