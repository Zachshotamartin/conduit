# Conduit: Exhaustive Build Plan

Document status: normative implementation plan for Conduit. Gate R0
repository infrastructure is `in progress`; no product gate is accepted.

Last revised: 2026-08-30.

Companion specifications:

- [Product requirements and user flows](./PRODUCT_REQUIREMENTS.md)
- [Architecture](./ARCHITECTURE.md)
- [Protocol conformance](./PROTOCOL_CONFORMANCE.md)
- [Authorization model](./AUTHORIZATION_MODEL.md)
- [Operations and test plan](./OPERATIONS_TEST_PLAN.md)
- [Threat model](./THREAT_MODEL.md)
- [Benchmark plan](./BENCHMARK_PLAN.md)
- [Marketing plan](./MARKETING_PLAN.md)
- [Decision records](./decisions/)

Conduit is a self-hosted GraphQL gateway whose product is the subscription
path: filtered subscriptions over `graphql-transport-ws`, publish-time
authorization, cross-node fanout, explicit backpressure, and honest resume.
Queries and mutations must work and are held to production quality, but no
query-side subsystem advances ahead of a user-visible subscription slice that
depends on it.

## 1. How to Read and Enforce This Plan

### 1.1 Status vocabulary

Every deliverable has exactly one of four statuses:

- **accepted**: implemented on the mainline and backed by its named automated
  gate;
- **in progress**: present on a working branch and not a release claim until
  its gate passes;
- **planned**: specified here but not implemented;
- **deferred**: intentionally outside the named gate and forbidden from being
  used to claim that gate complete.

A package, type, stub, or happy-path unit test is never completion. A gate is
accepted only when its user-visible flow, failure behavior, security cases,
documentation, and the complete §X.9 acceptance checklist pass together in
CI. R0 is `in progress` on `gate/r0`; R1 through R10 remain `planned`.

### 1.2 Gates and the capability each unlocks

Each gate's evidence unlocks exactly one product capability claim. No claim
may be made from a partially passed gate.

| Gate | Product evidence unlocked |
| --- | --- |
| R0 | Repository, toolchain, CI, and architecture checks exist and are green. |
| R1 | Single-node GraphQL queries and mutations resolve against real data sources with complexity limits enforced. |
| R2 | Subscriptions work over `graphql-transport-ws` and pass a conformance suite against the unmodified reference client. |
| R3 | Authorization is enforced at subscribe time and at publish time, including revocation and expiry mid-subscription. |
| R4 | The predicate index matches filters correctly and beats the linear scan on a published benchmark. |
| R5 | Cross-node fanout delivers correctly under node loss and bus partition. |
| R6 | Backpressure, quotas, and slow-consumer policy hold under adversarial load without unbounded memory growth. |
| R7 | Reconnect and resume behave to specification with a documented, measured gap window. |
| R8 | Metrics, tracing, an admin surface, drain-on-deploy, and a runbook make the system operable. |
| R9 | The measured scale target is met and published with honest statistical treatment. |
| R10 | Packaging, deployment, upgrade, and rollback satisfy the 1.0 release gate, and a real application runs on it end to end. |

Gate dependencies are strictly ordered R0 → R1 → … → R10 with one deliberate
overlap: R4 (index) and R5 (fanout) may proceed in parallel once R3 is
accepted, because the index consumes the same envelope contract R5 carries
and both are proven against the deterministic memory bus. R6 requires both.
No other reordering is permitted without an ADR.

### 1.3 Sequencing rules

1. Write the failing deterministic test before implementation for every
   parser, protocol state transition, authorization decision, predicate
   compilation, reducer, and error category.
2. Complete the thinnest user-visible vertical slice before broadening an
   internal subsystem. No internal subsystem advances ahead of a
   user-visible slice that depends on it.
3. Prove protocol, matching, authorization, and fanout behavior with
   injected clocks, in-process transports, and simulated peers before any
   real broker, cluster, or load harness is involved.
4. Keep one executor, one protocol state machine, one registry, one
   authorization decision path, and one publish pipeline across every
   transport and every gate.
5. Never treat a client frame, GraphQL document, credential, bus message,
   data-source response, or configuration file as trusted without a bounded
   parser at the owning boundary.
6. Never use a real provider of nondeterminism (network, broker, wall
   clock) to prove behavior a deterministic harness can prove.
7. Keep release and marketing claims no stronger than the measured
   configuration per the BENCHMARK_PLAN claims ladder.
8. Land wire-contract changes (protocol extensions, envelopes, resume
   tokens, control messages, admin API, configuration) with cross-version
   fixtures before deleting compatibility code.
9. A reversal of an accepted ADR decision requires a new ADR; no silent
   edits.
10. Every gate closes with explicit deferrals (§X.10) and a
    requirements-traced list (§X.11); the traceability matrix in §19 is the
    authoritative ownership record.

## 2. Current Baseline: What Is and Is Not Built

### 2.1 What exists

The private GitHub repository, this documentation set, the eleven accepted
ADRs, the exact Go toolchain pin, repository-local developer bootstrap,
deterministic clock and error foundations, configuration contracts, and R0
checks/workflow scaffolding are in progress on `gate/r0`. No gateway
listener or GraphQL product behavior exists; no gate is accepted and no
benchmark claim is earned. GitHub branch-protection application is blocked
by the current account tier and is recorded under `docs/evidence/r0/`.

### 2.2 The current honest product claim

> Conduit is a fully specified, unimplemented design for a subscription-first
> GraphQL gateway. Nothing runs. No performance, scale, conformance, or
> security property has been demonstrated.

No README, package description, demo, or portfolio bullet may imply
otherwise until the named gates pass. The claims that become available at
each gate are enumerated in MARKETING_PLAN §claims-ladder and nowhere else.

### 2.3 Inherited assets

None. Conduit shares documentation conventions with the sibling Robin
project but no code, no fixtures, and no evidence. Robin gate results are
not Conduit evidence.

## 3. Target Architecture Summary

ARCHITECTURE.md is the authoritative component specification. This section
fixes what the plan needs: boundaries, layout, core interfaces, and the two
state machines, so tickets can name their owned surfaces exactly.

### 3.1 The two pipelines

```text
subscribe path:
  client WebSocket
    -> transport (TLS, upgrade, subprotocol)
    -> protocol state machine (graphql-transport-ws)
    -> auth mode (connection_init payload -> principal)
    -> document intake (bounded parse, validate, complexity)
    -> SubscriptionAuthorizer.AuthorizeSubscribe        [enforcement point]
    -> predicate compiler (arguments -> predicate IR)
    -> connection registry + predicate index registration
    -> Next/Error/Complete delivery via outbound queue

publish path:
  mutation resolver success (or admin publish)
    -> publish mapping -> envelope (versioned, validated)
    -> bus publish (tenant-scoped subject)
    -> every node: bus consume -> dedupe window
    -> predicate index match (candidate set)
    -> SubscriptionAuthorizer.AuthorizePublish per entry [enforcement point]
    -> backpressure-aware enqueue on each connection's outbound queue
    -> writer goroutine -> socket
```

Trust boundaries, actor analysis, and abuse cases for each arrow are in
THREAT_MODEL §5–§7.

### 3.2 Repository layout

```text
cmd/
  conduit/                 # the gateway binary (serve, validate, doctor, version)
  conduit-loadgen/         # the load harness (BENCHMARK_PLAN §4)
internal/
  transport/               # listener, TLS, upgrade, WS library wrapper (only importer of coder/websocket)
  protocol/                # graphql-transport-ws state machine, message codecs
  graphql/
    ast/                   # only importer of gqlparser; bounded intake
    schema/                # SDL load, directive validation, binding config
    executor/              # Conduit-owned execution, null propagation, errors
    complexity/            # depth and cost accounting
  datasource/
    postgres/              # pgx adapter
    http/                  # template adapter, origin allowlist
    function/              # UDS/loopback function contract
  auth/
    principal/             # principal model, epochs
    oidc/  apikey/  custom/
    revocation/            # revocation set, control-message apply, sweep
    rules/                 # structured YAML rule engine
  filter/
    predicate/             # predicate IR, compiler
    index/                 # counting attribute index
    oracle/                # linear-scan differential oracle
  registry/                # per-node connection registry
  fanout/                  # envelope, dedupe, match-dispatch pipeline
  bus/
    memory/                # deterministic, fault-injectable
    nats/                  # reference production adapter
  resume/                  # replay buffers, token codec, splice
  queue/                   # outbound queues, backpressure policies
  admin/                   # admin listener, versioned API
  config/                  # schema, precedence, validation, reload
  observability/           # metrics catalogue, tracing, slog setup, redaction
  clock/                   # injected clock, timing wheel
  platform/                # the only home for GOOS-conditional code
test/
  conformance/             # reference-client + scripted-client suites
  hostile/                 # hostile client, fuzz corpora
  fault/                   # bus fault matrices, chaos scenarios
  load/                    # workload definitions shared with bench
  fixtures/                # SDL sets, rule sets, envelope corpora, JWKS fixtures
deploy/
  kubernetes/  docker/
bench/
  env/                     # reference environment manifests
reports/                   # published benchmark run reports
docs/
```

Negative import boundaries are enforced by the R0 architecture check
(NFR-MAINT-001):
`transport` is the only importer of the WebSocket library; `graphql/ast` is
the only importer of gqlparser; `protocol` imports neither executor nor
datasource; `queue` and `registry` do not import `bus`; nothing outside
`platform` reads `runtime.GOOS`; `admin` cannot import `transport` (separate
listener stack). The positive rule that every active sink owner directly
imports `observability/redaction` activates only after the canonical
redaction API and exact package-level owner inventory are defined; §5.10
records that explicit deferral so R0 does not invent owners or accept dummy
imports in doc-only skeletons.

### 3.3 Core interfaces to establish and preserve

Names may change only through an ADR and migration. Responsibilities may not
collapse across boundaries. Full definitions with doc comments are in
ARCHITECTURE §17; the plan pins the shapes:

```go
type AuthMode interface {
    Name() string
    Authenticate(ctx context.Context, cred CredentialMaterial, meta ConnMeta) (Principal, error)
}

type SubscriptionAuthorizer interface {
    AuthorizeSubscribe(ctx context.Context, p Principal, sub SubscribeRequest) (Decision, error)
    AuthorizePublish(ctx context.Context, entry EntryRef, env Envelope) (Decision, error)
}

type DataSource interface {
    Name() string
    Resolve(ctx context.Context, req SourceRequest) (SourceResult, error)
    Close(ctx context.Context) error
}

type Bus interface {
    Publish(ctx context.Context, subject Subject, data []byte) error
    Subscribe(ctx context.Context, subject Subject, h BusHandler) (BusSubscription, error)
    Events() <-chan BusEvent // connected, disconnected, slow-consumer, heartbeat
    Health() BusHealth
}

type PredicateIndex interface {
    Register(e Entry) error
    Unregister(id EntryID) error
    Match(env Envelope, out *CandidateSet) error // out reused; zero-alloc path
    Stats() IndexStats
}

type ReplayBuffer interface {
    Append(env Envelope) Position
    ReplayAfter(pos Position, visit func(Envelope) bool) (ReplayResult, error)
    Horizon() HorizonInfo
}

type OutboundQueue interface {
    Enqueue(msg OutboundMessage, policy BackpressurePolicy) EnqueueResult
    Control(msg OutboundMessage) error // ping/pong/ack/close; never dropped
    Depth() QueueDepth
}

type ConnectionRegistry interface {
    Register(c *Conn) error
    Deregister(id ConnID) []EntryID // atomic with index unregister
    Lookup(id ConnID) (*Conn, bool)
    Range(visit func(*Conn) bool)
}

type Clock interface {
    Now() time.Time
    Schedule(d time.Duration, fn func(now time.Time)) TimerHandle // callback must not block
    Cancel(h TimerHandle) bool
}
```

### 3.4 Connection state machine

States and transitions are normative in PROTOCOL_CONFORMANCE §3; the plan
pins the state list so tickets and tests share names: `connecting`,
`awaiting_init`, `authenticating`, `ready`, `draining_connection`,
`closing`, `closed`. Illegal transitions are structurally unrepresentable
(typed state table) and every legal transition row is unit-tested before any
socket integration (R2.02).

### 3.5 Subscription lifecycle state machine

Per-subscription states: `requested`, `authorizing`, `registered`,
`delivering`, `completing`, `completed`, `errored`. The registration step is
atomic across registry and index (FR-CONN-001): a subscription is matchable
if and only if it is deliverable. Teardown removes index entries before the
connection struct is released; a candidate produced against a dying entry is
discarded at the enqueue guard (the epoch check named in AUTHORIZATION_MODEL
§8).

### 3.6 Publish pipeline stages

Nine stages, each with a typed error category and a metric: map, seal
(envelope), bus-publish, consume, dedupe, match, authorize, enqueue, write.
Stage behavior, ordering argument, and failure handling are ARCHITECTURE
§10; per-stage latency series feed NFR-PERF-001 measurement.

## 4. Cross-Phase Engineering Rules

### 4.1 Test-first workflow

Each ticket follows this merge order:

1. add a test or fixture that fails for the intended reason;
2. add or update the boundary schema, typed errors, and bounds;
3. implement the smallest behavior that makes the unit test pass;
4. add integration coverage through public package boundaries;
5. add the adversarial and interruption cases the ticket names;
6. run the package tests, the architecture check, the current gate suite,
   and all accepted earlier gate suites;
7. update current-versus-planned documentation in the same changeset.

Determinism rules: injected `Clock` everywhere (no `time.Now()` outside
`clock`, architecture-checked); seeded randomness with logged seeds; no
wall-clock sleeps for correctness; the race detector on for every unit and
integration suite; bounded polling with recorded deadlines only in container
suites. Flake policy per harness class is OPERATIONS_TEST_PLAN §8 and is a
gate obligation, not advice.

### 4.2 Error taxonomy

Every boundary maps failures into a stable category with a safe client
message, structured diagnostic fields, and a metric. Top-level categories:

- `invalid_request` (malformed document, frame, or message; bounds);
- `invalid_configuration` (startup and reload validation);
- `unauthenticated`, `permission_denied`, `token_expired`, `grant_revoked`;
- `quota_exceeded`, `rate_limited`, `complexity_exceeded`;
- `source_unavailable`, `source_timeout`, `source_invalid_response`;
- `bus_unavailable`, `bus_degraded`, `publish_rejected`;
- `resume_rejected`, `resume_gap`;
- `overloaded` (load shed, fd budget), `draining`;
- `timeout`, `cancelled`, `internal_invariant`.

GraphQL-visible categories surface as `extensions.code`; connection-level
categories map to the close-code table (PROTOCOL_CONFORMANCE §7). Unknown
errors are converted once at the owning boundary, counted under
`internal_invariant`, and never leak internals (FR-GQL-012, canary-tested).

### 4.3 Bounded-resource rules

Every queue, buffer, cache, map, and parser input added in any ticket must
state its bound and overflow behavior in the ticket's definition of done.
The reviewer checklist rejects any unbounded structure. Named bounds live in
configuration with validated defaults (PRODUCT_REQUIREMENTS defaults table
values are the single source).

### 4.4 Data and privacy defaults

- No durable node state by design: logs are the only thing a node writes to
  disk. Uninstall is therefore trivial and tested (FR-OPS-011).
- No payload bytes in logs, traces, metrics, admin output, or diagnostics;
  redaction is centralized and canary-tested at every sink (NFR-SEC-004).
- No telemetry phones home. Ever. Conduit is self-hosted.
- Credentials exist in memory only within the auth mode's presentation
  scope; the principal carries no raw credential (FR-AUTH-005).

### 4.5 Performance and memory budgets

Budgets are gates to measure against, not claims to publish before
measurement. The SLO targets and the deliberately looser CI regression
ceilings are both fixed so a slow CI runner cannot silently redefine
product performance:

| Budget | SLO target | CI regression ceiling |
| --- | ---: | ---: |
| publish→enqueue p95 (reference workload) | 50 ms | n/a (measured in bench envs only) |
| index match p99 at 100k entries | 1 ms | 2 ms (microbench, benchstat) |
| delivery-path heap allocations per delivery | 0 | 0 (allocation test, hard) |
| idle memory per connection | 64 KiB | 96 KiB (10k-conn CI probe) |
| protocol message decode | — | 10 µs p99 microbench |
| startup to ready (valid config, no bus) | 2 s | 4 s |

Latency SLOs are only measurable in the benchmark environments; CI enforces
what CI can measure honestly (microbenchmarks, allocation counts, a bounded
10k-connection probe on the largest runner) and nothing more.

### 4.6 Dependency review gates

Conduit implements its protocol machine, executor, index, registry, queues,
fanout, resume, and rule engine in this repository. It uses: coder/websocket
(transport only), gqlparser/v2 (parse/validate only), nats.go, pgx, the OTel
SDK with Prometheus exporter, a JOSE library for JWT/JWKS, and yaml.v3.
Every addition needs a recorded review: purpose, license, release cadence,
transitive tree, native code, install size, and removal difficulty
(NFR-SEC-010, NFR-MAINT-005). Frameworks that would replace differentiating
implementation (GraphQL gateways, subscription managers, rule engines) are
excluded unless an ADR proves they do not.

### 4.7 Marketing and claims discipline

Marketing deliverables are part of this plan (MARKETING_PLAN is normative).
Two standing rules bind every gate: (a) each gate's §X.9 acceptance includes
updating the "publishable claims" register in MARKETING_PLAN with what that
gate's evidence newly permits, and (b) no asset — README line, launch post,
demo script, conference abstract — may state a claim whose gate is not
accepted. The claims-ladder audit is a release-blocking CI check from R0
(a lint over README and MARKETING_PLAN forbidden-claim markers).

## 5. R0 — Repository, Toolchain, CI, and Architecture Checks

**Status:** in progress.

**Effort range:** 3–5 focused days.

### 5.1 Why this gate exists

Every later gate cites automated evidence. R0 creates the machinery that
makes evidence possible: the repository, the pinned toolchain, the CI
workflows that later gates attach required checks to, the architecture
checks that make boundary violations fail CI rather than review, and the
claims-discipline lint that keeps the README honest from the first commit.
R0 also prevents the earliest dangerous shortcut: writing feature code
before the harness that would prove it exists.

### 5.2 Prerequisites

- The GitHub CLI is authenticated for the owning account with `repo` and
  `workflow` scopes (`gh auth status` exits zero and lists both). This is
  the first verified step of the entire plan and is already satisfied on the
  authoring machine (account `Zachshotamartin`).
- ADR-0001 through ADR-0011 are accepted.
- This documentation set is complete and internally consistent.

### 5.3 Owned files, interfaces, and state

R0 owns: the repository itself; `go.mod`/`go.work` with the pinned
toolchain directive; `Makefile` (`bootstrap`, `build`, `test`, `check`,
`conformance`, `bench-smoke` targets); `.github/workflows/pr.yml`,
`integration.yml`, `nightly.yml`, `release.yml` (skeletons whose jobs later
gates fill); `tools/archcheck` (the import-boundary checker); the docs-status
and claims-ladder lints; `internal/clock` (the first real package, because
every later test injects it); typed error scaffolding in `internal/errors`
with the §4.2 categories; and the `internal/config` skeleton with the
precedence chain and validation phase framework.

The architecture check is configuration-as-code:

```go
// tools/archcheck/rules.go
type Rule struct {
    Package string   // import path pattern
    MayImport []string
    MustNotImport []string
    Reason string    // printed on violation
}
```

Rules encode every §3.2 boundary. The check runs on the module graph, not
on file text, so vendored or generated code cannot dodge it.

### 5.4 Algorithms and state behavior

