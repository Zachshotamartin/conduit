# Dependency review: `github.com/vektah/gqlparser/v2` `v2.5.36`

Review status: accepted through R1.02 on 2026-08-30. This record answers
BUILD_PLAN §4.6 and OPERATIONS_TEST_PLAN §5.2; acceptance is limited to
bounded GraphQL lexing, parsing, and final schema compilation in
`internal/graphql/ast`. Conduit retains its accumulating October-2021 SDL
rules, immutable metadata, semantic hashing, operation policy, and executor.

## Decision

- **Capability:** Go's standard library has no GraphQL lexer or parser.
  Conduit needs a spec-aware parser, but deliberately retains its executor,
  limits, authorization hooks, and subscription machinery. gqlparser supplies
  only the non-differentiating grammar/AST capability selected by ADR-0003.
- **Pin:** `github.com/vektah/gqlparser/v2 v2.5.36`, module sum
  `h1:CN9mKVHgMkc+XftdOWIhb4HEL8wKSYkFAqhf8booa7s=`. The annotated tag resolves
  to commit `57521eafd79fa1b1cf81fa270ea6c832233e4827`, tagged and released on
  2026-07-01.
- **Maintenance signal:** the repository is not archived and was last pushed
  on 2026-08-17. Six releases appeared between 2025-10-30 and 2026-07-01,
  including three in June/July 2026. Human contribution history is
  concentrated but not single-person: GitHub reported 130 contributions from
  `vektah`, 58 from `vvakame`, 43 from `StevenACoffman`, 18 from `lwc`, and 13
  from `Code-Hex` (the 87-entry Dependabot account is excluded from this
  bus-factor read). Release publication is currently concentrated in
  `StevenACoffman`; maintainer concentration remains a monitored risk.
- **Advisory check:** GitHub's global security-advisory API returned no entry
  affecting `github.com/vektah/gqlparser/v2@v2.5.36` on 2026-08-30. The pinned
  `govulncheck` job remains the release-blocking reachability check; this
  point-in-time query is not proof against future advisories.

## Closure, licensing, and confinement

- R1.02 imports gqlparser's `ast`, `gqlerror`, `lexer`, `parser`, and
  `validator` packages. The validator's `core` package makes
  `github.com/agnivade/levenshtein v1.2.1` reachable for typo suggestions; it
  has its own accepted review and vendored digest. `go mod graph` also records
  testify, go-spew, go-difflib, kr/pretty, go.yaml.in/yaml/v3,
  gopkg.in/check.v1, kr/text, creack/pty, arbovm/levenshtein, and
  dgryski/trifles as non-reachable upstream test closure. Every retained
  non-reachable module/version is disclosed in `reviews.json`.
- gqlparser, levenshtein, testify, kr/pretty, kr/text, and creack/pty carry MIT
  terms; go-spew carries ISC; go-difflib carries BSD-3-Clause; gocheck carries
  BSD-2-Clause; and go.yaml.in/yaml/v3 carries MIT and Apache-2.0 terms. Every
  identifier is on Conduit's allowlist. `reviews.json` discloses every
  non-reachable module/version retained in `go.sum`; the validator import in
  R1.02 must promote any newly reachable module to a full accepted review and
  vendored digest.
- Only `internal/graphql/ast` imports gqlparser. The parser representation is
  held behind opaque Conduit `Schema` and `Operation` types. `tools/depsaudit`
  enforces this package confinement, and `tools/archcheck` enforces the same
  architecture boundary over the repository import graph.
- Conduit carries one reviewed, minimal patch to the vendored v2.5.36 parser.
  `parseInterfaceTypeExtension` now parses the October-2021
  `implements` clause and counts it when checking that an extension is
  nonempty. `parseInputObjectTypeExtension` now parses directives in constant
  mode, rejecting variables where SDL requires `Const` values. Both defects
  have checked-in boundary fixtures. No public API, validator rule, executor,
  or dependency version is changed, and no `replace` directive or undisclosed
  fork is used. A future gqlparser upgrade must either contain equivalent
  upstream fixes or reapply and re-review this exact patch.
- The vendored, linked gqlparser subset is 404 KiB on disk and contains 63
  files. Both that subset and the 2.2 MiB upstream module tree are pure Go:
  no C, assembly, cgo directive, native library, subprocess, or runtime
  service is introduced.
- `reviews.json` records the deterministic `sha256-framed-tree-v1` digest
  `sha256:5dd86c970802423ad4805caff83647f1e07e22e9e728ca3d80fcd1a23f3e72d4`
  over every sorted vendored path, permission mode, and file body. Any source,
  local patch, grammar fixture, or license change therefore fails the offline
  dependency audit.
- Removal difficulty is moderate. The vendor AST is confined to one package,
  so callers are insulated, but a replacement must preserve source positions,
  GraphQL grammar behavior, the bounded-input corpus, and later R1 execution
  fixtures byte-for-byte. No gqlparser executor, transport, or framework API
  is admitted.

## Upgrade and removal evidence

Every upgrade or replacement runs UNIT-003 under the race detector and the
checked-in fuzz seed corpus. The suite proves exact byte, token, and
syntactic-depth boundaries, pre-parse allocation ceilings, ignored-token
behavior, safe typed errors, no partial operations or schemas, logical source
name confinement, all-file diagnostics, immutable snapshots, directive
semantics, and stable semantic hashes. The complete SDL corpus is mandatory:
gqlparser is fail-fast, mutates its schema AST during validation, and carries
post-October-2021 `@defer`/`@oneOf` definitions, so Conduit runs its own frozen,
accumulating validation before using gqlparser only as the final compiler
guard. The dependency audit, architecture check, vet, redaction canaries,
docs-status lint, and claims lint remain required beside that corpus.
