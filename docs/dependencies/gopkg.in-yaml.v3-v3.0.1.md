# Dependency review: `gopkg.in/yaml.v3` `v3.0.1`

Review status: accepted for R0.10 on 2026-08-30. This record answers
BUILD_PLAN §4.6 and OPERATIONS_TEST_PLAN §5.2; acceptance is limited to the
configuration parser in `internal/config`.

## Decision

- **Capability:** Go's standard library has no YAML parser. Conduit needs a
  YAML 1.2-adjacent parser that exposes a syntax tree so file syntax,
  top-level keys, schema types, and source-aware errors can remain separate
  validation phases. The dependency supplies that parser and no gateway or
  framework behavior.
- **Pin:** `gopkg.in/yaml.v3 v3.0.1`, module sum
  `h1:fxVm/GzAzEWqLHuvctI91KS9hhNmmWOoWu0XTYJS7CA=`. The tag resolves to
  commit `f6f7691b1fdeb513f56608cd2c32c51f8194bf51`, dated 2022-05-27.
- **Maintenance signal:** the upstream `go-yaml/yaml` repository is archived
  and its last push was 2025-04-01. Contributor history is concentrated (the
  GitHub contributor view showed 244 contributions from the leading
  contributor and 46 from the next). This is a real maintenance weakness,
  not an active-maintenance claim. The fixed normative dependency budget,
  mature pinned release, narrow confinement, and replacement seam make it
  acceptable for R0; an advisory, parser defect in the config corpus, or a
  required YAML feature reopens the decision and triggers migration review.
- **Advisory check:** GitHub's global security-advisory API returned no entry
  affecting `gopkg.in/yaml.v3@v3.0.1` on 2026-08-30. The pinned
  `govulncheck` job remains the release-blocking reachability check; this API
  result is not treated as proof that future advisories cannot exist.

## Closure, licensing, and confinement

- The runtime graph adds one linked module: `gopkg.in/yaml.v3 v3.0.1`.
  Its declared `gopkg.in/check.v1`
  `v0.0.0-20161208181325-20d25e280405` dependency is test-only: it appears in
  `go.sum`/`go mod graph` but not `vendor/modules.txt` and is not linked into
  Conduit.
- yaml.v3 carries dual MIT and Apache-2.0 terms. check.v1 carries the
  two-clause BSD license. All three identifiers are on the project's license
  allowlist.
- Only `internal/config` imports yaml.v3. `tools/depsaudit` enforces both the
  approved module and this package confinement, and the architecture check
  runs against the resulting repository graph.
- The vendored yaml.v3 tree is 360 KiB on disk and contains 16 files. It is
  pure Go: no C, assembly, cgo directive, native library, subprocess, or
  runtime service is introduced.
- `reviews.json` records the accepted review and the deterministic
  `sha256-framed-tree-v1` digest
  `sha256:8fd099dfecbcd82abaf9556bde54751559cae22ab0b7da8ef411e6e891d01739`
  over every sorted vendored path, permission mode, and file body. It also
  records the upstream-only `gopkg.in/check.v1` dependency and its exact
  version as a non-linked transitive disclosure.
- Removal difficulty is low to moderate. yaml.v3 types are contained within
  `internal/config/load.go`; Conduit's public config tree, validation errors,
  precedence, and CLI do not expose them. A replacement must reproduce the
  checked-in configuration corpus before the import changes.

## Upgrade and removal evidence

Every upgrade or replacement runs UNIT-001 under the race detector. The
corpus covers valid YAML, malformed YAML, unknown top-level keys, wrong
types, ranges, phase ordering, every R0 cross-field rule, schema-derived
environment mapping, source precedence, file search precedence, and the
no-partial-config invariant. UNIT-016 additionally proves deterministic
`conduit validate` failure output and its stable process code. The dependency
audit, architecture check, vet, docs-status lint, and claims lint remain
required alongside that corpus.