Bootstrap order (numbered, each step verifiable):

1. `gh auth status` — verified; abort with instructions if scopes missing.
2. `gh repo create <owner>/conduit --private --clone` (public at R10's
   publication audit, not before); default branch `main`.
3. Commit this documentation set as the first commit.
4. Add toolchain pin (`go 1.23.x` exact), empty module, `Makefile`,
   workflow skeletons; second commit.
5. Branch protection via `gh api`: require the `pr.yml` checks, forbid
   force-push, require one review.
6. Land `clock`, `errors`, `archcheck`, lints with their tests (test-first:
   each check has a fixture repository layout that must fail it).
7. Tag the baseline `v0.0.0-docs`.

Edge cases: repository name collision (fail, never delete); `gh` token with
missing `workflow` scope silently skipping workflow push (explicitly probed
by pushing a trivial workflow and reading it back via API); branch
protection API drift (assert the protection state by reading it back, not
by the mutation call succeeding).

### 5.5 Implementation tickets and sequence

1. **R0.01 — Verify GitHub CLI authentication.** Definition of done:
   `make check-gh` target exists, runs `gh auth status`, asserts account
   and scopes, and is documented as the plan's first command. (Done on the
   authoring machine; the target makes it reproducible for any contributor.)
2. **R0.02 — Create repository and protection.** Repo created via `gh`,
   branch protection asserted by read-back, docs committed. DoD: a fresh
   clone contains only docs and passes `git fsck`.
3. **R0.03 — Pin toolchain and module layout.** `go.mod` with exact
   toolchain, directory skeleton per §3.2 with `doc.go` package statements
   only (no stub logic). DoD: `go build ./...` succeeds trivially.
4. **R0.04 — Build `internal/clock`.** Injected clock, timing-wheel
   interface with a deterministic test double (wheel implementation itself
   lands in R2 where first needed). DoD: fake-clock tests demonstrating
   schedule/fire/cancel determinism.
5. **R0.05 — Build `internal/errors`.** §4.2 categories as typed errors
   with category, safe message, and metric-key accessors. DoD: exhaustive
   category test; unknown-wrap test.
6. **R0.06 — Build `tools/archcheck`.** Module-graph import checker with
   the §3.2 rule set. DoD: fixture violations fail with the rule's reason;
   the real tree passes.
7. **R0.07 — Build the docs-status and claims lints.** Forbidden-phrase
   lint (the placeholder marker spelled T-O-D-O, the phrase “coming” followed
   by “soon”, and unstatused deliverables) over docs/ except
   OPEN_QUESTIONS; claims-ladder lint over README/MARKETING_PLAN (markers
   `<!-- claim:R<n> -->` must map to accepted gates in a checked-in gate
   status file). DoD: fixtures fail; current tree passes.
8. **R0.08 — Wire pr.yml.** vet, staticcheck, archcheck, lints, unit tests
   with race detector, 15-minute budget. DoD: required checks attached to
   branch protection and green on a no-op PR.
9. **R0.09 — Wire integration/nightly/release skeletons.** Jobs exist,
   run trivially green, and carry the artifact-retention configuration.
   DoD: nightly runs on schedule once.
10. **R0.10 — Config skeleton.** Precedence chain (defaults < file < env <
    flags), validation-phase framework, `conduit validate` returning typed
    errors for a fixture set of invalid configs. DoD: table-driven
    validation tests; no server code.
11. **R0.11 — Marketing register bootstrap.** MARKETING_PLAN claims
    register initialized with every claim marked unearned; README carries
    the §2.2 honest claim verbatim. DoD: claims lint green.

### 5.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| gh scope probe | Token lacks `repo` or `workflow` scope. | `make check-gh` fails with the missing scope named; passes on the authoring account. |
| protection read-back | Branch protection mutation silently no-ops. | Read-back shows required checks, review count, force-push disabled. |
| clock determinism | Fake clock fires timers on wall time. | Scheduled callbacks fire only on explicit advance, in order, with stable tie-breaking. |
| error taxonomy | A category lacks safe message or metric key. | Every §4.2 category constructs, serializes safely, and round-trips its category. |
| archcheck fixtures | A forbidden import passes the checker. | Each fixture violation fails naming the rule; the real tree passes. |
| docs-status lint | A doc contains the configured placeholder marker outside OPEN_QUESTIONS. | Lint fails on fixture, passes on tree. |
| claims lint | README claims a capability with no accepted gate. | Lint fails on fixture claim marker, passes on tree. |
| config precedence | Env value fails to override file value. | Table-driven precedence and validation-phase tests pass. |

### 5.7 Failure and security cases

- CI secrets: none exist in R0 beyond the default `GITHUB_TOKEN`; workflows
  set `permissions:` minimally (contents: read) from the first skeleton.
- The claims lint failing must block merge — a broken lint that silently
  passes is itself a failure case with a fixture test.
- `gh repo create` on an existing name aborts; no deletion path exists in
  any script in this repository.
- Fork PRs get no secrets and cannot run release jobs (workflow condition
  tested with a fixture fork simulation where GitHub allows).

### 5.8 Migration, documentation, and installation work

No migrations (nothing exists). Root README gains the contributor bootstrap
(§4 of OPERATIONS_TEST_PLAN) and the honest status snapshot. No install
channel exists or is implied.

### 5.9 Acceptance evidence

R0 is accepted only when:

- the repository exists with protection asserted by read-back;
- `make bootstrap && make check && make test` is green on a clean Linux and
  a clean macOS machine (recorded run links in the acceptance PR);
- pr.yml is a required check and has failed at least once on a deliberate
  fixture violation (evidence that the gate can fail);
- archcheck, docs-status lint, and claims lint are required checks;
- the claims register exists with every claim unearned;
- nightly has completed one scheduled run.

### 5.10 Explicit deferrals

R0 defers all product code: no listener, no parser, no schema, no executor,
no protocol, no index, no bus, no benchmarks. The timing-wheel
implementation is deferred to R2. Release packaging jobs are skeletons only
(R10 fills them). Public repository visibility is deferred to R10. The
positive `observability/redaction` must-import rule is deferred to the first
gate that implements a sink: that gate must define the canonical redaction
API and a machine-readable exact owner inventory before adding sink code;
R8 closes the inventory across logs, traces, metrics, errors, and diagnostics.
R0 continues to enforce the negative observability-SDK confinement rule.

### 5.11 Requirements traced

R0 terminally owns `NFR-MAINT-001`, `NFR-MAINT-004`, `NFR-MAINT-005`,
`NFR-MAINT-006`, and `NFR-SEC-010`. It advances `FR-OPS-002` (config
skeleton) and `FR-OPS-012` (workflow skeletons) without closing them; §19
records R10 as their terminal owner.

## 6. R1 — Single-Node Queries and Mutations Against Real Data Sources

**Status:** planned.

**Effort range:** 4–6 focused weeks.

### 6.1 Why this gate exists

R1 is the thinnest user-visible slice: an operator can point Conduit at SDL
plus binding config and execute real queries and mutations over HTTP with
depth and complexity limits enforced. It exists before any subscription work
because the subscription path reuses everything R1 builds — schema loading,
document intake, the executor, data sources, error formatting — and those
must be proven where failures are cheapest to diagnose. R1 also lands the
publish *mapping* seam (mutations declaring what they publish) without any
delivery, so R2 and R5 attach to a tested contract instead of retrofitting
one.

### 6.2 Prerequisites

- R0 accepted.
- Fixture SDL corpus and binding configs checked in under `test/fixtures`
  (valid set, and an invalid set with one file per validation rule).
- Docker/podman available for the Postgres container suite.

### 6.3 Owned files, interfaces, and state

R1 owns `internal/graphql/{ast,schema,executor,complexity}`,
`internal/datasource/{postgres,http,function}`, the HTTP operation endpoint
in `internal/transport` (no WebSocket yet), and the `DataSource` port.

Document intake bound contract:

```go
type IntakeLimits struct {
    MaxBytes  int // default 1 << 20
    MaxTokens int // default 20_000
    MaxDepth  int // default 15
}

// Intake parses and validates within limits. It never allocates an AST for
// an input that exceeds MaxBytes or MaxTokens; token counting happens
// during lexing with early abort.
func Intake(doc []byte, lim IntakeLimits, schema *Schema) (*Operation, error)
```

Publish-mapping seam (no delivery in R1):

```go
type PublishMapping struct {
    Field      FieldRef          // subscription field this mutation feeds
    Attributes map[string]ArgExpr // envelope attributes from mutation result
    Payload    PayloadExpr        // envelope payload from mutation result
}

type PublishSink interface { // R1 binds a recording fake; R5 binds the bus
    Emit(ctx context.Context, env Envelope) error
}
```

The function-source wire contract (versioned) is owned here:

```json
{"conduit_function/v1": {"field": "Query.userById", "args": {"id": "u_1"},
 "principal": {"subject": "s", "tenant": "t", "scopes": ["read"]},
 "deadline_ms": 1500}}
```

Response: `{"conduit_function/v1": {"data": …}}` or
`{"conduit_function/v1": {"error": {"code": "…", "message": "…"}}}`, with
response bounds (1 MiB default) enforced by the adapter.

### 6.4 Algorithms and state behavior

Startup schema pipeline (numbered):

1. Read SDL file set; reject on I/O error naming the path.
2. Parse via `graphql/ast` with intake bounds scaled for schemas (4 MiB).
3. Validate SDL against the spec rules; collect all errors, not first-only.
4. Validate Conduit directives: `@source` names a configured source;
   `@auth` names a defined rule; `@filterable` sits on a supported scalar
   argument of a subscription field; `@backpressure` values well-formed;
   `@complexity` costs non-negative.
5. Validate binding config against the schema: every non-introspection
   field of an operation type resolves to exactly one source or explicit
   parent-projection; orphan bindings are errors.
6. Build the immutable `Schema` object; compute and log its hash.
7. On any error: exit nonzero (startup) or reject reload atomically (R8).

Execution algorithm: field collection, argument coercion, source dispatch
with per-source concurrency caps and deadline propagation, list and
non-null propagation per spec, error accumulation with paths. Edge cases
named and tested: null bubbling through non-null lists; aliased duplicate
fields with conflicting arguments (spec rule); fragment cycles (rejected at
validation); source returning more/fewer rows than the shape requires;
source timeout mid-list; mutation serial order with a failing middle field
(later mutations still run per spec; their publish mappings for the failed
field do not emit).

Complexity accounting runs post-validation pre-execution: cost = Σ field
costs × multiplier products; rejection carries computed cost in
`extensions` (FR-GQL-009).

Mutations emit publish mappings only after resolver success, into the
recording `PublishSink` (asserted by tests; no delivery exists).

### 6.5 Implementation tickets and sequence

1. **R1.01 — Bounded intake.** Lexer-level byte/token caps, depth cap,
   fuzz seed corpus. DoD: over-bound inputs allocate no AST (alloc test).
2. **R1.02 — SDL load and directive validation.** Steps 1–7 with the full
   invalid-fixture set. DoD: every invalid fixture fails with file/line;
   valid set builds; hash stable.
3. **R1.03 — Binding config.** Schema-config cross-validation. DoD: orphan
   and missing bindings fail with both locations named.
