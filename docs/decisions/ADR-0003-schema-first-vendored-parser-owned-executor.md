# ADR-0003: Schema-First SDL, Library Parser/Validator, Conduit-Owned Executor

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-GQL-001–FR-GQL-006, FR-AUTH-010,
  FR-FILT-001, gates R1 and R3

## Context

Three coupled decisions: (a) how operators define schemas (schema-first SDL
versus code-first), (b) whether GraphQL parsing and validation are library
code or Conduit code, and (c) whether execution is a library executor or a
Conduit-owned executor. The constraints that matter:

- Conduit is a gateway: schemas are operator-supplied configuration loaded at
  startup, not compiled into the binary. Any code-generation approach
  (gqlgen-style) is structurally incompatible with operator-supplied SDL.
- Publish-time authorization (FR-AUTH-010) and filter extraction (FR-FILT-001)
  require the executor to expose per-field authorization hooks and to compile
  subscription arguments into predicates — capabilities no off-the-shelf Go
  executor exposes at the needed depth.
- Writing a GraphQL parser and validator from scratch is avoidable
  correctness risk: the October 2021 spec's parsing and validation rules are
  large, stable, and fully covered by existing libraries.

## Decision

- **Schema-first**: operators supply SDL files plus a resolver-binding
  configuration mapping fields to data sources. Directives
  (`@source`, `@auth`, `@filterable`, `@backpressure`, `@complexity`) carry
  gateway metadata in the SDL. The SDL and binding config are validated at
  startup; an invalid schema fails startup, never a request.
- **Library parser/validator**: `vektah/gqlparser/v2` (the parser underlying
  gqlgen, spec-complete, maintained) is the single dependency for lexing,
  parsing, and the spec validation rules. It is wrapped behind
  `conduit/graphql/ast` so no other package imports it directly, and it is
  exercised by Conduit's own bounded-input tests (depth, token count, byte
  size) before any document reaches it.
- **Conduit-owned executor**: field collection, execution order, list/null
  propagation, error formatting, data-source dispatch, complexity accounting,
  field-level authorization hooks, and the subscription source-stream
  contract are Conduit code, tested against the spec's execution section and
  a differential corpus.

## Alternatives Considered

- **gqlgen end to end**: rejected; codegen requires schemas at build time,
  which contradicts the gateway model.
- **graphql-go/graphql**: rejected; code-first schema construction, long
  maintenance gaps, and no publish-time hook surface.
- **wundergraph/graphql-go-tools**: closest fit (built for gateways),
  rejected because its execution engine is optimized for federation and
  normalization workflows and embeds its own subscription management, which
  would fight Conduit's registry, index, and backpressure ownership; adopting
  only its parser would duplicate what gqlparser already provides with a
  smaller surface.
- **Own parser and validator**: rejected; large avoidable correctness risk
  with no product differentiation.
- **Code-first schema API**: rejected; it turns every schema change into a
  binary rebuild and makes the operator surface a Go API instead of
  reviewable configuration.

## Consequences

Conduit owns the execution semantics it most needs to control, and owns the
obligation to prove them: the R1 evidence includes a spec-execution test
corpus (null propagation, error paths, list coercion) that a library executor
would have carried. gqlparser becomes a reviewed dependency with a pinned
version and an upgrade test corpus. Directive names above are part of the
public schema contract and are versioned with the configuration schema.
