# Dependency review: `github.com/agnivade/levenshtein` `v1.2.1`

Review status: accepted for R1.02 on 2026-08-30. This record answers
BUILD_PLAN §4.6 and OPERATIONS_TEST_PLAN §5.2 for the transitive production
dependency introduced when Conduit made gqlparser's schema validator
reachable.

## Decision

- **Capability:** gqlparser's validator uses edit distance only to rank typo
  suggestions in validation errors. Conduit does not import this module
  directly and does not use it for validity, routing, authorization, or
  execution decisions.
- **Pin:** `github.com/agnivade/levenshtein v1.2.1`, module sum
  `h1:EHBY3UOn1gwdy/VbFwgo4cxecRznFk7fKWN1KOX7eoM=`. The tag resolves to
  commit `813c5d3147488182a4d0d6aea81fc9f28d330cc1`, dated 2024-12-23.
- **Maintenance signal:** GitHub reports that the repository is not archived,
  was last pushed on 2026-03-03, and has two open issues. The contributor API
  reports 52 contributions from `agnivade`, six each from `dchapes` and
  `psadac`, five from `jub0bs`, and two each from `hanzei` and `zhyon404`.
  There are no GitHub Release objects; the reviewed immutable module tag and
  checksum are therefore the release evidence. Ownership is concentrated and
  remains a monitored risk.
- **Advisory check:** GitHub's global security-advisory API returned no entry
  affecting `github.com/agnivade/levenshtein@v1.2.1` on 2026-08-30. The pinned
  `govulncheck` job remains the release-blocking reachability check; this
  point-in-time query is not proof against future advisories.

## Closure, licensing, and confinement

- The reachable package imports only the standard library's `unicode/utf8`.
  Its module declares `github.com/arbovm/levenshtein` and
  `github.com/dgryski/trifles` only for upstream comparison tests; neither is
  reachable or vendored, and both exact versions are disclosed in
  `reviews.json`.
- The vendored tree is 20 KiB and five files. It is pure Go: no C, assembly,
  cgo directive, native library, subprocess, network client, or runtime
  service is introduced. The retained `License.txt` is MIT, an automatically
  allowed project license.
- No Conduit package imports this module. Reachability is confined to
  `gqlparser/validator/core`, itself reachable only through
  `internal/graphql/ast`; `tools/depsaudit` and `tools/archcheck` enforce the
  outer gqlparser boundary.
- The implementation works on runes and uses a `uint16` dynamic-programming
  row; upstream documents a 65,536-rune accuracy limit. Conduit's aggregate
  4 MiB pre-parse bound limits resource use, and edit distance affects only
  optional diagnostic suggestions, never schema acceptance.
- `reviews.json` records the deterministic `sha256-framed-tree-v1` digest
  `sha256:992b61494dc700e30aaace3e3ddd94b0bbef31a3d7e0736d72562d3a66a077f2`
  over every sorted vendored path, permission mode, and file body.

## Upgrade and removal evidence

An upgrade reruns the complete R1.02 SDL corpus under the race detector,
`govulncheck`, dependency/license audit, and architecture confinement checks.
Removal is low difficulty: Conduit can pin a gqlparser validator variant
without suggestion ranking or replace that small ranking helper while its own
diagnostic rule codes and ordering remain stable.