4. **R1.04 — Executor core.** Collection, coercion, null propagation over
   an in-memory fixture source. DoD: spec-execution corpus green (the
   corpus is the ticket's first artifact).
5. **R1.05 — Complexity and depth.** Accounting, rejection, extensions
   payload. DoD: boundary tests at limit and limit±1.
6. **R1.06 — Error formatting.** Category → `extensions.code`, paths,
   locations, redaction canaries. DoD: canary corpus never appears in any
   response fixture.
7. **R1.07 — Postgres source.** pgx pool, parameterized statement binding,
   timeout, typed errors, testcontainer suite. DoD: injection-shaped
   inputs are inert (adversarial fixture set); pool exhaustion sheds with
   `source_unavailable`.
8. **R1.08 — HTTP source.** Template expansion (URL/headers/body from
   coerced args only), origin allowlist, response bounds, retry
   classification (idempotent GET only). DoD: SSRF fixture set (redirects
   to disallowed origins, IP literals, userinfo URLs) all fail closed.
9. **R1.09 — Function source.** UDS/loopback client, v1 contract, bounds,
   deadline mapping. DoD: contract fixtures both directions; oversized
   response fails typed.
10. **R1.10 — HTTP operation endpoint.** POST /graphql, content-type
    handling, request bounds, deadlines, metrics skeleton. DoD: end-to-end
    fixture queries/mutations against all three sources.
11. **R1.11 — Publish-mapping seam.** Mapping compile from SDL config,
    recording sink, emission-on-success semantics. DoD: failing resolver
    emits nothing; succeeding emits exactly its mappings, envelope-shaped.
12. **R1.12 — Introspection policy (execution half).** enabled/disabled
    modes wired (admin-only completes in R3 with auth). DoD: disabled mode
    removes introspection at validation, not execution.
13. **R1.13 — R1 gate suite assembly.** The UNIT/intake/executor/source
    matrices wired as the `gate-r1` CI job. DoD: job required and green.

### 6.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| intake bounds | Oversized document reaches the parser. | Byte/token/depth over-limit inputs fail typed with zero AST allocation. |
| SDL validation corpus | An invalid fixture loads. | Every invalid fixture fails naming file, line, and rule; valid corpus builds with stable hash. |
| spec execution corpus | Null propagation or coercion deviates from spec. | Corpus (≥60 cases incl. null bubbling, aliases, fragments, skip/include) matches expected responses byte-exactly. |
| complexity boundary | Cost limit±1 behaves identically. | At-limit executes; over-limit rejects with computed cost in extensions. |
| mutation ordering | Failed middle mutation halts later fields or emits its mapping. | Spec serial semantics hold; only successful fields emit mappings. |
| postgres adversarial | Injection-shaped argument alters a statement. | Parameterization proven: hostile fixtures return data-typed results or typed errors, never statement changes; archcheck forbids string SQL assembly. |
| HTTP source SSRF | Template reaches a non-allowlisted origin. | All SSRF fixtures fail closed before connection; allowlist matches exact origins only. |
| function contract | Version or bound violation is accepted. | Unknown version, oversized, and malformed responses fail typed; valid round-trip preserved. |
| redaction canaries | A canary string appears in any error response. | Canary corpus absent from all response fixtures and logs. |
| endpoint end-to-end | Any of the three sources fails the fixture app. | Fixture app queries and mutations succeed against postgres+http+function with deadlines enforced. |

### 6.7 Failure and security cases

- Startup with any invalid SDL/config exits nonzero before binding a port.
- A data source that hangs is cut by deadline; the operation reports
  `source_timeout` at the failed path with siblings unaffected where spec
  allows.
- Response bodies from HTTP/function sources are untrusted: bounded,
  content-type-checked, JSON-parsed with depth limits.
- No listener speaks WebSocket in R1; upgrade attempts get 400 with a
  planned-feature message (and no subprotocol negotiation).
- `auth.mode: none` acknowledgment is required even in R1 (the auth
  subsystem arrives in R3; R1 runs only in acknowledged dev mode and says
  so at startup, so no R1 deployment can be mistaken for authenticated).

### 6.8 Migration, documentation, and installation work

Docs: data-source configuration reference, SDL directive reference (marking
`@auth`/`@backpressure` as validated-but-inert until their gates), the
15-minute first-run walkthrough (FR-OPS flow 6.1, minus subscriptions).
Marketing register: R1 permits the claim "executes queries and mutations
against Postgres/HTTP/function sources with enforced limits — single node,
development auth only"; the register records exactly that sentence.

### 6.9 Acceptance evidence

- `gate-r1` CI job green and required: all §6.6 rows plus the OPERATIONS
  UNIT rows assigned earliest-gate R1.
- The fixture application (SDL + three sources) runs the documented
  first-run walkthrough from a clean container, recorded in the acceptance
  PR.
- Redaction canary sweep green across responses and logs.
- Claims register updated; claims lint green.
- No WebSocket, subscription, auth-mode, index, bus, or resume code merged
  beyond the seams named in §6.3 (reviewed against the diff).

### 6.10 Explicit deferrals

R1 defers: all WebSocket transport and protocol work (R2); all real
authentication and authorization (R3 — R1 runs acknowledged-dev-mode only);
predicate compilation and the index (R4); envelope delivery beyond the
recording sink, the bus, fanout (R5); backpressure and quotas beyond plain
HTTP request bounds (R6); resume (R7); admin surface beyond a metrics
skeleton (R8); all performance claims (R9); packaging (R10).

### 6.11 Requirements traced

R1 terminally owns `FR-GQL-001` through `FR-GQL-009`, `FR-GQL-011`,
`FR-GQL-013`, `FR-GQL-014`, and `NFR-MAINT-003`. It advances `FR-GQL-010`
and `FR-GQL-012` (terminal R3), `FR-GQL-015` (terminal R2), `FR-FAN-001`
(seam only; terminal R5), and `FR-OPS-002` (terminal R10).

## 7. R2 — Subscriptions over `graphql-transport-ws` with Conformance

**Status:** planned.

**Effort range:** 5–7 focused weeks.

### 7.1 Why this gate exists

R2 makes the product's center exist: a client subscribes over the exact
protocol, a publish reaches it, and the unmodified reference `graphql-ws`
client works against Conduit with no Conduit-specific accommodation. The
protocol state machine, message codecs, close-code discipline, connection
registry, and outbound queue all land here — deterministically, on the
in-process bus, single-node, with matching done by the linear-scan oracle
(the index is R4; correctness precedes speed). R2 is also where hostile
clients first meet Conduit, because a protocol boundary proven only against
polite clients is not proven.

### 7.2 Prerequisites

- R1 accepted (executor, intake, sources, publish-mapping seam).
- PROTOCOL_CONFORMANCE.md state table and ambiguity register frozen for the
  gate (changes during R2 amend the doc in the same PR as the code).
- Node container fixture with the pinned reference `graphql-ws` client
  version range available in CI.
- `bus/memory` with fault injection (built here, used by every later gate).

### 7.3 Owned files, interfaces, and state

R2 owns `internal/protocol`, `internal/transport` (WebSocket upgrade, TLS,
subprotocol negotiation), `internal/registry`, `internal/queue` (bounded
queue and writer loop; policies beyond `disconnect`-on-full arrive in R6),
`internal/bus/memory`, `internal/fanout` (single-node pipeline over the
memory bus), `internal/filter/oracle` (linear scan), the timing wheel in
`internal/clock`, and `test/{conformance,hostile}`.

Protocol message codec shapes (field-level contracts in
PROTOCOL_CONFORMANCE §4; the Go types are owned here):

```go
type MessageType string // connection_init, connection_ack, ping, pong,
                        // subscribe, next, error, complete

type InboundMessage struct {
    Type    MessageType
    ID      string          // bounded 255 bytes where present
    Payload json.RawMessage // bounded, type-specific decode
}

type ConnState uint8 // connecting, awaiting_init, authenticating, ready,
                     // draining_connection, closing, closed

type Transition struct {
    Next     ConnState
    Send     []OutboundMessage
    Close    *CloseIntent // code + reason, nil if staying open
}

// The state table is data, not branching code: table[state][event] -> Transition.
func Step(s ConnState, ev Event) (Transition, error)
```

Subscription entry (registered against the oracle in R2; the same struct
feeds the index in R4):

```go
type Entry struct {
    ID        EntryID       // (ConnID, subscription id)
    Tenant    TenantID
    Field     FieldRef
    Predicates []Predicate  // compiled in R4; R2 carries raw coerced args
    RawArgs   ArgValues
    Principal PrincipalRef  // dev-mode principal until R3
    BP        BackpressureConfig
    Epoch     uint64
}
```

The wire envelope contract v1 (shared with R5; validated from R2):

```go
type Envelope struct {
    Version    uint16            // 1
    Tenant     TenantID
    Field      FieldRef
    PublishID  [16]byte          // UUIDv7
    OriginNode NodeID
    PublishedAt int64            // unix ms, origin clock
    Attributes map[string]AttrValue // scalar-typed, bounded count 32
    Payload    []byte            // bounded per config
}
```

JSON example and byte bounds are in ARCHITECTURE §10; the codec rejects
unknown versions and over-bound fields with typed errors (FR-FAN-002).

### 7.4 Algorithms and state behavior

Read-loop algorithm per connection (numbered):

1. Reader goroutine reads one text frame (library frame cap = configured
   inbound bound; binary frame → step 8 with `binary_frame` event).
2. Decode `InboundMessage` with bounds; malformed → `invalid_message`
   event.
3. `Step(state, event)` against the table; illegal per-state messages
   produce the table's close intent (never a panic, never a default case).
4. Execute transition sends via `OutboundQueue.Control` (ack, pong) or the
   data path (next/error/complete).
5. `subscribe` in `ready`: bounded payload decode → R1 intake → single
   result (query/mutation over WS: execute, send Next then Complete —
   FR-GQL-015) or subscription registration (below).
6. `complete`: deregister entry; racing in-flight deliveries are cut at
   the enqueue guard by entry-epoch check.
7. `ping`: reply pong echoing payload (bounded); `pong`: clear keepalive
   deadline.
8. Close intents: send close frame with code/reason, stop reads, signal
   writer to flush control frames within the close grace (2 s), tear down.

Registration atomicity (FR-CONN-001), numbered:

1. Reserve subscription ID on the connection (duplicate → 4409 close per
   spec).
2. Build entry; assign epoch from the connection's epoch counter.
3. Insert into registry and oracle/index under the shard lock; the entry
   becomes matchable and deliverable in the same critical section.
4. On any later teardown: mark entry epoch dead, remove from oracle/index,
   then release; the enqueue guard drops candidates carrying a dead epoch.

Single-node fanout over the memory bus reuses the full nine-stage pipeline
(§3.6) so R5 changes only the bus binding: mutation → seal → memory-bus →
consume → dedupe → oracle match → (dev-mode authorize pass-through) →
enqueue → write.

Keepalive/idle/lifetime on the timing wheel: server ping every 25 s; miss
accounting on the connection; idle 5 min → 4702; lifetime 12 h ±10% jitter
→ warning ping then 4701. All fired via injected clock; every timer test is
deterministic.

Edge cases named: client Complete racing server Complete; subscribe with
valid document but undefined subscription field; two subscribes same ID in
one read batch; close during replayed sends; writer socket stall during
close grace (hard cut at grace expiry); UTF-8 split across WS continuation
frames (library reassembles; codec sees whole messages — asserted).

### 7.5 Implementation tickets and sequence

1. **R2.01 — Message codecs.** All eight message types, bounds, typed
   decode errors, fuzz seeds. DoD: codec matrix green; fuzz corpus seeded
   and running nightly.
2. **R2.02 — State table.** The full `Step` table from PROTOCOL_CONFORMANCE
   §3 with every cell populated. DoD: exhaustive table test — every
   state×event asserted, including impossible pairs.
3. **R2.03 — Timing wheel.** Hashed wheel (tick 500 ms, 4 levels),
   schedule/cancel/fire under fake clock, load test for 1M timers. DoD:
   determinism and collision-order tests.
4. **R2.04 — Transport upgrade path.** TLS config, upgrade, subprotocol
   negotiation (reject legacy per ADR-0002: HTTP 400 pre-handshake, 4406
   post), library wrapper confinement. DoD: negotiation matrix incl. both
   rejection paths; archcheck keeps the library confined.
5. **R2.05 — Outbound queue and writer.** Bounded queue (256/1 MiB),
   control-frame bypass lane, writer loop, disconnect-on-full as the only
   R2 policy. DoD: full-queue behavior, control bypass under data
   saturation, writer stall handling.
6. **R2.06 — Connection registry.** Sharded registry, atomic
   register/deregister with oracle, epoch guard. DoD: race-detector churn
   test (10k conns × register/publish/deregister loops) clean.
7. **R2.07 — Linear-scan oracle.** Raw-arg equality matching for R2
   (full predicate grammar arrives in R4 with compilation), stable
   iteration, instrumentation hooks. DoD: match correctness fixtures;
   oracle is the only matcher wired.
8. **R2.08 — Envelope codec v1.** Struct, JSON codec, bounds,
   unknown-version rejection, dedupe-window skeleton (window logic
   completes in R5). DoD: codec matrix; version fixtures.
9. **R2.09 — Memory bus.** In-process bus with injectable partition,
   delay, reorder, duplication; `Events()` semantics. DoD: fault-injection
   API tests; deterministic delivery order under no-fault config.
10. **R2.10 — Pipeline assembly.** Mutation publish-mapping → sink →
    memory bus → match → enqueue, end to end on one node. DoD: the §1.2
    R2 capability demonstrated by deterministic test: subscribe with
    matching args receives the mutation's event; non-matching receives
    nothing.
11. **R2.11 — Keepalive, idle, lifetime.** Wheel-driven ping/idle/4702/
    4701 with jitter. DoD: fake-clock timelines for each; jitter bounds
    asserted statistically over seeds.
12. **R2.12 — Conformance suite.** The scripted protocol client + the
    unmodified reference client container, executing PROTOCOL_CONFORMANCE
    §8's CONF rows. DoD: every CONF row green in CI; reference client pin
    documented.
13. **R2.13 — Hostile client suite.** HOST rows: malformed frames,
    out-of-order, oversized ±1, duplicate IDs, init floods, slow reads,
    binary frames, invalid UTF-8. DoD: every HOST row green; node survives
    the full suite in one process run (no restarts between rows).
14. **R2.14 — Fuzzing targets.** Frame decoder, message decoder, subscribe
    payload; nightly jobs with crash triage policy. DoD: 72 fuzz-hours
    accumulated with zero open crashers before gate acceptance.
15. **R2.15 — Gate suite assembly + docs.** `gate-r2` job; protocol
    reference docs published; claims register update. DoD: job required
    and green.

### 7.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| state-table exhaustion | Any state×event cell is unasserted or panics. | Every cell yields its documented transition, sends, and close code; no default branch exists. |
| reference-client interop | The unmodified client needs any accommodation. | Pinned `graphql-ws` client completes connect/subscribe/next/complete/ping-pong/error scenarios against Conduit byte-unmodified. |
| duplicate-ID close | Second subscribe with an active ID is tolerated. | Connection closes 4409 naming the ID; first subscription's teardown is clean. |
| init discipline | Late init, double init, or pre-ack subscribe slips through. | 4408 on timeout at 3 s fake-clock; 4429 on double init; 4401 on pre-ack subscribe. |
| oversized frame | A frame over 512 KiB is buffered. | Close 4400 with bounded memory delta; library cap engaged as backstop. |
| registration atomicity | A candidate is produced for a torn-down entry and delivered. | Churn race test: zero deliveries to dead epochs, zero orphan entries after 10k-conn churn. |
| single-node fanout | Matching subscriber misses the event or non-matching receives it. | Deterministic publish test delivers exactly to matching entries, in per-publisher order. |
| WS single-result ops | Query over WS diverges from HTTP semantics. | Same document over HTTP and WS yields identical results; WS wraps as Next+Complete. |
| keepalive/idle/lifetime | Any timer fires on wall clock or wrong code. | Fake-clock timelines produce 4702/4701 exactly as documented with jitter within ±10%. |
| hostile survival | Any HOST row crashes, leaks, or affects another connection. | Full hostile suite in one process: memory returns to baseline band, unrelated connection's latency unaffected (measured in-suite). |
| control bypass | Ping/pong/close starves behind a full data queue. | Control lane delivers under data saturation; close grace honored. |

### 7.7 Failure and security cases

- All NFR-SEC-001 frame/message parsing bounds are enforced in this gate's
  code and adversarially tested (terminal owner).
- Panic policy: a per-connection panic recovers to 1011 close + counter;
  a panic in shared pipeline stages is a test failure class of its own
  (the pipeline must never panic on data).
- Slow reader during close: hard cut at grace expiry; socket closed even
  if the close frame cannot flush.
- The memory bus is a test instrument: archcheck forbids `bus/memory`
  imports outside tests and the composition root's explicit dev profile.
- permessage-deflate is not negotiated (NFR-SEC-008); asserted in the
  upgrade matrix.

### 7.8 Migration, documentation, and installation work

Envelope v1 and the protocol extension namespace are versioned from first
merge (NFR-COMPAT-003 machinery starts here; terminal R7/R10). Docs: the
protocol reference (client-author-facing), the close-code table, the
dev-mode quick start now including a subscription. Marketing register: R2
permits "implements graphql-transport-ws, proven against the unmodified
reference client; single node, development auth" — nothing about scale,
auth, or fleets.

### 7.9 Acceptance evidence

- `gate-r2` green and required: §7.6 rows, PROTO/CONF/HOST matrices from
  OPERATIONS_TEST_PLAN with earliest-gate R2.
- 72 accumulated fuzz-hours, zero open crashers, triage log in-repo.
- Reference-client pin and interop transcript recorded in the acceptance
  PR.
- Race-detector suite green including the churn test.
- Claims register updated; lint green.

### 7.10 Explicit deferrals

R2 defers: real auth modes and both authorization decision points (R3 —
dev-mode principal only); predicate compilation and the counting index
(R4 — oracle only, equality on raw args); NATS and multi-node anything
(R5); `drop_oldest`/`coalesce_by_key`, quotas, rate limits, fd budget
(R6 — disconnect-on-full only); resume positions in Next extensions exist
as reserved fields but no tokens, buffers, or replay (R7); admin surface
beyond metrics skeleton (R8); all performance claims (R9).

### 7.11 Requirements traced

R2 terminally owns `FR-SUB-001` through `FR-SUB-012`, `FR-GQL-015`,
`FR-CONN-001`, `NFR-SEC-001`, `NFR-COMPAT-001`, and `NFR-COMPAT-002`. It
advances `FR-FAN-002`/`FR-FAN-004` (envelope, ordering; terminal R5),
`FR-CONN-002`/`FR-CONN-003`/`FR-CONN-007` (timers and queue exist;
policies and quotas terminal R6), and `FR-RESUME-001` (reserved extension
field only; terminal R7).

## 8. R3 — Authorization at Subscribe Time and Publish Time

**Status:** planned.

**Effort range:** 5–7 focused weeks.

### 8.1 Why this gate exists

Grants change while subscriptions are live. R3 builds the three auth modes,
the principal model, the rule engine, and both enforcement points — and
proves with adversarial evidence that neither can be bypassed, including
the two hardest promises in the product: no delivery after revocation
reaches the node, and no delivery after token expiry. Everything later
(index, fanout, resume) threads through the decision points R3 freezes, so
retrofitting authorization after those gates would invalidate their
evidence.

### 8.2 Prerequisites

- R2 accepted.
- AUTHORIZATION_MODEL.md rule schema, decision-point algorithms, and
  adversarial table frozen for the gate.
- JWKS fixture server and API-key fixture store in `test/fixtures`.
- ADR-0008 and ADR-0009 semantics restated as executable acceptance
  fixtures (timeline scripts for expiry and revocation).

### 8.3 Owned files, interfaces, and state

R3 owns `internal/auth/**` (principal, oidc, apikey, custom, revocation,
rules), the `AuthMode` and `SubscriptionAuthorizer` ports, the epoch-keyed
decision cache, the revocation set with its bus-control application path
(bus transport is memory-bus here; NATS binding in R5 changes transport
only), and the enforcement-point wiring in the subscribe and publish
pipelines.

Principal and epoch:

```go
type Principal struct {
    Subject  string
    Tenant   TenantID
    Scopes   []string
    Claims   map[string]ClaimValue // bounded count 64, bounded bytes
    Expiry   time.Time             // zero for non-expiring modes
    Mode     AuthModeName
    // grant-state epoch is node-local, not stored on the principal:
}

type EpochSource interface{ Current(t TenantID) uint64 }

type Decision struct {
    Allow  bool
    Rule   RuleName   // the rule that decided
    Reason DenyReason // typed; never free text from rule authors
}
```

Revocation control message v1 (bus wire shape, JSON over
`conduit.<tenant>.ctl.revoke`):

```json
{"conduit_ctl/v1": {"kind": "revoke", "revoke": {"class": "principal",
 "id": "subject:alice", "issued_at_ms": 1756500000000,
 "revocation_id": "rev_018f3c…"}}}
```

Decision cache: `map[EntryID]cachedDecision{epoch uint64, allow bool}`
sharded with the index shards; a probe compares the entry's tenant epoch
and discards on mismatch. Cache size is bounded by entry count (one slot
per entry, embedded in the entry struct — no separate growth).

### 8.4 Algorithms and state behavior

Subscribe-time decision (enforcement point
`SubscriptionAuthorizer.AuthorizeSubscribe`), numbered:

1. `connection_init` → auth mode `Authenticate` → principal or 4403 (one
   uniform failure; mode-specific detail goes to logs only, FR-AUTH-018).
2. `subscribe` → intake → field's `@auth` rule lookup (undefined rule was
   a startup failure; absence of any rule denies in secure modes).
3. Evaluate the structured rule against principal, field, coerced args.
4. Deny → typed `Error` frame on that ID (subscription never registers).
5. Allow → bind rule ref + argument-claim bindings into the entry, then
   the R2 atomic registration.

Publish-time decision (enforcement point
`SubscriptionAuthorizer.AuthorizePublish`), per candidate, numbered:

1. Guard: entry epoch live (R2 rule).
2. Probe decision cache: hit with current tenant epoch → use cached allow;
   miss or stale → evaluate.
3. Evaluation inputs: current principal state (incl. expiry vs injected
   clock), current revocation set, the rule, and the concrete envelope
   (attribute values participate — a rule can bind envelope attributes
   against principal claims).
4. Deny → suppress delivery, count `authz_suppressed`, no client signal
   (the subscription may later be swept by revocation/expiry handling; a
   suppressed delivery alone is silent by design — documented).
5. Allow → cache under current epoch → enqueue.
6. Re-check epoch at enqueue commit; a revocation that landed between
   evaluate and enqueue wins (the suppressed-delivery guarantee is
   "after node-local apply", stated exactly).

Expiry timeline (FR-AUTH-012), numbered: warning ping at T−60 s (wheel);
at T: epoch advance for the principal's entries → publish-time checks fail
closed → per-subscription `Error` (`TOKEN_EXPIRED`) → 4403 close. Edge
cases: expiry during a replay visit (replay passes the same publish-time
call — R7 inherits this contract); expiry exactly at evaluation instant
(clock comparison is `!before(expiry)` — expiry instant denies); skew
policy (±30 s applies to validation at authenticate time only, never to
the expiry cut).

Revocation timeline (FR-AUTH-013), numbered: admin/API intake → audit
record → control publish → each node applies to revocation set + epoch
advance (this instant is the "no delivery after" line) → sweep walks
affected entries: `GRANT_REVOKED` errors, 4403 closes for fully revoked
principals. Partial classes: `scope` revocation re-evaluates rules rather
than closing connections; `key` revocation kills API-key principals only.

Degraded mode (FR-AUTH-015/016): control-heartbeat timer (10 s) per bus;
on loss, `fail_closed` suspends deliveries for principals of revocable
modes (JWT with revocation feed, API key, custom) while `none`-mode dev
traffic continues; `fail_open_bounded` continues until the staleness
ceiling then suspends. Recovery requests a revocation-set snapshot
(`conduit_ctl/v1 {kind: "snapshot_request"}`) and reconciles before
resuming normal mode. Both paths are deterministic-tested with the memory
bus's partition injection.

### 8.5 Implementation tickets and sequence

1. **R3.01 — Principal and epochs.** Structs, bounds, epoch source,
   immutability. DoD: bounds and immutability tests.
2. **R3.02 — Rule engine.** YAML schema, loader, startup validation,
   evaluator (allOf/anyOf/not depth 4, claim refs, arg bindings). DoD:
   the six worked examples from AUTHORIZATION_MODEL plus invalid-rule
   corpus; property test: evaluator is total (never panics, always
   allow/deny) over generated rules×principals.
3. **R3.03 — OIDC/JWT mode.** JWKS cache (bounded refresh, kid
   single-flight), full validation checklist, claim mapping. DoD:
   fixture-server matrix incl. alg confusion (`none`, HS/RS swap), kid
   misses, skew boundaries, JWKS outage behavior (cached keys honored to
   TTL, then fail closed).
4. **R3.04 — API-key mode.** Salted-hash store, constant-time compare,
   metadata, rotation states. DoD: timing-oracle test (statistical),
   rotation fixture.
5. **R3.05 — Custom authorizer.** v1 contract client (UDS/loopback), 2 s
   timeout fail-closed, decision TTL, revocation feed intake. DoD:
   contract fixtures, timeout/malformed fail-closed, feed→control-path
   test.
6. **R3.06 — Subscribe-time enforcement.** Wire point into the R2
   pipeline; uniform 4403/typed errors. DoD: allow/deny matrix over
   modes×rules; unauthenticated subscribe impossible by construction
   (state table already guarantees pre-ack rejection — cross-test).
7. **R3.07 — Publish-time enforcement + cache.** The §8.4 algorithm with
   enqueue-commit epoch re-check. DoD: allow/deny/suppress fixtures;
   cache hit/stale behavior; the revoke-between-evaluate-and-enqueue race
   test (deterministic interleaving harness).
8. **R3.08 — Revocation set and control path.** Set, apply, sweep, audit
   records, bounded set with expiry-based eviction. DoD: timeline
   fixtures for all revocation classes; eviction bounds.
9. **R3.09 — Expiry handling.** Wheel integration, warning ping, cut,
   errors, close. DoD: full timeline fake-clock test; skew edge cases.
10. **R3.10 — Degraded mode.** Heartbeat, both policies, snapshot
    reconcile. DoD: partition-injection timelines for both policies incl.
    heal reconciliation.
11. **R3.11 — Field-level auth for queries/mutations.** Execution-path
    rule evaluation, null propagation, uniform errors; introspection
    admin-only mode completes (FR-GQL-010). DoD: field matrix incl.
    error-shape uniformity probes.
12. **R3.12 — Tenancy enforcement.** Tenant resolution per mode,
    multi-tenant rejection rules, per-tenant shard addressing, cross-
    tenant probes. DoD: instrumented-index probe shows zero cross-tenant
    candidates over adversarial corpus.
13. **R3.13 — Redaction hardening.** Canary corpus extended with
    credentials/JWKS/rule internals across all sinks. DoD: sweep green
    (terminal evidence moves to R8 when diagnostics sink exists).
14. **R3.14 — Authorization matrix + gate assembly.** The full
    principal×field×state matrix from OPERATIONS_TEST_PLAN AUTHZ family,
    mutation testing enabled on `auth/**` and rule evaluator
    (NFR-MAINT-002 partial), `gate-r3` job. DoD: job required and green;
    mutation score threshold ≥ 85% on the named packages recorded.

### 8.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| authz matrix | Any principal×field×state cell deviates from the documented decision. | Full matrix (≥3 modes × ≥6 rule shapes × allow/deny/expired/revoked/degraded states) matches expected decisions exactly. |
| revoke-then-publish | An event published after node-local apply is delivered. | Deterministic interleaving: zero deliveries after apply instant across 10k randomized-seed interleavings (seeds logged). |
| expire-mid-subscription | A delivery occurs at or after the expiry instant. | Fake-clock timeline: warning ping at T−60 s, zero deliveries ≥ T, typed errors, 4403. |
| stale-cache probe | A cached allow survives an epoch advance. | Cache probe after revocation/reload/expiry re-evaluates; instrumentation proves zero stale-hit deliveries. |
| enqueue-commit race | Revocation between evaluate and enqueue still delivers. | Interleaving harness: the commit-time epoch check suppresses; counter increments. |
| JWT adversarial | Any alg-confusion, kid, skew, or claim-mapping fixture authenticates. | Full JWT adversarial corpus rejected with uniform 4403; logs carry detail, client sees none. |
| authorizer fail-closed | Timeout or malformed custom-authorizer response allows. | Both fail closed with typed category; latency bounded by the 2 s budget. |
| cross-tenant probes | Any tenant-A entry appears in tenant-B candidate sets. | Instrumented matcher over adversarial corpus (identical fields/args across tenants): zero cross-tenant candidates. |
| degraded-mode policies | fail_closed delivers, or fail_open_bounded exceeds its ceiling. | Partition timelines: suspensions and resumptions exactly per policy; health output reflects state. |
| uniform errors | Deny responses differ by cause in client-visible ways. | Response-shape differ probe across causes shows byte-identical structure modulo IDs. |
| mutation testing | Mutants of decision code survive the suite. | ≥85% mutation score on `auth/**` and rule evaluator; survivors triaged in-repo. |

### 8.7 Failure and security cases

- The named enforcement points are the only paths to registration and to
  enqueue; archcheck forbids `queue` imports from any package that could
  bypass `fanout`'s authorize stage.
- Rule-author mistakes fail startup, never at decision time; the evaluator
  is total.
- Revocation-set memory is bounded; a revocation flood (THREAT_MODEL §7.5)
  degrades to fail-closed suspension, never OOM.
- Auth-mode outages (JWKS, custom authorizer) never widen access: every
  outage path is fail-closed with typed categories and health signals.
- Timing side channels: constant-time key compare; JWT and API-key deny
  paths latency-equalized within test tolerance.

### 8.8 Migration, documentation, and installation work

Docs: auth-mode configuration reference, rule cookbook (the six examples),
the revocation and expiry operational timelines, degraded-mode operator
guide. `conduit validate` extends to rule files. Marketing register: R3
permits "authorization enforced at subscribe and at publish, with tested
revocation and expiry mid-subscription — single node"; the fleet-SLO claim
remains locked until R5.

### 8.9 Acceptance evidence

- `gate-r3` green and required: §8.6 rows plus AUTHZ family rows with
  earliest-gate R3.
- Mutation-testing report ≥85% on named packages, committed under
  `reports/`.
- The two flagship timelines (revoke-mid, expire-mid) recorded as readable
  transcripts in the acceptance PR.
- Cross-tenant probe suite green with instrumentation evidence.
- Claims register updated; lint green.

### 8.10 Explicit deferrals

R3 defers: predicate compilation and the counting index (R4 — the oracle
still matches); NATS transport for control messages and the measured
fleet propagation SLO (R5); quotas and rate limits (R6); resume
interaction with expiry/revocation beyond the stated contract (R7 proves
it); the CEL-style expression language, in-band token refresh, and
per-field event masking (OPEN_QUESTIONS; masking is explicitly
unsupported in v1 — a delivery passes whole or is suppressed whole).

### 8.11 Requirements traced

R3 terminally owns `FR-AUTH-001` through `FR-AUTH-013`, `FR-AUTH-016`,
`FR-AUTH-017`, `FR-AUTH-018`, `FR-GQL-010`, `FR-GQL-012`, `NFR-SEC-002`,
and `NFR-SEC-006`. It advances `FR-AUTH-014`/`FR-AUTH-015` (semantics
proven on memory bus; fleet SLO terminal R5) and `NFR-SEC-004`
(terminal R8).

## 9. R4 — Predicate Index Beats the Linear Scan

**Status:** planned.

**Effort range:** 4–6 focused weeks. May run in parallel with R5 after R3
(§1.2).

### 9.1 Why this gate exists

Matching cost is the scalability thesis: with a linear scan, publish cost
grows with subscription count and the 50k-connection target dies at the
matcher. R4 builds the predicate compiler and the counting attribute index
(ADR-0006), proves exact equivalence against the retained linear-scan
oracle by property testing, and publishes the benchmark that shows where
and by how much the index wins (FR-FILT-010). Both implementations exist
permanently; only the index is production-selectable.

### 9.2 Prerequisites

- R3 accepted (entries carry principals and rules; matching feeds the
  authorize stage).
- BENCHMARK_PLAN W7 (index microbench) workload definition frozen.
- Property-testing harness with seed logging (NFR-MAINT-006) in place.

### 9.3 Owned files, interfaces, and state

R4 owns `internal/filter/{predicate,index}`, the compiler from coerced
subscription arguments to predicate IR, and the `PredicateIndex` port
implementation. The oracle gains the full predicate grammar (it matched
raw-arg equality in R2/R3).

Predicate IR:

```go
type Predicate struct {
    Attr string
    Op   PredicateOp // Eq, In, Gt, Gte, Lt, Lte, Between, Present
    Val  AttrValue   // typed scalar; In carries []AttrValue ≤ 100
}

type CompiledSubscription struct {
    Conjunctions []Conjunction // ≤ 8 after disjunction normalization
}

type Conjunction struct {
    Predicates []Predicate // K ≤ 32 (attribute-count bound)
    Residual   []ResidualPredicate // non-indexable; per-field ceiling
}
```

Index shards per (tenant, field): equality hash sub-indexes
(`attr → value → entry list`), interval sub-indexes (sorted endpoint
arrays, binary search), per-entry conjunction count K, pooled per-publish
counter arrays, epoch-snapshot (copy-on-write shard descriptor) for the
publish path.

### 9.4 Algorithms and state behavior

Compilation (numbered): coerced args → predicate list per §7.4 of the PRD
grammar → type check against `@filterable` declarations → disjunction
normalization (cartesian, abort > 8 with typed error) → residual
classification → `CompiledSubscription`. Edge cases: `between` with
inverted bounds (subscribe-time error); `in` with duplicates (deduped);
timestamp attributes in mixed precisions (normalized to unix ms at
compile and at envelope seal — both sides tested); null/absent attribute
vs `Present` (absent fails `Present`, fails every comparison, per
documented three-way rule: absent ≠ null; envelopes may carry explicit
null which fails comparisons but satisfies nothing except `Eq null` —
enumerated in fixtures).

Counting match (numbered): for each envelope attribute, probe equality
sub-index (exact) and interval sub-index (range stab); accumulate hits in
the pooled counter array indexed by entry slot; entries with count == K
enter the candidate set; append residual-list scan results; hand the set
to the authorize stage. Complexity: O(A·log N + C + R). The counter pool
returns arrays on pipeline completion; leak = allocation-test failure.

Churn: subscribe/unsubscribe mutate a shard under its lock and publish a
new epoch descriptor; in-flight matches complete on the old snapshot; the
enqueue-commit epoch guard (R3) covers the semantic race.

### 9.5 Implementation tickets and sequence

1. **R4.01 — Predicate IR and compiler.** Grammar, type checks,
   normalization, bounds. DoD: compiler corpus incl. every edge case
   above; property: compile is total on generated arg sets.
2. **R4.02 — Oracle grammar completion.** Full predicate evaluation in
   the scan matcher. DoD: oracle fixture matrix; oracle becomes the
   qualified differential reference.
3. **R4.03 — Equality sub-index.** Hash structure, entry lists, slot
   allocation. DoD: unit matrix; bench harness hook.
4. **R4.04 — Interval sub-index.** Endpoint arrays, stab queries, rebuild
   policy on churn threshold. DoD: boundary matrix (open/closed ends,
   duplicates, adjacent intervals).
5. **R4.05 — Counting matcher.** Counter pool, candidate assembly,
   residual append. DoD: zero-alloc match on the steady-state path
   (alloc test); correctness fixtures.
6. **R4.06 — Epoch snapshots and churn.** COW descriptors, lock
   discipline. DoD: race-detector churn suite (mixed subscribe/publish/
   unsubscribe at 10k entries) clean.
7. **R4.07 — Differential property suite.** Generators over the full
   grammar × envelope space; index ≡ oracle on candidate sets. DoD: ≥1M
   generated cases in nightly, ≥100k in PR CI, seeds logged; zero
   divergence.
8. **R4.08 — Residual ceiling and metrics.** Ceiling rejection, FR-FILT-
   009 metric set. DoD: ceiling boundary tests; metrics-contract rows.
9. **R4.09 — W7 benchmark.** The naive-vs-index microbench across entry
   counts 1k/10k/100k/500k and match-rate distributions, benchstat
   treatment, report template. DoD: published report under `reports/`
   showing crossover and ≥10× advantage at 100k entries on Env-A;
   NFR-PERF-002 ceiling met.
10. **R4.10 — Production selection and gate assembly.** Index wired as
    the only production matcher; oracle selectable in tests only
    (archcheck rule); `gate-r4` job. DoD: job required and green.

### 9.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| compiler corpus | Any grammar form compiles wrongly or panics. | Every documented form and edge case yields the expected IR or typed subscribe-time error. |
| differential property | Index and oracle candidate sets ever differ. | ≥100k PR / ≥1M nightly generated cases with zero divergence; failing seeds reproduce deterministically. |
| interval boundaries | An endpoint-adjacent value stabs wrongly. | Open/closed/duplicate/adjacent boundary matrix exact. |
| absent-vs-null | Absent attribute satisfies any comparison or Present. | Three-way semantics fixtures exact per the documented rule. |
| disjunction bound | A 9-conjunction expansion registers. | Bound rejection with typed error naming the bound; 8 registers. |
| residual ceiling | Entry 1,001 registers on the residual list. | Ceiling rejection typed; metric reflects length. |
| churn race | Concurrent churn corrupts a shard or match. | Race suite clean; in-flight matches complete on old epoch; no lost/duplicate registration. |
| zero-alloc match | A steady-state match allocates. | Allocation regression test: 0 allocs/op on the match path at 100k entries. |
| W7 benchmark | Index fails to beat scan at 10k+ entries or misses the 1 ms p99 at 100k. | Published Env-A report: crossover ≤ 10k entries, p99 ≤ 1 ms at 100k, benchstat-significant. |

### 9.7 Failure and security cases

- Predicate bombs (maximal In lists × maximal conjunctions × churn) are a
  named hostile fixture; memory stays within the entry-count budget
  formula (ARCHITECTURE §16).
- Index poisoning via pathological churn (register/unregister storms) is
  bounded by subscribe-path quotas (R6) — R4 documents the dependency and
  tests structural integrity under storm without quotas.
- A divergence found by the differential suite after acceptance is a
  release-blocking incident (OPERATIONS_TEST_PLAN §17), not a bug-backlog
  item.

### 9.8 Migration, documentation, and installation work

Docs: filter authoring guide for schema authors (`@filterable`, supported
operators, the residual and disjunction bounds, the absent/null rule),
index metrics reference. The W7 report is published in-repo with the
BENCHMARK_PLAN report template. Marketing register: R4 permits "sublinear
filter matching, benchmarked ≥10× over linear scan at 100k subscriptions
(microbenchmark, single process)" — the L0 claims-ladder caveat applies
verbatim.

### 9.9 Acceptance evidence

- `gate-r4` green and required: §9.6 rows plus INDEX family rows.
- W7 report published with env manifest, seeds, benchstat output.
- Mutation testing extended to `filter/**` with ≥85% score.
- Claims register updated; lint green.

### 9.10 Explicit deferrals

R4 defers: NATS and fleet behavior (R5); quota protection of the
subscribe path (R6); any macro-latency claim (R9 — W7 is L0
microbenchmark evidence only); interval-tree upgrade for endpoint churn
(OPEN_QUESTIONS entry with its measured trigger).

### 9.11 Requirements traced

R4 terminally owns `FR-FILT-001` through `FR-FILT-010` and `NFR-PERF-002`.
It advances `NFR-PERF-005` (zero-alloc match; the full delivery-path
allocation contract closes in R6) and `NFR-SCALE-004` (structure proven;
the loaded-node measurement closes in R9).

## 10. R5 — Cross-Node Fanout Under Node Loss and Bus Partition

**Status:** planned.

**Effort range:** 5–7 focused weeks. May run in parallel with R4 after R3
(§1.2); R6 requires both.

### 10.1 Why this gate exists

A mutation on any node must reach subscribers on every node, and the
failure modes of the transport that makes that true — node loss, bus
partition, bus backlog, duplication, reordering — must have specified,
tested behavior rather than emergent behavior. R5 binds the NATS adapter
behind the `Bus` port (ADR-0004), proves the deterministic fault matrix on
the memory bus first, then proves the broker-specific behaviors against a
real NATS server, and measures the fleet revocation-propagation SLO that
R3 could only prove semantically.

### 10.2 Prerequisites

- R3 accepted; R4 accepted or running in parallel (R5's fleet suites run
  with the oracle matcher until R4 lands, and re-run with the index before
  R5 acceptance — both runs are acceptance evidence).
- ADR-0004 and ADR-0005 semantics restated as executable fault-matrix
  fixtures.
- NATS server version range pinned in OPERATIONS_TEST_PLAN §2;
  testcontainer and 3-node kind fixtures available.

### 10.3 Owned files, interfaces, and state

R5 owns `internal/bus/nats`, the completion of `internal/fanout` (dedupe
window, degraded-mode signals, per-stage metrics), the control-message
paths over real transport (revocation, snapshot, drain announce), and
`test/fault`.

NATS adapter mapping:

```go
type natsBus struct {
    conn *nats.Conn // pinned nats.go; reconnect handled by Conduit policy,
                    // library auto-reconnect configured with bounded buffer
}
// Subject mapping:
//   publish:  conduit.<tenant>.pub.<field-hash>
//   control:  conduit.<tenant>.ctl.<kind>
//   snapshot: conduit.<tenant>.ctl.snapshot.<node>
// Pending limits: 64 MiB / 65,536 msgs per subscription; overrun -> BusEvent{SlowConsumer}
```

Dedupe window: per (tenant, field) ring of publish IDs with a 60 s
horizon on the injected clock; membership probe before match dispatch;
window memory bounded by publish rate × horizon with a configured ceiling
(default 262,144 IDs per node) and drop-oldest eviction (an evicted ID
that recurs is delivered twice — the at-most-once contract is per the
window bound; documented honestly in ARCHITECTURE §10 and the API docs).

### 10.4 Algorithms and state behavior

Fleet consume path (numbered): bus deliver → envelope decode (unknown
version → counted reject) → dedupe probe → tenant shard match → authorize
→ enqueue (all per earlier gates; R5 adds no new hot-path stage, only the
real transport).

Node loss (ADR-0005 contract), behavior specification:

1. Node dies: its connections drop at the TCP layer; no state migration.
2. Other nodes observe nothing except (a) bus membership events if
   configured and (b) reconnect load arriving through the LB.
3. Reconnecting clients present resume tokens; R5 predates resume (R7),
   so the R5 contract is: reconnects are fresh subscribes; the fleet
   suite asserts continued correct delivery for surviving nodes during
   and after the kill, and correct fresh-subscribe service to
   reconnecting clients within accept-pacing bounds.

Bus partition (FR-FAN-006), numbered:

1. Heartbeat loss on control subjects (10 s) and/or bus client
   disconnect events → node enters `bus_degraded`.
2. Local publishes continue to local subscribers (the memory-path stages
   are intact); envelopes destined for the bus are dropped with the
   `publish_rejected`-class counter after the client-visible mutation
   already succeeded — the mutation's publish step reports partial
   delivery in `extensions` (documented, tested).
3. Readiness reflects degraded state; FR-AUTH-015 policy governs
   revocation staleness.
4. Heal: bus reconnect → snapshot reconcile (revocations) → resume
   consuming from now (no replay expectation; the missed-envelope gap is
   the documented cost, and R7's buffers do not cross nodes).

Bus backlog (FR-FAN-007): NATS pending-limit overrun surfaces
`SlowConsumer`; Conduit drops the overrun batch (the library already
did), counts, logs, health-flags, and — if overruns persist beyond a
configured window — escalates to `bus_degraded` so operators see a node
that cannot keep up rather than silent loss.

Duplication and reorder: dedupe window absorbs duplicates within horizon
(FR-FAN-008); per-publisher order within a subject is a NATS guarantee
verified (not assumed) by the broker suite; cross-publisher reorder is
out of contract (FR-FAN-004).

Revocation SLO measurement: the fleet fixture publishes timestamped
revocations from an admin node and measures node-local apply lag on all
nodes across 1,000 revocations under load; p99 ≤ 2 s is the acceptance
line (FR-AUTH-014).

### 10.5 Implementation tickets and sequence

1. **R5.01 — NATS adapter.** Connection lifecycle, TLS+creds, subject
   mapping, pending limits, event mapping to `BusEvent`. DoD:
   testcontainer suite for connect/publish/subscribe/reconnect/slow-
   consumer event surfacing.
2. **R5.02 — Dedupe window completion.** Ring, horizon, ceiling,
   eviction semantics, metrics. DoD: duplication fixtures incl.
   horizon-eviction recurrence (documented double-delivery case).
3. **R5.03 — Degraded-mode integration.** Bus events → degraded state
   machine → readiness/health/metrics; partial-publish extensions
   signal. DoD: memory-bus partition timelines; mutation extensions
   fixture.
4. **R5.04 — Control-path over real transport.** Revocation, snapshot
   request/response, drain announce over NATS subjects. DoD:
   testcontainer control matrix; snapshot reconcile after partition.
5. **R5.05 — Deterministic fault matrix.** The full memory-bus matrix:
   partition (one node, majority, full), delay, reorder, duplication,
   node kill mid-burst, kill during control apply. DoD: every FAN family
   row with earliest-gate R5 green deterministically.
6. **R5.06 — Broker integration suite.** The same scenarios against real
   NATS in CI containers: broker restart, node kill, induced
   slow-consumer, TLS-auth failure modes. DoD: nightly broker suite
   green 5 consecutive runs (flake policy: container class).
7. **R5.07 — 3-node fleet fixture.** kind-based fleet with LB, scripted
   clients, kill/partition orchestration. DoD: the §1.2 R5 capability
   demonstrated: publish on A delivers on B and C; kill C mid-stream
   leaves A/B correct.
8. **R5.08 — Revocation SLO measurement.** The 1,000-revocation lag
   harness under load. DoD: report under `reports/` with p50/p95/p99;
   p99 ≤ 2 s on the fleet fixture.
9. **R5.09 — Publish rate limiting.** Per-tenant token bucket at the
   seal stage. DoD: boundary and burst fixtures; typed rejection naming
   the limit class.
10. **R5.10 — Admin publish path.** `/admin/v1/publish` (admin listener
    is a minimal stub until R8; the endpoint lands behind the same
    versioned router) through seal → bus with full validation. DoD:
    injected envelopes indistinguishable downstream from mutation
    publishes (fixture equality).
11. **R5.11 — Index/oracle dual-run and gate assembly.** Fleet suites
    re-run with the R4 index if it landed after the first run; `gate-r5`
    job. DoD: both matcher configurations green; job required.

### 10.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| cross-node delivery | An envelope from node A misses a matching entry on B or C. | Fleet fixture: 100% delivery to matching entries across nodes at reference fixture load (no backpressure engaged). |
| node kill mid-burst | Survivor delivery corrupts, stalls, or duplicates during the kill. | Kill C at peak: A/B delivery correctness and ordering unaffected (per-publisher sequence check); reconnect-as-fresh service within pacing bounds. |
| partition: isolated node | Isolated node stops local delivery or serves silently stale auth. | Local-to-local delivery continues; degraded flag set within 10 s fake-clock (memory bus) and observed window (NATS); FR-AUTH-015 policy applied. |
| partition heal | Heal replays stale envelopes or skips snapshot reconcile. | Post-heal: consumption resumes from now; revocation snapshot reconciled before normal mode; no fabricated replay. |
| duplication | A duplicate within horizon delivers twice. | Dedupe fixtures: single delivery within horizon; documented recurrence case beyond horizon delivers and is counted. |
| ordering | Per-publisher per-field order breaks across the bus. | Sequence-stamped publisher fixtures: monotone order at every subscriber across 1M envelopes (broker suite). |
| backlog overrun | Overrun buffers unboundedly or drops silently. | Induced slow node: drops counted, health-flagged, escalation to degraded after window; RSS bounded. |
| control under partition | A revocation issued during partition is lost after heal. | Snapshot reconcile applies it; post-heal delivery suppression proven. |
| revocation SLO | p99 apply lag exceeds 2 s under load. | 1,000-revocation report: p99 ≤ 2 s on the 3-node fixture. |
| envelope versioning | An unknown-version envelope is partially interpreted. | Version-bump fixtures: counted rejection, no delivery, no crash. |
| publish rate limit | Over-limit publish silently drops or kills the mutation. | Typed `publish_rejected` in mutation extensions; under-limit unaffected. |

### 10.7 Failure and security cases

- Bus credentials are secrets: never logged, never in diagnostics
  (canary rows); TLS to NATS required in non-dev profiles (NFR-SEC-005
  advanced here, terminal R8).
- A forged control message from a bus position is the THREAT_MODEL §7.7
  case: control messages are accepted only from the bus connection
  Conduit authenticated; subject ACLs on the NATS server are documented
  as required deployment configuration, and the runbook (R8) carries the
  verification step.
- Envelope payload bytes cross nodes: size bounds are enforced at seal
  and at decode, so a compromised or buggy peer cannot balloon memory.
- The partial-delivery mutation signal must never claim full delivery
  during degraded mode (no-fabricated-success rule; fixture-tested).

### 10.8 Migration, documentation, and installation work

Envelope v1 and control v1 gain cross-version fixtures (the first real
NFR-COMPAT-005 artifacts: N/N+1 decode tables checked in). Docs: bus
deployment guide (NATS sizing, subject ACLs, TLS), degraded-mode operator
semantics, the partial-delivery contract. Marketing register: R5 permits
"cross-node fanout with tested node-loss and partition behavior; measured
revocation propagation p99 ≤ 2 s on the reference 3-node fixture" —
fixture-scoped, not a general fleet claim (that is R9's L3).

### 10.9 Acceptance evidence

- `gate-r5` green and required: §10.6 rows, FAN family rows, broker
  nightly green streak of 5.
- Fault-matrix transcripts (kill, partitions, heal) in the acceptance PR.
- Revocation SLO report published under `reports/`.
- Both matcher configurations (oracle, index) green on the fleet suites.
- Claims register updated; lint green.

### 10.10 Explicit deferrals

R5 defers: resume across reconnects (R7 — reconnects are fresh
subscribes here); backpressure policies and quotas under adversarial load
(R6); fleet-scale performance numbers (R9 — R5 fixtures prove
correctness, not capacity); alternative bus adapters (OPEN_QUESTIONS);
NATS JetStream anything (ADR-0004 rejection stands).

### 10.11 Requirements traced

R5 terminally owns `FR-FAN-001` through `FR-FAN-012`, `FR-AUTH-014`,
`FR-AUTH-015`, and `NFR-SEC-003`. It advances `NFR-PERF-003` and
`NFR-SCALE-003` (measured properly in R9) and `NFR-COMPAT-005` (first
fixtures; terminal R10).

## 11. R6 — Backpressure, Quotas, and Slow Consumers Under Adversarial Load

**Status:** planned.

**Effort range:** 4–6 focused weeks.

### 11.1 Why this gate exists

A gateway that holds tens of thousands of sockets is, to an attacker or a
bad network, a memory-allocation service. R6 makes the bounded-everything
principle real where it is hardest: per-connection outbound queues with
the three overflow policies, connection and subscription quotas, inbound
rate limits, fd-budget load shedding, and the proof obligation that
matters — under deliberately stalled consumers and hostile load, node
memory stays within budget and unaffected connections stay within
latency budget. The zero-allocation delivery-path contract also closes
here.

### 11.2 Prerequisites

- R4 and R5 accepted (the delivery path being protected is the real
  one: index matching, fleet fanout).
- Stalled-consumer fixture (scriptable read-stall client) and heap
  instrumentation harness in place.
- THREAT_MODEL §7.9 resource-exhaustion cases restated as fixtures.

### 11.3 Owned files, interfaces, and state

R6 owns the completion of `internal/queue` (the `drop_oldest` and
`coalesce_by_key` policies, drop accounting, client notices), quota
accounting in `internal/registry`, inbound rate limiting in
`internal/protocol`, fd-budget tracking in `internal/transport`, and the
`@backpressure` directive's full semantics in `internal/graphql/schema`.

Queue policy execution (per subscription, on enqueue when full):

```go
type EnqueueResult uint8 // Enqueued, DroppedOldest, Coalesced,
                         // DisconnectInitiated, DeadEpoch

type BackpressureConfig struct {
    Policy      BPPolicy // DropOldest | CoalesceByKey | Disconnect
    QueueBound  int      // per-subscription share accounting, default derives
    CoalesceKey KeyExpr  // compiled from @backpressure(coalesceKey:)
}
```

Coalesce-key extraction is compiled at schema load against the payload
type; extraction failure at runtime (payload missing the key) falls back
to `drop_oldest` for that event with a distinct counter
(`coalesce_key_missing`) — never a crash, never silent.

### 11.4 Algorithms and state behavior

Enqueue with policy (numbered):

1. Guard epoch (R3/R4 contract).
2. Reserve queue capacity (messages and bytes); success → enqueue,
   return.
3. Full + `drop_oldest`: evict the oldest queued `Next` belonging to the
   same subscription (never another subscription's message); count;
   stamp the drop notice onto the next delivered message's extensions;
   enqueue new.
4. Full + `coalesce_by_key`: extract key; scan the subscription's queued
   messages for the key (bounded scan — per-subscription queue share);
   replace payload in place preserving queue position; on no match,
   behave as drop_oldest step 3; on extraction failure, distinct counter
   then step 3.
5. Full + `disconnect`: initiate 4704 close (control lane), mark entries
   dead, count.
6. Edge cases: queue full entirely of control-lane-exempt data for other
   subscriptions (eviction is per-subscription — a subscription with
   zero queued messages and policy drop_oldest on a full connection
   queue evicts from the connection's largest-queue subscription only if
   configured `shared_queue: true`; default is per-subscription shares
   so cross-subscription starvation is structural, not incidental);
   coalesce on a 1-slot share (replace-or-drop degenerates correctly);
   drop notice itself never allocates beyond the pooled extension
   buffer.

Quotas: connection quota check at `connection_init` (4703);
subscription quotas at subscribe (typed error, not close); per-tenant
aggregates maintained in the registry with lock-free counters; inbound
rate limiting token bucket per connection with warning then 4400-class
close; publish-side quotas were R5.

Fd budget: transport tracks open fds against the soft ceiling (default:
rlimit − 2,048 reserve); beyond it, upgrades are refused with 503 and a
`Retry-After`, readiness degrades, accepts pause; recovery is
hysteretic (resume at ceiling − 5%).

### 11.5 Implementation tickets and sequence

1. **R6.01 — Policy engine.** The §11.4 enqueue algorithm, all three
   policies, share accounting. DoD: policy matrix incl. every edge case
   named above.
2. **R6.02 — Coalesce-key compilation.** Schema-load compile, payload
   binding, failure fallback. DoD: extraction fixtures incl. missing
   and mistyped keys.
3. **R6.03 — Drop accounting and notices.** Counters, extension
   notices, pooled buffers. DoD: notice-content fixtures; alloc test on
   the notice path.
4. **R6.04 — Quota engine.** Connection/subscription/tenant quotas,
   4703 and typed-error paths. DoD: boundary matrix at limit and ±1
   across all quota classes.
5. **R6.05 — Inbound rate limiting.** Token bucket, warning, close
   escalation. DoD: burst/sustained fixtures; hostile-suite
   integration.
6. **R6.06 — Fd budget and load shed.** Tracking, refusal, hysteresis,
   readiness integration. DoD: fd-exhaustion fixture (rlimit-lowered
   container) sheds before exhaustion and recovers.
7. **R6.07 — Stalled-consumer suite.** The adversarial centerpiece:
   5% of 10k CI-scale connections stall reads under W2-shaped load;
   assert per-policy behavior, bounded RSS, unaffected-connection
   latency. DoD: BP family rows green with heap evidence artifacts.
8. **R6.08 — Delivery-path allocation closure.** End-to-end zero-alloc
   assertion (consume→dedupe→match→authorize→enqueue incl. notices).
   DoD: allocation regression test 0 allocs/op wired as a required
   check (NFR-PERF-005 terminal).
9. **R6.09 — DoS suite integration.** THREAT_MODEL §7.9 fixtures:
   connection floods, subscription floods, publish floods against
   quota walls, slowloris (handshake and read), log-amplification
   probes with rate-limited logging. DoD: hostile suite extended; node
   survives the full battery in one process with RSS returning to
   baseline band.
10. **R6.10 — Gate assembly + docs.** `gate-r6` job; backpressure
    authoring guide; quota operator guide. DoD: job required and green.

### 11.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| policy matrix | Any policy at queue-full deviates from §11.4. | drop_oldest evicts own-subscription oldest; coalesce replaces in place preserving position; disconnect closes 4704 via control lane; DeadEpoch never enqueues. |
| per-subscription shares | One slow subscription starves another on the same connection. | Share fixtures: sibling subscription delivery unaffected by a saturated share (default mode). |
| coalesce edge cases | Missing key crashes, silently drops, or miscounts. | `coalesce_key_missing` counter increments; fallback drop_oldest engages; 1-slot share degenerates correctly. |
| drop notices | A drop is invisible to the client or allocates unboundedly. | Next delivered message carries `conduit.dropped{count,policy}`; notice path 0 allocs/op. |
| quota boundaries | Any quota at limit±1 misbehaves. | Full boundary matrix: at-limit admitted, over-limit 4703/typed-error exactly per class. |
| stalled consumers | RSS grows past budget or healthy connections degrade. | 5%-stall run: RSS within the ARCHITECTURE §16 formula band; healthy-connection enqueue latency within budget; per-policy outcomes exact. |
| zero-alloc delivery | Any hot-path stage allocates in steady state. | End-to-end allocation test: 0 allocs/op at 100k entries with notices active. |
| fd load shed | Exhaustion crashes accepts or refusal never recovers. | Lowered-rlimit fixture: 503 shedding before exhaustion, readiness degraded, hysteretic recovery. |
| DoS battery | Any flood crashes, leaks, or starves control frames. | Full battery in one process: survival, bounded RSS, control-lane latency within budget throughout. |
| rate-limit escalation | Warning/close sequence deviates or affects neighbors. | Token-bucket timeline exact; neighbor connections unaffected (measured in-suite). |

### 11.7 Failure and security cases

- Eviction fairness is a security property: an attacker must not be able
  to evict a victim subscription's events by saturating their own
  (per-subscription shares are the default; `shared_queue: true` is an
  explicit operator opt-in with the starvation risk documented where it
  is set).
- The disconnect policy is itself abusable (an attacker triggering event
  bursts to disconnect a victim); the docs state the selection guidance
  and the quota interactions that bound it (THREAT_MODEL §7.9 case).
- Log rate limiting closes NFR-SEC-009 here: hostile-input log
  amplification fixtures show bounded log bytes per connection per
  minute.
- Load shedding must fail closed for new work while never cutting
  established connections' control frames.

### 11.8 Migration, documentation, and installation work

`@backpressure` semantics graduate from validated-but-inert (R1) to
enforced; SDL fixtures with the directive gain behavior tests. Docs:
policy-selection guide with the abuse trade-offs, quota configuration
reference, the capacity implications (feeding FR-OPS-010, terminal R10).
Marketing register: R6 permits "explicit per-field backpressure policies
and quotas, proven under adversarial load with bounded memory (10k-conn
CI-scale evidence)"; the 50k figure remains locked until R9.

### 11.9 Acceptance evidence

- `gate-r6` green and required: §11.6 rows, BP and CONN family rows,
  extended hostile suite.
- Heap-evidence artifacts (RSS curves, alloc profiles) for the stalled-
  consumer and DoS batteries archived under `reports/`.
- Zero-alloc delivery check required in CI.
- Claims register updated; lint green.

### 11.10 Explicit deferrals

R6 defers: resume/replay interaction with drops (R7 — a dropped-then-
resumed event's fate is defined there: policy drops are final, resume
does not resurrect them; R6 documents the statement, R7 proves it);
drain (R8, terminal FR-CONN-010); the 50k-scale versions of every
adversarial scenario (R9 runs W9); TLS-material reload (R8 with the
admin surface).

### 11.11 Requirements traced

R6 terminally owns `FR-CONN-002` through `FR-CONN-009`, `FR-CONN-011`
through `FR-CONN-014`, `NFR-PERF-005`, `NFR-SEC-008`, and `NFR-SEC-009`.
It advances `FR-CONN-010` (drain machinery exists unpaced; terminal R8)
and `FR-OPS-010` (capacity coefficients; terminal R10).

## 12. R7 — Reconnect and Resume with a Measured Gap Window

**Status:** planned.

**Effort range:** 4–5 focused weeks.

### 12.1 Why this gate exists

Reconnects are routine — deploys, network blips, node loss, token expiry
— and without continuity every reconnect is a full client-state refetch.
R7 implements ADR-0007: replay ring buffers, signed resume tokens, the
splice algorithm, and the honesty machinery (`resume_gap`,
`resume_rejected`). Its hardest obligations are exactness at the splice
point (no duplicate, no gap, deterministically proven) and honesty about
the window (measured, published, cited by the API docs — never implied
away).

### 12.2 Prerequisites

- R6 accepted (drops are final and counted; resume must not resurrect
  them).
- PROTOCOL_CONFORMANCE resume-extension shapes frozen (they shipped as
  reserved fields in R2).
- BENCHMARK_PLAN W8 (gap window) workload frozen.
- HMAC key management design reviewed against THREAT_MODEL §7.8.

### 12.3 Owned files, interfaces, and state

R7 owns `internal/resume` (buffers, token codec, splice), the extension
wiring in `internal/protocol` and `internal/fanout` (positions stamped at
enqueue), and key management in `internal/config` (key file, rotation).

Resume token v1 (byte layout, then HMAC):

```text
| ver (1) | tenant-hash (8) | field-hash (8) | node-epoch (8) |
| position (8) | issued-unix-ms (8) | key-id (2) | hmac-sha256 (32) |
= 65 bytes binary, base64url on the wire (≈88 bytes, bound 512 covers v2 growth)
```

```go
type ResumeCodec interface {
    Issue(p Position, now time.Time) (Token, error)
    Verify(t Token, now time.Time) (Position, VerifyResult) // constant-time MAC
}

type ReplayResult struct {
    Replayed  int
    Complete  bool      // false -> resume_gap emitted
    Covered   PosRange
}
```

Positions: per-(tenant, field) monotonic uint64 within a node epoch;
node epoch is a random 64-bit value drawn at process start (no
cross-restart continuity by design — a restart is a gap, honestly).

### 12.4 Algorithms and state behavior

Append path: enqueue-stage success stamps the position into the `Next`
extensions and appends the envelope to the buffer (append happens once
per envelope per node, not per subscriber — the buffer stores envelopes,
and replay re-runs match+authorize per subscriber).

Resume splice (numbered — the exactness proof obligation):

1. `Subscribe` carries `extensions.conduit.resume.token`; verify
   (signature, tenant, field match against the subscribe; max age);
   failure → `resume_rejected` notice, proceed fresh (FR-RESUME-007).
2. Register the entry (R2 atomicity) with `delivering_replay` marker:
   live envelopes for this entry are diverted to a bounded pending-live
   staging queue (share-sized) instead of the outbound queue.
3. Replay: iterate buffer from position+1 through the same
   match→AuthorizePublish→enqueue path (suppressed deliveries suppress
   here too; policy drops during replay count normally).
4. Cutover: note the buffer head position H at iteration end; drain the
   staging queue, discarding any staged envelope with position ≤ H
   (already replayed — the no-duplicate half) and enqueueing the rest
   in order (the no-gap half); switch the entry to live delivery
   atomically under the shard lock.
5. If verify succeeded but position < buffer tail (horizon passed) or
   node-epoch mismatch: emit `resume_gap{from, to, reason}` before any
   live delivery, then proceed live-only (FR-RESUME-005).
6. Staging overflow during a pathological replay (slow client +
   fire-hose field): apply the entry's backpressure policy to the
   staging queue itself; a disconnect policy aborts the resume with
   4704 (edge case enumerated and tested).

Key rotation: key file holds ≤ 4 keys with ids; Issue uses the newest;
Verify accepts any listed; rotation is config reload; tokens outliving
all listed keys verify-fail into fresh-subscribe (documented operator
guidance: rotation cadence ≥ token max age).

Gap-window measurement (W8): publish at reference rates; disconnect
cohorts for 5/30/120/600 s; resume; measure replay completeness and
resume_gap incidence; publish horizon-seconds vs publish-rate curves
(FR-RESUME-008).

### 12.5 Implementation tickets and sequence

1. **R7.01 — Replay buffers.** Ring, bounds (4,096/16 MiB), horizon
   metrics, append integration. DoD: bound/eviction fixtures; horizon
   metric rows.
2. **R7.02 — Token codec.** Layout, HMAC, constant-time verify, key
   ring, rotation. DoD: forgery corpus (bit-flips, truncation, wrong
   key, wrong tenant/field, future issue time, over-age) all rejected
   with distinct counters; round-trip property test.
3. **R7.03 — Position stamping.** Enqueue-stage stamping, extensions
   wiring, reserved-field activation. DoD: reference-client
   invisibility re-run (extensions ignored cleanly).
4. **R7.04 — Splice implementation.** The §12.4 algorithm with staging.
   DoD: deterministic splice suite: scripted interleavings of
   replay-tail vs live-head including simultaneous arrival, empty
   replay, and staging-overflow-per-policy; zero duplicates, zero gaps
   across 10k seeded interleavings.
5. **R7.05 — Gap honesty paths.** resume_gap and resume_rejected
   notices, reasons, counters. DoD: horizon/epoch/verify-fail fixtures
   produce exact notices before any live delivery.
6. **R7.06 — Authorization and drop interaction.** Replay through
   AuthorizePublish (expiry/revocation mid-replay), R6 drops-are-final
   proof. DoD: revoked-then-resume delivers nothing post-apply;
   dropped-event positions absent from replay delivery but present in
   position sequence (client sees the seam only via dropped-notice
   counters, exactly as live).
7. **R7.07 — Reconnect pacing hints.** Retry-after jitter on 4700/4701
   closes (machinery; drain itself is R8). DoD: hint presence/jitter
   distribution tests.
8. **R7.08 — W8 measurement.** The gap-window benchmark and report.
   DoD: published horizon-vs-rate report under `reports/`; API docs
   cite it.
9. **R7.09 — Contract freeze and gate assembly.** NFR-COMPAT-003
   closure: protocol extensions, envelope, control, token formats
   versioned with cross-version fixtures; `gate-r7` job. DoD: job
   required and green; contract-freeze checklist in the acceptance PR.

### 12.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| token forgery corpus | Any forged/expired/cross-scope token verifies. | Every corpus row rejected constant-time with its distinct counter; valid round-trip exact. |
| splice exactness | Any interleaving yields a duplicate or gap at cutover. | 10k seeded interleavings: delivered position sequence is exactly (replayed ∪ staged-after-H ∪ live), strictly monotone, no repeats. |
| horizon honesty | A horizon-passed resume delivers without resume_gap. | Gap notice with exact covered range precedes first live delivery; incidence matches buffer math. |
| epoch mismatch | A post-restart resume silently pretends continuity. | Node-epoch mismatch → resume_gap{reason: node_restart}; fresh positions issued. |
| replay authorization | A revoked/expired principal receives replayed events. | Mid-replay revocation/expiry timelines: zero deliveries post-apply/cut; sweep behavior identical to live. |
| drops are final | Resume resurrects a policy-dropped event. | Dropped envelope absent from replay delivery; counters reconcile (delivered + dropped + suppressed = matched). |
| staging overflow | Pathological resume grows staging unboundedly. | Per-policy staging behavior exact; disconnect policy aborts with 4704; RSS bounded. |
| buffer bounds | Buffer exceeds count/byte bounds or leaks on field churn. | Bound fixtures incl. field removal on schema reload freeing buffers. |
| reference-client re-run | Extension activation breaks the unmodified client. | Full R2 conformance suite green with extensions active. |
| W8 report | Horizon claims lack measurement. | Published report: horizon seconds at 1k/5k envelope rates with the report template's statistical treatment. |

### 12.7 Failure and security cases

- Token harvesting (THREAT_MODEL §7.8): tokens are scope-bound (tenant,
  field) and MAC'd; a stolen token replays only what its principal
  could — replay runs full AuthorizePublish with the *resuming
  connection's current principal*, not the token's history (stated
  invariant; adversarial fixture: token issued under principal A
  presented by principal B replays under B's grants only).
- Resume probing as a buffer-scraping oracle: verify failures are
  uniform to the client, counted and rate-limited server-side.
- Buffer memory is a named node-level consumer with per-tenant ceilings
  (multi-tenant fairness; fixture-tested).
- Key-file permissions checked at startup (0600-class); world-readable
  key file refuses to start outside dev acknowledgment.

### 12.8 Migration, documentation, and installation work

This gate freezes the v1 public contracts (NFR-COMPAT-003): a
contract-change checklist enters the PR template; cross-version fixture
suites become release-blocking. Docs: client-author resume guide (the
honest one: what resume does, the gap window with the W8 numbers, how to
handle resume_gap), key-rotation runbook entry. Marketing register: R7
permits "resume with bounded replay and an honest, measured gap window
(published horizon curves)" and the developer-preview claim set becomes
complete pending R0–R7 all-green.

### 12.9 Acceptance evidence

- `gate-r7` green and required: §12.6 rows, RESUME family rows.
- Splice interleaving suite with logged seeds archived.
- W8 report published; API docs cite it by path.
- Contract-freeze checklist executed; cross-version fixtures green.
- Full conformance re-run green with extensions active.
- Claims register updated (developer-preview tier claims unlocked only
  if R0–R7 all accepted); lint green.

### 12.10 Explicit deferrals

R7 defers: cross-node buffer coverage and durable resume (ADR-0007
non-goals; OPEN_QUESTIONS carries the durable-stream reopen trigger);
drain-integrated resume rehearsal at fleet scale (R8 drain + R9 surge);
storm-scale reconnect measurement (`FR-RESUME-009`, R9).

### 12.11 Requirements traced

R7 terminally owns `FR-RESUME-001` through `FR-RESUME-008`,
`NFR-SEC-007`, and `NFR-COMPAT-003`. It advances `FR-RESUME-009`
(hint machinery; measurement terminal R9).

## 13. R8 — Observability, Admin Surface, Drain, and the Runbook

**Status:** planned.

**Effort range:** 4–5 focused weeks.

### 13.1 Why this gate exists

At 50,000 connections per node, the telemetry is the only view anyone
will ever have of the system. R8 turns the metrics skeleton into the
documented catalogue with its cardinality budget, adds sampled tracing,
completes the admin API, implements paced drain (the deploy story's
foundation), ships the reference dashboards and alerts, and writes the
runbook whose entries the chaos suite rehearses. Operability is a gate,
not a garnish: an alert without a rehearsed runbook entry fails R8.

### 13.2 Prerequisites

- R7 accepted (drain interacts with resume hints; diagnostics cover
  buffers and tokens).
- OPERATIONS_TEST_PLAN §16 metric catalogue and cardinality budget
  frozen.
- FR-OPS-008 alert list frozen (the ≥10 alerts named there).

### 13.3 Owned files, interfaces, and state

R8 owns `internal/admin` (versioned router, auth, all `/admin/v1`
endpoints), `internal/observability` completion (catalogue, tracing,
redaction at the diagnostics sink), drain orchestration in
`internal/registry`/`internal/transport`, SIGHUP/admin config and
schema reload completion (FR-OPS-003), TLS reload, and
`deploy/kubernetes` manifests' health wiring (manifests complete in
R10; probes and lifecycle hooks are R8 obligations).

Admin API authentication: bearer token (constant-time compare, separate
secret) or mTLS (client CA config); every mutating endpoint writes an
audit record (FR-ADMIN-008).

Drain state machine: `serving → draining → drained` with paced close
scheduling (uniform spread over the window with jitter, resume hints
attached per R7.07), readiness failing at `draining` entry, in-flight
operation grace, deadline hard-cut.

### 13.4 Algorithms and state behavior

Drain pacing (numbered): compute cohort size = ceil(connections /
(window / tick)); each tick closes one cohort ordered by connection age
(oldest first — they hold the most server state); each close is 4700
with a jittered retry-after spanning the remaining fleet capacity
assumption; new upgrades 503 from drain entry; subscriptions complete
normally within the operation grace; at deadline, remaining connections
hard-close 4700; process exits only when the registry is empty and the
bus consumer has cleanly unsubscribed (drain announce on the control
subject lets peers expect the load).

Schema reload (FR-OPS-003, completing the R1 validation machinery):
validate-new → diff against serving schema → removed/changed
subscription fields collect their entries → atomic swap under a
schema epoch → collected entries complete with typed
`schema_superseded` errors → buffers for removed fields freed. A
validation failure leaves the old schema serving with the failure in
admin output and logs.

Metrics catalogue enforcement: the catalogue is data
(`observability/catalogue.go`); a CI contract test walks the registry
and fails on any series absent from the catalogue or any label outside
its bound; the tenant label applies the cap-with-`other` rule beyond
the configured cardinality cap.

### 13.5 Implementation tickets and sequence

1. **R8.01 — Admin listener and auth.** Separate server, bearer/mTLS,
   audit records, never-on-client-port archcheck rule. DoD: auth
   matrix; audit fixtures.
2. **R8.02 — Inspection endpoints.** connections/subscriptions with
   pagination and field set per FR-ADMIN-002. DoD: fixture
   comparisons; no-payload-bytes canary rows.
3. **R8.03 — Metrics catalogue completion.** Every §16 metric emitted,
   contract test, cardinality caps. DoD: contract test required in
   CI; catalogue doc generated from the data.
4. **R8.04 — Tracing.** OTel wiring, sampling config, the traced
   paths from ADR-0010, span-attribute redaction. DoD: trace fixture
   assertions incl. sampled-fanout links; canary rows on span
   attributes.
5. **R8.05 — Drain.** State machine, pacing, hints, announce,
   readiness, deadline. DoD: fake-clock drain timelines at 1k/10k
   conns; drain under active load fixture.
6. **R8.06 — Reload completion.** Schema/TLS/config reload paths with
   atomic swap and typed teardown. DoD: reload-under-load fixtures;
   failed-validation no-op proof.
7. **R8.07 — Health semantics.** readyz/healthz per FR-ADMIN-005 with
   every degraded input (bus, fd, drain, revocation staleness) wired.
   DoD: health truth-table test.
8. **R8.08 — Diagnostics bundle.** Inventory-first bundle, redaction,
   size bounds. DoD: bundle inventory fixture; canary sweep
   (NFR-SEC-004 terminal evidence).
9. **R8.09 — Dashboards and alerts.** Reference Grafana dashboards
   and the ≥10 alert rules as code under `deploy/`. DoD: alert rules
   lint + fire-under-fixture tests (each alert provoked once in a
   chaos fixture).
10. **R8.10 — Runbook.** One rehearsed entry per alert plus incident
    entries (node OOM suspicion, bus outage, JWKS outage, cert
    expiry, reconnect storm). DoD: CHAOS suite scripts provoke each
    scenario; the runbook entry's diagnosis steps reference real
    metric names and admin endpoints (link-checked).
11. **R8.11 — Gate assembly.** `gate-r8` job incl. CHAOS rows with
    earliest-gate R8. DoD: job required and green.

### 13.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| admin isolation | An admin endpoint answers on the client listener. | Port-scan fixture: admin routes 404-absent on client port; archcheck rule active. |
| admin auth | Any endpoint serves without valid bearer/mTLS. | Auth matrix: every endpoint × both modes × invalid/expired/absent credentials fails closed. |
| metrics contract | An undocumented series or over-budget label ships. | Contract test enumerates registry == catalogue exactly; tenant cap engages `other`. |
| drain timeline | Pacing, hints, readiness, or deadline deviate. | Fake-clock timelines: cohort schedule exact, 4700+hints on every close, readiness fails at entry, deadline hard-cut, clean exit. |
| drain under load | Draining node corrupts deliveries or strands entries. | Fleet fixture: zero delivery errors on peers during drain; drained node's clients resume elsewhere (R7 path) with expected gap behavior. |
| reload atomicity | A failed reload leaves mixed state or drops valid conns. | Reload fixtures: failure = pure no-op; success = typed completes only on removed fields; buffers freed. |
| health truth table | Any degraded input fails to surface, or liveness flaps. | Truth-table test over all degraded-input combinations matches documented semantics. |
| diagnostics redaction | A canary appears in any bundle file. | Full canary corpus absent; inventory lists every file with sizes. |
| alert provocation | An alert cannot be provoked or fires spuriously in the fixture. | Each of the ≥10 alerts fires exactly in its provocation fixture and not in the steady-state fixture. |
| runbook linkage | A runbook step references a nonexistent metric/endpoint. | Link-check over runbook: every metric name and endpoint resolves. |

### 13.7 Failure and security cases

- Drain must be unprivileged-abuse-proof: only admin-authenticated
  triggers; SIGTERM from the platform is equivalent by design
  (Kubernetes preStop); a drain cannot be triggered via any client
  input.
- Diagnostics bundles are the highest-risk exfiltration sink; the
  inventory-first rule and canary sweep are the control (THREAT_MODEL
  §7.10).
- Tracing must never carry payload bytes or credentials in span
  attributes (canary rows); sampling config cannot exceed the
  documented ceiling without a logged override.
- Reload paths are operator-facing DoS risks (a bad schema pushed
  repeatedly); reload attempts are rate-limited and audited.

### 13.8 Migration, documentation, and installation work

Docs: complete operator guide (config reference generated from the
schema, health semantics, drain procedure, reload procedure), the
dashboards/alerts installation guide, the runbook itself. Marketing
register: R8 permits "operable: documented metrics catalogue, paced
drain, rehearsed runbook" with fixture-scale caveats.

### 13.9 Acceptance evidence

- `gate-r8` green and required: §13.6 rows, ADMIN/CHAOS families.
- Every alert's provocation transcript archived.
- Diagnostics canary sweep green (NFR-SEC-004 terminal).
- Drain-under-load fleet transcript in the acceptance PR.
- Claims register updated; lint green.

### 13.10 Explicit deferrals

R8 defers: scale-level drain/reconnect measurement (R9 W6); packaging
of dashboards into the release artifact set (R10); admin API additions
beyond `/admin/v1` (versioned future work); log shipping/aggregation
integrations (operator-side, documented as out of scope).

### 13.11 Requirements traced

R8 terminally owns `FR-ADMIN-001` through `FR-ADMIN-008`, `FR-CONN-010`,
`FR-OPS-003`, `FR-OPS-008`, `FR-OPS-009`, `NFR-SEC-004`, and
`NFR-SEC-005`. It advances `FR-OPS-004`/`FR-OPS-006` (probes and
lifecycle wiring; terminal R10).

## 14. R9 — The Measured Scale Target, Published Honestly

**Status:** planned.

**Effort range:** 3–5 focused weeks (dominated by environment work and
run repetition, not code).

### 14.1 Why this gate exists

Every number in the marketing plan, the capacity model, and the 1.0
support statement traces to this gate. R9 executes BENCHMARK_PLAN's
workload catalogue on the named reference environments, publishes the
reports with the required statistical treatment, and either meets the
targets or revises them by ADR — never by quietly weakening a sentence.
R9 also runs the adversarial scale scenarios (stalled consumers, expiry
storms, node-loss surges) at target scale, because a 50k claim proven
only on polite traffic is an idle-pool claim in disguise.

### 14.2 Prerequisites

- R8 accepted (measurement itself depends on the metrics catalogue;
  drain and health interact with several workloads).
- `conduit-loadgen` complete per BENCHMARK_PLAN §4 including the
  generator self-test (≥2× target proven before any SUT run).
- Env-A/Env-B/Env-C provisioned per `bench/env/` manifests; sysctl and
  kernel pins applied and recorded.

### 14.3 Owned files, interfaces, and state

R9 owns `cmd/conduit-loadgen` completion, `bench/env` manifests,
`test/load` workload wiring (W1–W10), the report generation tooling,
and the capacity-model coefficient extraction feeding FR-OPS-010.

### 14.4 Algorithms and state behavior

Run protocol per workload (numbered): environment verification
(manifest diff must be empty) → generator self-test → warmup per
BENCHMARK_PLAN §5 → measured window → artifact capture (HDR histograms,
RSS curves, gctrace, run manifest with SHA and config hash) → repeat to
run count → report generation → anomaly review (any dropped run appears
with cause). Acceptance comparisons use the BENCHMARK_PLAN statistical
rules; a target miss is a finding, not a formatting problem: the gate
stops, the miss is diagnosed, and either the implementation improves or
an ADR revises the target with the measurement attached.

Scale scenarios beyond the headline: W9 at 50k with 5% stalls (R6
behavior at scale), W5 expiry storm (ADR-0008 reconnect load), W6
node-loss surge on Env-B (FR-RESUME-009 measurement: reconnect
completion curve, delivery disturbance window), W10 past-target
degradation (load shed correctness at 60k/8k env/s — the honest
failure-mode characterization).

### 14.5 Implementation tickets and sequence

1. **R9.01 — Loadgen completion and self-test.** Full protocol client,
   IP aliasing, stall mode, receipt timestamping, HDR/JSONL output.
   DoD: self-test report ≥2× target on Env-C.
2. **R9.02 — Environment manifests and verification.** Env-A/B/C
   as code with drift detection. DoD: verification tool fails on a
   deliberately drifted sysctl.
3. **R9.03 — W1/W2 headline runs.** Idle hold and reference mixed at
   50k. DoD: NFR-SCALE-001/002 and NFR-PERF-001 rows met or ADR'd;
   reports published.
4. **R9.04 — W3/W4 sweeps.** Publish knee, churn. DoD: NFR-SCALE-005/
   006 evidence; knee documented in capacity model.
5. **R9.05 — W5/W9/W10 adversarial scale.** Expiry storm, stalls at
   scale, past-target. DoD: bounded-memory and shed-correctness
   assertions at scale; reports.
6. **R9.06 — W6 fleet runs.** Env-B node-loss surge and fleet scaling
   factor. DoD: NFR-SCALE-003, NFR-PERF-003, FR-RESUME-009 evidence;
   reports.
7. **R9.07 — Gateway overhead runs.** The NFR-PERF-004 query-overhead
   measurement against instrumented sources. DoD: report.
8. **R9.08 — Capacity model extraction.** Coefficients from the runs
   into the FR-OPS-010 model with worked examples. DoD: model doc
   cross-checked against two held-out run configurations (predicted vs
   measured within 20%).
9. **R9.09 — Claims and gate assembly.** Claims register gains the
   measured numbers with their ladder levels and caveat templates;
   `gate-r9` = report presence + regression re-run hooks for release
   candidates. DoD: job required; register lint green.

### 14.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| generator self-test | SUT numbers taken with an unproven generator. | Self-test report shows generator sustains ≥2× every target dimension on Env-C. |
| environment drift | A run proceeds on a drifted environment. | Drift detector empty-diff gate precedes every recorded run; manifests archived per run. |
| W1 idle hold | 50k idle misses memory target or stability. | ≥30 min at 50k: RSS/conn ≤ 64 KiB, zero unexpected closes, keepalive integrity. |
| W2 reference load | Latency or memory targets miss under mixed load. | p50/p95/p99 ≤ 10/50/150 ms publish→enqueue; RSS/conn p95 ≤ 100 KiB; 5 runs, median-of-runs, CIs published. |
| W6 node loss | Reconnect surge or delivery disturbance unbounded. | Reconnect completion curve published; surviving-node delivery p99 recovers within the documented window; FR-RESUME-009 numbers recorded. |
| W9 stalls at scale | Memory or neighbor latency degrades beyond R6 bands at 50k. | R6 assertions hold at 50k: bounded RSS, healthy-connection budgets met. |
| W10 past target | Overload collapses instead of shedding. | 60k/8k env/s: sheds with 503/4703 correctly, established connections' targets hold, recovery on load removal. |
| capacity model | Coefficients don't predict held-out runs. | Two held-out configs predicted within 20% on memory and delivery rate. |
| claims ladder | A published number exceeds its ladder level. | Register audit: every number carries level, env, workload, and caveat template verbatim. |

### 14.7 Failure and security cases

- A run that meets targets only with GC settings absent from the
  published configuration is invalid (ADR-0001: GOGC/GOMEMLIMIT are
  part of the claim).
- Loadgen and SUT co-residence invalidates a run (BENCHMARK_PLAN
  environment rule; the manifest check enforces separation).
- Numbers from any environment other than Env-A/B are not publishable
  at L2/L3 regardless of how good they look.

### 14.8 Migration, documentation, and installation work

The published reports land under `reports/` with the template; README
and MARKETING_PLAN gain the measured numbers with mandatory caveats;
the capacity model becomes operator documentation. No code migrations.

### 14.9 Acceptance evidence

- All W1–W10 reports published with full statistical treatment.
- Every NFR-PERF and NFR-SCALE row either met (report path cited in
  §19) or revised by an accepted ADR with the measurement attached.
- Capacity model published with held-out validation.
- Claims register carries the measured claims at correct ladder
  levels; lint green.

### 14.10 Explicit deferrals

R9 defers: WAN/browser/client-receipt-centric claims (explicitly
unsupported, BENCHMARK_PLAN §11); continuous benchmarking automation
beyond release-candidate re-runs (OPEN_QUESTIONS); vertical-scaling
exploration beyond the reference machine class.

### 14.11 Requirements traced

R9 terminally owns `NFR-SCALE-001` through `NFR-SCALE-006`,
`NFR-PERF-001`, `NFR-PERF-003`, `NFR-PERF-004`, `NFR-PERF-006`, and
`FR-RESUME-009`. It advances `FR-OPS-010` (model content; the operator
doc packaging closes in R10).

## 15. R10 — Packaging, Deployment, Upgrade, Rollback, and the 1.0 Gate

**Status:** planned.

**Effort range:** 5–7 focused weeks.

### 15.1 Why this gate exists

R10 turns a proven system into a shippable product: reproducible
artifacts with provenance, the Kubernetes deployment story with the
load-balancer realities of long-lived WebSockets, rolling upgrade with
the mixed-version window, rollback, uninstall, the flagship example
application running end to end, and the launch marketing assets — all
under the claims ladder. 1.0 is this gate's acceptance, and nothing
about 1.0 is a ceremony: every checklist row is automated or has an
archived rehearsal transcript.

### 15.2 Prerequisites

- R9 accepted; all gates R0–R9 green on the release-candidate SHA.
- OPERATIONS_TEST_PLAN §13–§15 packaging/install/upgrade content
  frozen.
- MARKETING_PLAN launch-asset list frozen.

### 15.3 Owned files, interfaces, and state

R10 owns `release.yml` completion (build matrix, SBOM, signing,
provenance), `deploy/kubernetes` and `deploy/docker` completion,
`examples/orderboard` (the flagship app: SDL, sources, auth rules,
clients), cross-version fixture automation, `conduit doctor`
completion, the uninstall/purge documentation and tests, and the
launch assets under `marketing/` (site copy, launch post, demo
script) governed by MARKETING_PLAN.

### 15.4 Algorithms and state behavior

Release pipeline (numbered): tag → reproducible build matrix
(CGO_ENABLED=0, -trimpath, pinned toolchain; independent rebuild
byte-comparison job) → SBOM (syft) → sign (cosign keyless) →
provenance attestation → image build (distroless, nonroot) → smoke
(clean-container run of `conduit validate` + fixture serve) →
cross-version suite (N−1 fixtures against N) → publish artifacts →
claims-ladder audit of release notes.

Rolling upgrade contract (FR-OPS-005/006): preStop triggers drain;
terminationGracePeriodSeconds ≥ drain window + grace;
maxUnavailable sized against the capacity model; the mixed-version
window rules enumerated per contract (envelope, control, token,
admin API) with the N/N+1 decode-table fixtures release-blocking.
Rollback is the same rollout in reverse with the same contracts —
plus the rule that a rollback crossing a contract version boundary
is forbidden by the compatibility checker (tooling reads both
versions' contract manifests and refuses).

The flagship application (PRD §1.2) is a scripted scenario suite,
not a demo: each of the six steps is an automated test against the
Env-B fleet with archived transcripts.

### 15.5 Implementation tickets and sequence

1. **R10.01 — Reproducible builds.** Matrix, byte-compare rebuild
   job. DoD: two independent CI builders produce identical
   artifacts.
2. **R10.02 — SBOM, signing, provenance.** syft/cosign/attestation
   jobs, verification docs. DoD: `cosign verify` documented and
   tested in the smoke job.
3. **R10.03 — Images and smoke.** Distroless nonroot images,
   clean-container smoke. DoD: smoke green on both architectures.
4. **R10.04 — Kubernetes completion.** Manifests, PDB, probes,
   preStop, LB requirement docs, kind-based install test. DoD: K8S
   family rows green; a standard rollout on the kind fleet loses no
   more than the drain-window contract states (measured in-test).
5. **R10.05 — Cross-version automation.** N/N+1 fixture generation
   at tag time, decode tables, the rollback boundary checker. DoD:
   deliberately broken fixture fails the release job.
6. **R10.06 — `conduit doctor` completion.** Full check set
   (fd limits, clock, bus, TLS material, kernel sysctls vs bench
   guidance). DoD: doctor matrix on broken-environment fixtures.
7. **R10.07 — Uninstall and purge.** Docs + test proving the
   no-durable-state claim (post-uninstall filesystem diff empty
   except logs; bus subject decommission steps verified). DoD:
   PKG rows green.
8. **R10.08 — Flagship application.** `examples/orderboard` with
   per-user visibility rules, the six scripted scenarios on Env-B.
   DoD: all six transcripts archived; the example installs from the
   published artifacts only (no repo checkout on the target).
9. **R10.09 — Schema evolution policy tooling.** `@deprecated` usage
   counters in admin output, the policy doc, a breaking-change lint
   for SDL diffs. DoD: policy fixtures; lint wired.
10. **R10.10 — Launch assets.** Site copy, launch post, demo script,
    architecture explainer — every claim marker resolving to an
    accepted gate; the demo script runs the flagship scenarios, not
    staged footage. DoD: claims-ladder audit green over `marketing/`;
    MARKETING_PLAN asset checklist complete.
11. **R10.11 — 1.0 gate assembly.** The §20 release-candidate
    checklist automated where possible; `gate-r10` job. DoD: checklist
    green on the RC SHA; version 1.0.0 tagged only after.

### 15.6 Test-driven evidence matrix

| Test | First failing condition | Required passing assertion |
| --- | --- | --- |
| reproducibility | Two builders disagree on any artifact byte. | Independent rebuild byte-identical across the matrix. |
| provenance chain | Any artifact lacks SBOM, signature, or attestation. | `cosign verify` + attestation checks pass in the smoke job for every artifact. |
| clean-container smoke | The binary needs anything outside the image. | validate + fixture serve green in a scratch-network container on amd64 and arm64. |
| rollout loss bound | A standard rollout exceeds the drain-window loss contract. | kind-fleet rollout under scripted load: closes are 4700+hints, loss ≤ contract, clients resume per R7. |
| mixed-version window | Any v-N artifact misreads a v-N+1 contract or vice versa. | Cross-version decode tables green both directions for envelope/control/token/admin. |
| rollback boundary | A contract-crossing rollback proceeds. | Boundary checker refuses with both versions named; in-window rollback rehearsal green. |
| uninstall purge | Durable state survives uninstall. | Filesystem diff post-uninstall empty except logs; documented bus decommission verified. |
| flagship scenarios | Any of the six PRD §1.2 steps fails or is manual. | All six scripted scenarios green on Env-B from published artifacts; transcripts archived. |
| deprecation tooling | A breaking SDL change ships without the policy path. | SDL-diff lint blocks; usage counters visible in admin fixtures. |
| claims audit | Any launch asset claims beyond accepted gates. | Ladder audit over README/marketing/release notes green; every number carries its caveat template. |

### 15.7 Failure and security cases

- Supply-chain: release jobs run with minimal permissions, pinned
  actions, and no fork access; the provenance chain is the
  THREAT_MODEL §7.12 control set.
- A rollback that would cross a contract boundary is refused by
  tooling, not by memory.
- The example app must not embed real credentials; its auth fixtures
  are generated per install.
- Publishing the repository (private → public at this gate) triggers
  the pre-publication audit: history scan for secrets, license
  headers, claims audit.

### 15.8 Migration, documentation, and installation work

This gate is migration/documentation/installation work. The
documentation set's status lines flip to `accepted` per gate with
links to evidence; the root README becomes the honest 1.0 snapshot;
versioned docs publishing (site) per MARKETING_PLAN.

### 15.9 Acceptance evidence

- `gate-r10` green on the RC SHA; §20 checklist fully green.
- All §15.6 rows green; PKG/K8S families green.
- Flagship transcripts archived; launch assets audited.
- 1.0.0 tagged, signed, published; support statement live.

### 15.10 Explicit deferrals

R10 defers everything in OPEN_QUESTIONS (durable delivery, CEL rules,
additional buses, Windows, `@defer`/`@stream`, SSE transport,
federation) — each with its fail-closed default and reopen trigger
recorded there, and none implied by any 1.0 claim.

### 15.11 Requirements traced

R10 terminally owns `FR-OPS-001`, `FR-OPS-002`, `FR-OPS-004` through
`FR-OPS-007`, `FR-OPS-010` through `FR-OPS-013`, `NFR-COMPAT-004`,
`NFR-COMPAT-005`, `NFR-COMPAT-006`, and `NFR-MAINT-002` (final
coverage/mutation floors verified on the release SHA).

## 16. Consolidated Ticket Backlog and Dependency Graph

### 16.1 Ticket inventory

118 tickets across eleven gates: R0.01–R0.11, R1.01–R1.13, R2.01–R2.15,
R3.01–R3.14, R4.01–R4.10, R5.01–R5.11, R6.01–R6.10, R7.01–R7.09,
R8.01–R8.11, R9.01–R9.09, R10.01–R10.11. Every ticket is a single
reviewable unit of work with a concrete definition of done stated in its
gate section; a ticket without its failing test first is unmergeable
(§4.1).

### 16.2 Cross-gate dependency edges

Beyond the linear gate order and the single sanctioned R4∥R5 overlap
(§1.2), the load-bearing edges a scheduler must respect:

- R2.03 (timing wheel) ← R3.09 (expiry), R2.11 (keepalive), R6 timers,
  R8.05 (drain pacing).
- R2.08 (envelope codec) ← R4 matching, R5 everything, R7 buffers.
- R1.11 (publish-mapping seam) ← R2.10, R5.01; the seam contract may
  not change after R2.10 without touching both gates' suites.
- R3.07 (publish-time enforcement) ← R4.05 (candidate handoff),
  R5 consume path, R7.06 (replay authorization): all three call the
  same enforcement point; none may introduce a second path.
- R6.08 (zero-alloc closure) ← R7.04 (splice must not regress it; the
  allocation check re-runs in gate-r7).
- R7.09 (contract freeze) ← R10.05 (cross-version automation builds on
  the frozen manifests).
- R8.05 (drain) ← R9 W6 (surge measurement), R10.04 (rollout loss
  bound).

### 16.3 The critical path

R0 → R1 (intake/executor/sources) → R2 (protocol+pipeline) → R3
(enforcement points) → {R4, R5} → R6 → R7 → R8 → R9 → R10. The longest
chain runs through R2 and R3; nothing on the query-side (R1 breadth)
sits on the critical path after R2 starts, which is the plan's
structural expression of §3's product hierarchy.

## 17. Effort Model, Decision Points, and Risk Register

### 17.1 Effort totals

Summing gate ranges: 46–65 focused weeks single-engineer-equivalent.
The R4∥R5 overlap can compress the calendar by up to 5 weeks with two
engineers. No estimate below assumes heroics; every gate's range
includes its documentation and evidence assembly, which historically
dominate underestimates.

### 17.2 Scheduled decision points

| When | Decision | Input |
| --- | --- | --- |
| R4 mid-gate | Interval arrays vs interval tree for endpoint churn | R4.09 churn benchmark rows |
| R5 acceptance | Whether envelope broadcast bus load requires interest-routing work to be scheduled | measured bus bandwidth at fixture scale |
| R7 acceptance | Replay-buffer defaults revision | W8 horizon curves |
| R9 acceptance | Target revision ADRs, if any misses | W1–W10 reports |
| R10 entry | Repository publication timing | pre-publication audit |

### 17.3 Risk register

| Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- |
| GC pauses blow the p99 delivery budget at 50k connections | medium | high | zero-alloc delivery path (R6), GOMEMLIMIT discipline, pause visibility in every report (ADR-0001); fallback: revise p99 by ADR with measurements |
| Memory-per-connection misses 64 KiB | medium | high | budget table maintained from R2 with per-gate CI probes at 10k; the 251 KiB ceiling (ADR-0001) gives 4× headroom before the product thesis is threatened |
| Envelope broadcast saturates the bus before node count does | medium | medium | measured at R5 and R9; interest-routing optimization is a designed-for extension (ADR-0005 records the reopening condition) |
| Reference-client behavior changes under the pin-advance rule | low | medium | pinned version range, advance only behind full suite |
| Counting-index churn cost exceeds budget under real workloads | low | high | R4.09 includes churn benchmarks; COW epoch design isolates the publish path; interval-tree fallback pre-designed |
| NATS behavior diverges from the verified assumptions | low | high | R5 broker suite verifies ordering/at-most-once/overrun assumptions explicitly; bus port keeps replacement possible |
| Splice-correctness bug escapes to production | low | critical | 10k seeded interleavings + staging design reviewed against the proof obligation; divergence class = release-blocking incident |
| Fleet fixture flakiness erodes trust in R5/R8 evidence | medium | medium | container-class flake policy with mandatory triage; deterministic memory-bus twins for every fleet scenario |
| Scope creep from query-side feature pressure | high | medium | §1.3 rule 2 and PRD §1.3 hierarchy; exceptions only through OPEN_QUESTIONS process |
| Single-maintainer bus factor | high | medium | this documentation set is the mitigation: any competent engineer can execute a gate from its section alone |

## 18. Marketing Deliverables and Claims Discipline

MARKETING_PLAN.md is the normative asset list and claims ladder; this
section binds it into the build order.

### 18.1 Standing per-gate obligation

Every gate's §X.9 includes updating the claims register. The register
is a table in MARKETING_PLAN: claim text, ladder level, gate, evidence
link, status (`unearned`/`earned`). The claims lint (R0.07) fails CI
when README, docs, or `marketing/` assets carry a claim marker whose
register row is `unearned`.

### 18.2 Marketing ticket inventory

Marketing work is scheduled, not ambient:

- **R0.11** — register bootstrap (all claims `unearned`).
- **R1–R9 §X.8/§X.9** — one register update per gate with the exact
  earned sentence recorded in each gate's §X.8.
- **R10.10** — launch assets: site copy, launch post, demo script
  (runs the flagship scenarios live), architecture explainer,
  comparison page (Conduit vs generic GraphQL servers' subscription
  support — factual, citing the ladder), README final pass.
- **Post-1.0 cadence** — every published benchmark re-run refreshes
  the numbers or retracts them (BENCHMARK_PLAN §10 regression rule);
  retraction is a release-notes item, never a silent edit.

### 18.3 Forbidden-claims list

At no gate may any asset state: "guaranteed delivery", "exactly-once",
"real-time" without the measured-latency qualifier, "infinitely
scalable", "zero downtime" (drain has a loss contract; say what it
is), or any number without its ladder level and environment. The lint
carries this list as patterns.

## 19. Requirement-to-Evidence Traceability Matrix

### 19.1 Traceability rules

Every requirement ID minted in PRODUCT_REQUIREMENTS §7/§9 appears in
exactly one row. The terminal gate is the highest gate cited; lower
cited gates advance the requirement without closing it. A test counts
as evidence only when it drives the public boundary and asserts the
user-visible failure path. Where PRODUCT_REQUIREMENTS §10.3 and this
matrix disagree, this matrix controls. The CI trace-check
(OPERATIONS_TEST_PLAN §19) enforces that every ID below maps to at
least one named test family row.

### 19.2 GraphQL execution

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-GQL-001` | R1.02–R1.03 | `graphql/schema` | Invalid-fixture corpus fails with file/line/rule; valid set builds with stable hash. |
| `FR-GQL-002` | R1.04 | `graphql/executor` | Spec-execution corpus byte-exact. |
| `FR-GQL-003` | R1.04/R1.11 | executor, publish seam | Serial order; mappings emit only on success. |
| `FR-GQL-004` | R1.07 | `datasource/postgres` | Parameterization adversarial set; pool/timeout behavior. |
| `FR-GQL-005` | R1.08 | `datasource/http` | SSRF fixture set fails closed; bounds enforced. |
| `FR-GQL-006` | R1.09 | `datasource/function` | v1 contract fixtures both directions; bounds. |
| `FR-GQL-007` | R1.03/R1.07–R1.09 | `DataSource` port | One-source-per-field validation; adapter isolation archcheck. |
| `FR-GQL-008` | R1.05 | `graphql/complexity` | Depth boundary at limit±1. |
| `FR-GQL-009` | R1.05 | `graphql/complexity` | Cost boundary; computed cost in extensions. |
| `FR-GQL-010` | R3.11 (adv. R1.12) | schema/auth | Disabled mode removes at validation; admin-only mode gated by principal. |
| `FR-GQL-011` | R1.01 | `graphql/ast` intake | Over-bound inputs fail typed with zero AST allocation. |
| `FR-GQL-012` | R3.13 (adv. R1.06) | error formatting + redaction | Canary corpus absent from responses and logs across auth-bearing paths. |
| `FR-GQL-013` | R1.04 | executor coercion | Variable validation matrix with locations. |
| `FR-GQL-014` | R1.10 | transport/executor deadlines | Timeout cancels sources; typed error. |
| `FR-GQL-015` | R2.10 | protocol + executor | Same document identical over HTTP and WS (Next+Complete wrap). |

### 19.3 Subscription transport

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-SUB-001` | R2.04 | transport upgrade | Negotiation matrix incl. 400 and 4406 rejections. |
| `FR-SUB-002` | R2.12 | conformance suite | Unmodified pinned client passes full CONF set. |
| `FR-SUB-003` | R2.02/R2.11 | state table | 4408/4429/4401 timelines under fake clock. |
| `FR-SUB-004` | R3.06 (adv. R2.02) | protocol + auth handoff | Ack only after auth; uniform 4403. |
| `FR-SUB-005` | R2.06 | registry | 4409 on duplicate ID; 255-byte bound. |
| `FR-SUB-006` | R2.10 | protocol delivery | Next/Error/Complete semantics fixtures incl. prompt Complete teardown. |
| `FR-SUB-007` | R2.11 | keepalive | Ping/pong both directions; unsolicited pong ignored. |
| `FR-SUB-008` | R2.01/R2.13 | codecs | Malformed-frame matrix closes 4400 without echo. |
| `FR-SUB-009` | R2.05/R2.13 | transport bounds | 512 KiB boundary ±1; library cap backstop. |
| `FR-SUB-010` | R2.15 | close-code table | Every close path asserts a documented code (table-exhaustion test). |
| `FR-SUB-011` | R2.02 | state table | Exhaustive state×event assertion; no default branch. |
| `FR-SUB-012` | R2.13/R2.14 | hostile + fuzz | Full HOST battery survival; 72 fuzz-hours zero crashers. |

### 19.4 Authorization

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-AUTH-001` | R3.03 | `auth/oidc` | JWT adversarial corpus; JWKS rotation/outage behavior. |
| `FR-AUTH-002` | R3.04 | `auth/apikey` | Hash store, constant-time compare, rotation fixtures. |
| `FR-AUTH-003` | R3.05 | `auth/custom` | Contract fixtures; timeout/malformed fail closed. |
| `FR-AUTH-004` | R3.06 | config + modes | Per-tenant mode config; `none` acknowledgment warning. |
| `FR-AUTH-005` | R3.01 | principal | Immutability and bounds tests; no raw credentials. |
| `FR-AUTH-006` | R3.06 | AuthorizeSubscribe | Deny never registers; allow/deny matrix. |
| `FR-AUTH-007` | R3.11 | executor field auth | Field matrix with null propagation. |
| `FR-AUTH-008` | R3.02 | `auth/rules` | Undefined-rule startup failure; rule corpus. |
| `FR-AUTH-009` | R3.02/R3.14 | rules + audit | Structured evaluation; trace records under budget. |
| `FR-AUTH-010` | R3.07 | AuthorizePublish | No-skip-path archcheck; allow/deny/suppress fixtures. |
| `FR-AUTH-011` | R3.07 | decision cache | Stale-cache probe: zero stale-hit deliveries. |
| `FR-AUTH-012` | R3.09 | expiry handling | Full timeline: warning, cut, errors, 4403. |
| `FR-AUTH-013` | R3.08 | revocation set | Class timelines; sweep behavior. |
| `FR-AUTH-014` | R5.08 (adv. R3.08) | control path fleet | 1,000-revocation report p99 ≤ 2 s. |
| `FR-AUTH-015` | R5.03 (adv. R3.10) | degraded mode | Both policies under real-transport partition. |
| `FR-AUTH-016` | R3.10 | config | Explicit policy config; audited change only. |
| `FR-AUTH-017` | R3.12 | tenancy | Cross-tenant probes: zero candidates. |
| `FR-AUTH-018` | R3.06/R3.11 | error uniformity | Response-shape differ probe byte-identical. |

### 19.5 Filter matching

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-FILT-001` | R4.01 | predicate compiler | Compiler corpus; typed subscribe-time errors. |
| `FR-FILT-002` | R4.01/R4.05 | IR + matcher | Full grammar fixtures incl. absent/null rule. |
| `FR-FILT-003` | R4.01 (adv. R1.02) | schema validation | `@filterable` type-restriction fixtures. |
| `FR-FILT-004` | R4.01 | compiler | Type-mismatch subscribe errors, never silent never-match. |
| `FR-FILT-005` | R4.01 | normalization | 8-conjunction bound rejection. |
| `FR-FILT-006` | R4.08 | residual list | Ceiling rejection; metric. |
| `FR-FILT-007` | R4.07 | oracle | ≥1M nightly differential cases, zero divergence. |
| `FR-FILT-008` | R4.06 | index concurrency | Race-detector churn suite clean. |
| `FR-FILT-009` | R4.08 | index metrics | Metrics-contract rows present. |
| `FR-FILT-010` | R4.09 | W7 benchmark | Published report: crossover ≤10k, ≥10× at 100k. |

### 19.6 Cross-node fanout

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-FAN-001` | R5.01/R5.07 (adv. R1.11, R2.10) | seal + bus | Fleet fixture: publish on A delivers on B/C. |
| `FR-FAN-002` | R5.02 (adv. R2.08) | envelope codec | Version fixtures; counted rejection. |
| `FR-FAN-003` | R5.07 | consume path | Local matching without coordination (no cross-node calls in trace). |
| `FR-FAN-004` | R5.06 | ordering | 1M-envelope monotone sequence check at subscribers. |
| `FR-FAN-005` | R5.05/R5.07 | node-loss contract | Kill-mid-burst transcript; survivors unaffected. |
| `FR-FAN-006` | R5.03/R5.05 | partition behavior | Isolated-node local delivery + degraded flag + heal reconcile. |
| `FR-FAN-007` | R5.06 | backlog | Induced overrun: counted drops, escalation, bounded RSS. |
| `FR-FAN-008` | R5.02 | dedupe window | Duplication fixtures; documented horizon recurrence. |
| `FR-FAN-009` | R5.01 (adv. R3.12) | subjects | Tenant-scoped subject map; no foreign-tenant subscriptions. |
| `FR-FAN-010` | R5.10 | admin publish | Injected envelopes indistinguishable downstream. |
| `FR-FAN-011` | R5.09 | rate limiting | Boundary/burst fixtures; typed rejection. |
| `FR-FAN-012` | R5.11 (adv. R8.03) | fanout metrics | Per-stage series in the catalogue contract test. |

### 19.7 Connection lifecycle and backpressure

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-CONN-001` | R2.06 | registry | Atomic register/deregister churn race suite. |
| `FR-CONN-002` | R6 (adv. R2.11) | idle timer | 4702 timeline; quota-class boundary rows. |
| `FR-CONN-003` | R6 (adv. R2.11) | lifetime timer | 4701 with jitter distribution assertions. |
| `FR-CONN-004` | R6.04 | connection quotas | 4703 at init boundary matrix. |
| `FR-CONN-005` | R6.04 | subscription quotas | Typed-error (not close) boundary matrix. |
| `FR-CONN-006` | R6.05 | rate limiter | Token-bucket timeline; neighbor isolation. |
| `FR-CONN-007` | R6.01 (adv. R2.05) | outbound queue | Bounds; control-lane bypass under saturation. |
| `FR-CONN-008` | R6.01/R6.02 | policy engine | Full policy matrix incl. every §11.4 edge case. |
| `FR-CONN-009` | R6.03 | drop accounting | Counters + `conduit.dropped` notices; zero-alloc notice path. |
| `FR-CONN-010` | R8.05 (adv. R6) | drain | Paced timelines; drain-under-load fleet transcript. |
| `FR-CONN-011` | R6.03/R8.03 | observability | Per-policy per-field metrics within cardinality budget. |
| `FR-CONN-012` | R6.07 | slow-consumer detection | Threshold events precede policy engagement. |
| `FR-CONN-013` | R6 (adv. R2.04) | TLS/trusted-proxy | Reload fixtures; proxy allowlist; plaintext refusal. |
| `FR-CONN-014` | R6.06 | fd budget | Lowered-rlimit shed + hysteretic recovery. |

### 19.8 Reconnect and resume

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-RESUME-001` | R7.03 (adv. R2) | position stamping | Extensions present; reference-client invisibility re-run. |
| `FR-RESUME-002` | R7.02 | token codec | Forgery corpus; round-trip property; 512-byte bound. |
| `FR-RESUME-003` | R7.01 | replay buffers | Bounds/eviction/horizon metrics; field-churn freeing. |
| `FR-RESUME-004` | R7.04/R7.06 | splice + authz | Replay through AuthorizePublish; splice suite. |
| `FR-RESUME-005` | R7.05 | gap honesty | Exact notices before live delivery in every gap class. |
| `FR-RESUME-006` | R7.04 | splice ordering | 10k interleavings: monotone, no dup, no gap. |
| `FR-RESUME-007` | R7.02/R7.05 | rejection path | Typed rejection + fresh-subscribe + `resume_rejected` notice. |
| `FR-RESUME-008` | R7.08 | W8 measurement | Published horizon-vs-rate report cited by API docs. |
| `FR-RESUME-009` | R9.06 (adv. R7.07) | storm mitigation | Hint machinery tests + W6 surge measurement. |

### 19.9 Admin and observability

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-ADMIN-001` | R8.01 | admin listener | Port isolation fixture + archcheck rule. |
| `FR-ADMIN-002` | R8.02 | inspection | Fixture comparisons; no payload bytes. |
| `FR-ADMIN-003` | R8.01/R8.05 (adv. R5.10) | drain/revoke/publish | Endpoint fixtures incl. dry-run drain. |
| `FR-ADMIN-004` | R8.03 | metrics | Catalogue contract test required in CI. |
| `FR-ADMIN-005` | R8.07 | health | Truth-table test over degraded-input combinations. |
| `FR-ADMIN-006` | R8.02 | config inspection | Redacted effective config + hash fixture. |
| `FR-ADMIN-007` | R8.08 | diagnostics | Inventory-first bundle; canary sweep. |
| `FR-ADMIN-008` | R8.01 | audit | Structured audit records for every mutation. |

### 19.10 Operations

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `FR-OPS-001` | R10.01/R10.03 | packaging | Reproducible matrix; distroless nonroot smoke. |
| `FR-OPS-002` | R10.06 (adv. R0.10, R1) | config validation | Full validation phases; `conduit validate` parity. |
| `FR-OPS-003` | R8.06 | reload | Atomic swap; failed-validation no-op; typed teardown. |
| `FR-OPS-004` | R10.04 | kubernetes | kind install test; LB requirement docs verified. |
| `FR-OPS-005` | R10.05 | mixed-version | N/N+1 decode tables both directions. |
| `FR-OPS-006` | R10.04 (adv. R8.05) | rollout | Rollout loss ≤ drain contract, measured. |
| `FR-OPS-007` | R10.09 | schema evolution | Deprecation counters; breaking-change lint. |
| `FR-OPS-008` | R8.10 | runbook | Every alert rehearsed; link-check green. |
| `FR-OPS-009` | R8.03/R8.04 | conventions | Catalogue + cardinality budget contract test. |
| `FR-OPS-010` | R10 (adv. R9.08) | capacity model | Held-out prediction within 20%; operator doc. |
| `FR-OPS-011` | R10.07 | uninstall/purge | Post-uninstall diff empty except logs. |
| `FR-OPS-012` | R10.01/R10.02 | provenance | SBOM/signature/attestation verified in smoke. |
| `FR-OPS-013` | R10.06 | doctor | Broken-environment fixture matrix. |

### 19.11 Non-functional requirements

| Requirement | Terminal gate and tickets | Owning boundary | Required evidence |
| --- | --- | --- | --- |
| `NFR-PERF-001` | R9.03 | W2 report | p50/p95/p99 ≤ 10/50/150 ms, 5-run treatment. |
| `NFR-PERF-002` | R4.09 | W7 report | p99 ≤ 1 ms at 100k entries, benchstat. |
| `NFR-PERF-003` | R9.06 | W6/Env-B report | Bus added latency p95 ≤ 5 ms same-AZ. |
| `NFR-PERF-004` | R9.07 | overhead report | Gateway overhead p95 ≤ 5 ms. |
| `NFR-PERF-005` | R6.08 (adv. R4.05) | allocation test | 0 allocs/op end-to-end delivery path, required check. |
| `NFR-PERF-006` | R9 (all reports) | GC evidence | gctrace + GOGC/GOMEMLIMIT attached to every published number. |
| `NFR-SCALE-001` | R9.03 | W1/W2 | 50k sustained ≥30 min full protocol. |
| `NFR-SCALE-002` | R9.03 | memory method | ≤64 KiB idle / ≤100 KiB p95 loaded, RSS-based. |
| `NFR-SCALE-003` | R9.06 | Env-B | ≥2.5× fleet factor with overhead published. |
| `NFR-SCALE-004` | R9 (adv. R4) | W2/W7 | 100k entries with NFR-PERF-002 holding on loaded node. |
| `NFR-SCALE-005` | R9.04 | W3 | 5k env/s sustained within latency targets. |
| `NFR-SCALE-006` | R9.04 | W4 | 500 accepts/s with established-connection budgets held. |
| `NFR-SEC-001` | R2 (adv. R1.01) | bounded parsers | Intake/frame/message bound matrices + fuzzing. |
| `NFR-SEC-002` | R3.14 | enforcement points | Adversarial matrix + mutation testing ≥85%. |
| `NFR-SEC-003` | R5.08 | revocation SLO | p99 ≤ 2 s fleet report; fail-closed default. |
| `NFR-SEC-004` | R8.08 (adv. R3.13) | redaction | Canary sweep across every sink incl. diagnostics. |
| `NFR-SEC-005` | R8 (adv. R2.04, R5.01) | TLS legs | Per-leg TLS/acknowledgment matrix incl. bus and admin. |
| `NFR-SEC-006` | R3.12 | tenancy | Structural isolation probes. |
| `NFR-SEC-007` | R7.02 | token security | Forgery corpus; constant-time verify; rotation. |
| `NFR-SEC-008` | R6.09 (adv. R2.13) | DoS battery | Full battery survival with bounded RSS. |
| `NFR-SEC-009` | R6.09 | log limits | Amplification fixtures bounded. |
| `NFR-SEC-010` | R0 (standing) | dependency review | Review records; govulncheck triage SLAs. |
| `NFR-COMPAT-001` | R2.12 (re-run R7) | reference client | Full conformance incl. extension invisibility. |
| `NFR-COMPAT-002` | R2 (adv. R1.04) | spec conformance | Execution corpus + WS single-result parity. |
| `NFR-COMPAT-003` | R7.09 | contract freeze | Versioned manifests; cross-version fixtures. |
| `NFR-COMPAT-004` | R10 | platform tiers | Tier-1 release matrix; no untested claims. |
| `NFR-COMPAT-005` | R10.05 | mixed-version window | N/N+1 fixtures release-blocking. |
| `NFR-COMPAT-006` | R10.09 | deprecation policy | Policy fixtures; dual-support rule. |
| `NFR-MAINT-001` | R0.06 | archcheck | Boundary rules enforced in CI from R0. |
| `NFR-MAINT-002` | R10 (adv. R3.14, R4) | coverage/mutation | 80% floor + ≥85% mutation on named packages at release SHA. |
| `NFR-MAINT-003` | R1 (standing) | error taxonomy | Category/metric/meaning triple for every category. |
| `NFR-MAINT-004` | R0.07 | docs lint | Status discipline lint required. |
| `NFR-MAINT-005` | R0 (standing) | dependency budget | One-screen enumeration; review per addition. |
| `NFR-MAINT-006` | R0 (standing) | determinism | Clock injection archcheck; seed logging. |

## 20. Release-Candidate Readiness Checklist

Executed on the release-candidate SHA; every row automated or carrying
an archived rehearsal transcript:

1. Gates R0–R10 green on the RC SHA (all `gate-r*` required jobs).
2. Full nightly suite green on the RC SHA (fuzzing, soak, chaos,
   broker matrix) within the preceding 7 days.
3. Cross-version fixtures green against the previous tagged release.
4. All W1–W10 reports present for the RC SHA or the release notes
   carry the re-measurement/republish statement per BENCHMARK_PLAN §10.
5. Coverage ≥ 80%, mutation ≥ 85% on `auth/**` and `filter/**`.
6. govulncheck clean or triaged within SLA; dependency review records
   current.
7. Claims-ladder audit green over README, docs, `marketing/`, release
   notes.
8. Runbook link-check green; every shipped alert has a rehearsal
   transcript dated within the release cycle.
9. Pre-publication audit (history secret scan, licenses) green.
10. Uninstall/purge test green; support statement and versioning
    policy published.

## 21. Feature Exhaustiveness Audit

Every product-surface element from PRODUCT_REQUIREMENTS §5–§7 is owned
by exactly one terminal gate. The audit walks the surface; the CI
trace-check keeps it honest.

| Surface element | Owning gate | Verified by |
| --- | --- | --- |
| HTTP `POST /graphql` (queries/mutations) | R1 | UNIT + endpoint end-to-end |
| WebSocket upgrade + subprotocol | R2 | CONF negotiation rows |
| Full `graphql-transport-ws` message set | R2 | state-table + CONF |
| Operations over WS (single-result) | R2 | parity fixtures |
| SDL + directives (`@source`) | R1 | schema corpus |
| `@complexity`, depth limits | R1 | boundary rows |
| `@auth` + rule engine | R3 | AUTHZ matrix |
| `@filterable` + predicate grammar | R4 | differential property suite |
| `@backpressure` policies | R6 | BP matrix |
| Postgres / HTTP / function sources | R1 | adapter suites |
| Introspection policy | R3 | mode fixtures |
| OIDC / API key / custom auth modes | R3 | mode matrices |
| Subscribe-time + publish-time enforcement | R3 | adversarial matrix |
| Expiry + revocation mid-subscription | R3 (fleet SLO R5) | timelines + SLO report |
| Tenancy isolation | R3 | cross-tenant probes |
| Publish envelope + bus + NATS adapter | R5 | FAN matrix + broker suite |
| Node loss / partition / backlog behavior | R5 | fault matrix |
| Admin publish | R5 | injection fixtures |
| Quotas / rate limits / fd budget | R6 | CONN matrix |
| Outbound queues + slow-consumer policy | R6 | stalled-consumer suite |
| Resume tokens / replay / gap honesty | R7 | RESUME matrix + W8 |
| Admin API (`/admin/v1/*`) | R8 | ADMIN fixtures |
| Metrics catalogue / tracing / health | R8 | contract tests |
| Drain | R8 | drain timelines + fleet transcript |
| Schema/TLS/config reload | R8 | reload fixtures |
| Scale + latency targets | R9 | W1–W10 reports |
| Packaging / provenance / K8s / upgrade / rollback | R10 | PKG + K8S matrices |
| Uninstall/purge, doctor, capacity model | R10 | PKG rows + held-out validation |
| Flagship application | R10 | six scripted transcripts |
| Marketing claims and launch assets | R10 (register from R0) | claims lint + audit |

No product-surface element is unowned; no element is owned twice. The
`conduit validate`/`doctor` CLI surface is owned by R10 with R0/R1
advancement; the load harness is owned by R9; the linear-scan oracle is
permanent test infrastructure owned by R4.

## 22. Definitions of Done and Compatibility Truth

### 22.1 Definition of done, per artifact class

- **A parser or codec** is done when: its bounds are configured and
  tested at limit and limit±1; its malformed-input corpus is checked in;
  its fuzz target exists with a seeded corpus; its typed errors map to
  §4.2 categories; and no caller can reach it with unbounded input
  (archcheck or construction proves it).
- **A state machine** is done when: its state and event sets are closed
  types; every state×event cell is asserted, including impossible pairs;
  no default branch exists; and replay of a recorded event sequence
  reproduces the state deterministically.
- **An enforcement point** is done when: it is named in
  AUTHORIZATION_MODEL; every data path to its protected effect crosses
  it (archcheck or structural argument recorded); its adversarial tests
  exist and fail when the point is stubbed out (proven once by
  deliberate stub — the gate-can-fail rule); and its decisions are
  observable.
- **A benchmark** is done when: its workload is parameterized in
  BENCHMARK_PLAN; its environment manifest is archived; its statistical
  treatment matches §5 of that plan; its report is published under
  `reports/`; and its number appears nowhere above its ladder level.
- **A document** is done when: its status line is accurate; its claims
  carry gate references; the docs lint passes; and its normative
  statements do not conflict with a higher-precedence document
  (docs/README conflict rules).
- **A ticket** is done when its gate section's DoD line is satisfied
  and the §4.1 merge order was followed — reviewers verify the failing
  test predates the implementation in the commit sequence.

### 22.2 Compatibility truth table

The versioned public contracts and where their compatibility is proven:

| Contract | Versioned from | Frozen at | Cross-version proof |
| --- | --- | --- | --- |
| `graphql-transport-ws` behavior | R2 (spec-fixed) | R2 | conformance suite, pinned client range |
| `conduit` protocol extensions | R2 (reserved), active R7 | R7.09 | extension invisibility + R7 fixtures |
| Publish envelope v1 | R2.08 | R7.09 | N/N+1 decode tables (R5 first, R10.05 automated) |
| Control messages v1 | R3.08 | R7.09 | control matrix + decode tables |
| Resume token v1 | R7.02 | R7.09 | forgery corpus + version-bump fixtures |
| Function-source contract v1 | R1.09 | R7.09 | contract fixtures both directions |
| Configuration schema | R0.10 | R10 | validation fixtures + deprecation lint |
| Admin API `/admin/v1` | R5.10 (stub), R8 | R10 | endpoint fixtures + mixed-version rows |
| SDL directives | R1.02 | R10 | schema corpus + evolution policy lint |

Nothing outside this table is a public contract; internal Go interfaces
(§3.3) may change freely behind their tests until an ADR promotes them.

## 23. Immediate Execution Order from a Clean Start

The first ten working sessions, in order, with no decision left open:

1. Run `gh auth status`; confirm account and scopes (R0.01 — verified
   on the authoring machine 2026-08-30).
2. Create the private repository, push this documentation set, apply
   branch protection with read-back verification (R0.02).
3. Land the toolchain pin and module skeleton (R0.03).
4. Land `internal/clock` with its deterministic tests (R0.04) and
   `internal/errors` (R0.05) — the first two failing tests of the
   project precede their implementations.
5. Land `tools/archcheck` with fixture violations (R0.06), then the
   docs-status and claims lints (R0.07).
6. Wire `pr.yml` as a required check and prove it can fail (R0.08),
   then the workflow skeletons (R0.09) and config skeleton (R0.10).
7. Initialize the claims register all-unearned (R0.11); assemble R0
   acceptance evidence; accept R0.
8. Begin R1 with the bounded-intake failing tests (R1.01) and the SDL
   corpus (R1.02) — the corpus files themselves are the first R1
   deliverable, before any parser code.
9. Proceed through R1's ticket order exactly as §6.5 lists it; no
   ticket starts before its predecessor's DoD is met.
10. At R1 acceptance, re-read §7 (R2) prerequisites and freeze the
    PROTOCOL_CONFORMANCE state table before writing protocol code.

Anything that tempts a different order — a quick WebSocket demo, an
early benchmark, a marketing screenshot — is answered by §1.3 rule 2
and §18.3: unearned claims and unowned code have no home in this plan.
