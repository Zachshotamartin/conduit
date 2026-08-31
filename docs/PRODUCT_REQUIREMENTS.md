# Conduit Product Requirements and User Flows

Document status: accepted.
Normative product definition. Last revised: 2026-08-30.

Companion specifications: [Build plan](./BUILD_PLAN.md),
[Architecture](./ARCHITECTURE.md),
[Protocol conformance](./PROTOCOL_CONFORMANCE.md),
[Authorization model](./AUTHORIZATION_MODEL.md),
[Operations and test plan](./OPERATIONS_TEST_PLAN.md),
[Threat model](./THREAT_MODEL.md), [Benchmark plan](./BENCHMARK_PLAN.md),
[Marketing plan](./MARKETING_PLAN.md), [Glossary](./GLOSSARY.md).

This document is the only source of requirement IDs. Every ID below is traced
to a terminal owning gate in BUILD_PLAN §19. At the time of writing, every
requirement is `planned`; nothing in this document is an implementation claim.

## 1. Product Definition

### 1.1 One-sentence pitch

Conduit is a self-hosted GraphQL gateway built around the subscription path:
clients subscribe over WebSocket with per-subscription filters, mutations
publish, and the gateway fans out to every matching connection across a
horizontally scaled fleet — with authorization re-evaluated at publish time
and an explicit, measured backpressure policy.

### 1.2 Flagship demonstration

The demonstration that defines "done" for Conduit 1.0 (owned by gate R10):

1. A three-node Conduit fleet runs behind a standard load balancer with a
   NATS bus, serving a real example application (a live order board with
   per-user visibility rules).
2. Fifty thousand WebSocket connections are held on a single benchmarked node
   with published memory per connection; the fleet demonstration itself runs
   with a realistic mixed load.
3. A mutation submitted to node A appears on matching subscriptions on nodes
   B and C within the published latency percentiles.
4. An administrator revokes a user's grant; that user's live subscriptions
   stop receiving matching events within the published propagation SLO, and
   the adversarial test that proves no post-revocation delivery is named.
5. A node is killed; its clients reconnect through the load balancer with
   resume tokens, replay what the buffer holds, and receive an honest
   `resume_gap` notice for what it does not.
6. A rolling deploy drains a node with paced 4700 closes while the fleet
   keeps delivering.

Every step is a scripted, repeatable scenario in the repository, not a live
demo that works when the network is kind.

### 1.3 Product hierarchy

The subscription path is the product. GraphQL query and mutation execution,
the data-source adapters, and the admin surface exist to make the
subscription path usable and operable; they are held to production quality
but their feature depth is deliberately conservative (no federation, no
caching layer, no query planning research). Any pressure to deepen the
query-side feature set competes with the subscription thesis and loses by
default; OPEN_QUESTIONS records the exceptions process.

## 2. Primary Users and Jobs

### 2.1 Application developer

Builds a product on Conduit's API. Needs: subscriptions that behave exactly
as the protocol document says with the unmodified `graphql-ws` client;
filters expressed as ordinary GraphQL arguments; reconnect behavior that is
honest about gaps so client state logic can be written once and trusted;
error and close codes that distinguish "your token expired" from "you are
being throttled" from "the server is deploying".

### 2.2 Platform operator

Runs Conduit for one or many application teams. Needs: a single binary or
container with validated configuration; a capacity model that converts
expected connections, subscriptions, and publish rates into node counts and
memory; drain-on-deploy that does not page anyone; dashboards and alerts
derived from a documented metric catalogue; a runbook whose incident entries
were actually rehearsed (chaos suite evidence).

### 2.3 Security reviewer

Approves Conduit for deployment. Needs: named enforcement points for every
authorization claim; the adversarial evidence that each is not bypassable;
the revocation propagation SLO and its measurement; the tenancy isolation
argument; the threat model's residual-risk register stated plainly.

### 2.4 Contributor and evaluator

Reads the repository to judge or extend it. Needs: the documentation set to
match the code's actual status; the conformance, property, and fault suites
runnable locally without a cluster; architecture checks that make boundary
violations fail CI rather than review.

## 3. Product Principles

### 3.1 The subscription path is the product

Depth goes to subscribe, match, authorize, deliver, and reconnect. Breadth
anywhere else must justify itself against that.

### 3.2 Protocol exactness over convenience

Conduit implements `graphql-transport-ws` exactly, including its close codes
and its silences (documented decisions for every ambiguity, PROTOCOL_
CONFORMANCE §5). Compatibility with the unmodified reference client is a
hard gate, not an aspiration.

### 3.3 Authorization is enforced where data moves

Grants change while subscriptions are live, so the subscribe-time decision is
never sufficient: every candidate delivery passes a publish-time decision at
a named enforcement point. "Checked at subscribe" is never allowed to stand
in for "checked at publish".

### 3.4 Bounded everything

Every queue, buffer, index, parser input, and cache in Conduit has a
configured bound and a defined overflow behavior. Unbounded growth is a bug
class the test plan hunts explicitly (R6 adversarial-load suite).

### 3.5 Honest failure over implied completeness

Backpressure drops are counted and visible. Resume gaps are announced, never
absorbed. Degraded modes (bus partition, stale revocations) are entered
explicitly, logged, and surfaced in health output. No fallback silently
weakens a contract.

### 3.6 Determinism before infrastructure

Protocol, matching, authorization, and fanout behavior are proven with
injected clocks, in-process transports, and simulated peers. Real brokers,
real clusters, and real load exist to validate the deterministic model, not
to substitute for it.

### 3.7 Claims match evidence

Single-node results are single-node claims. Idle-connection results are
idle-connection claims. Every published number carries its benchmark
configuration (BENCHMARK_PLAN claims ladder), and marketing copy is bound to
the same rule (MARKETING_PLAN).

### 3.8 Self-hosted first

No SaaS dependency for any core function. One binary, one optional broker,
standard observability. Everything needed to operate Conduit ships in the
repository.

## 4. Scope

### 4.1 Required for the first usable release (developer preview, after R7)

- GraphQL queries and mutations over HTTP against the three data-source
  types, with depth and complexity limits.
- Subscriptions over `graphql-transport-ws` passing the conformance suite
  against the unmodified reference client.
- OIDC/JWT, API key, and custom authorizer modes; subscribe-time and
  publish-time enforcement; expiry and revocation mid-subscription.
- Predicate compilation and the counting index with the published
  index-versus-scan benchmark.
- Cross-node fanout over NATS with defined node-loss, partition, and backlog
  behavior.
- Backpressure policies, quotas, and connection lifecycle controls.
- Reconnect and resume with the measured gap window.

### 4.2 Required additionally for Conduit 1.0 (after R10)

- Metrics, tracing, admin surface, drain-on-deploy, runbook (R8).
- The measured 50,000-connection scale target, published with statistical
  treatment (R9).
- Packaging, Kubernetes deployment, rolling upgrade with the mixed-version
  window, rollback, uninstall/purge, diagnostics bundle (R10).
- The flagship demonstration application running end to end (R10).
- Launch marketing assets bound to the claims ladder (R10).

### 4.3 Explicit non-goals for 1.0

- Federation, schema stitching, or remote-schema composition.
- Response caching, automatic persisted queries, or a CDN story.
- The legacy `subscriptions-transport-ws` protocol (ADR-0002).
- At-least-once or durable delivery (ADR-0007); Conduit is not a message
  queue.
- Server-side subscription resolvers with arbitrary user code hosting;
  the function source is a bound endpoint contract, not a code platform.
- Windows-native binaries (ADR-0011).
- A hosted control plane, usage billing, or any SaaS component.
- GraphQL spec drafts beyond October 2021 (e.g. `@defer`/`@stream`);
  recorded in OPEN_QUESTIONS.

## 5. API Surface

### 5.1 Listeners

- **Client listener**: HTTPS `POST /graphql` (queries, mutations),
  `GET /graphql` upgrade to WebSocket (subprotocol
  `graphql-transport-ws`; queries and mutations are also legal over the
  socket per the protocol). TLS terminated locally or `trusted_proxy` mode
  with mandatory proxy allowlist.
- **Admin listener**: separate port, separate authentication (bearer token
  or mTLS). Endpoints: `/healthz`, `/readyz`, `/metrics`,
  `/admin/v1/connections`, `/admin/v1/subscriptions`, `/admin/v1/drain`,
  `/admin/v1/revocations`, `/admin/v1/publish`, `/admin/v1/config`,
  `/admin/v1/diagnostics`. The admin API is versioned under `/admin/v1`.

### 5.2 Schema directives (public contract)

- `@source(name: String!, ...)`: binds a field to a configured data source.
- `@auth(rule: String!, ...)`: field-level authorization rule reference.
- `@filterable`: marks a subscription argument as index-eligible.
- `@backpressure(policy: DROP_OLDEST | COALESCE_BY_KEY | DISCONNECT,
  queue: Int, coalesceKey: String)`: per-subscription-field overflow policy.
- `@complexity(cost: Int, multipliers: [String!])`: complexity accounting.

Complexity is calculated for the selected, spec-valid operation after
variables, defaults, and `@skip`/`@include` are resolved. Every active
syntactic field occurrence in every active fragment expansion is charged;
repeated spreads, duplicate response keys, aliases, and all statically valid
type-condition branches are charged separately. A field contributes its
non-negative declared cost (default 1) multiplied by the product of every
declared multiplier on that field and its field ancestors; that product also
scales descendants. Multiplier names are unique argument names on the same
field and must reference built-in `Int`/`Int!`; effective values include
operation and schema defaults and must be present, non-null, and non-negative
(zero is allowed), otherwise the request is invalid. Lists have no implicit
multiplier. Depth is the maximum number of active field occurrences on a
root-to-leaf path, with the root field at 1 and fragment, inline-fragment,
type-condition, list, and non-null wrappers transparent. Fragment cycles fail
spec validation. Limits are `limits.max_query_depth` (default 15) and
`limits.max_query_complexity` (default 10,000); equality is accepted. Depth is
checked before cost. Depth rejection uses `extensions.code = invalid_request`
and integer `depth`/`max_depth`; cost rejection uses
`extensions.code = complexity_exceeded` and exact base-10 string
`cost`/`max_cost` values.

### 5.3 Configuration and CLI

One executable `conduit` with subcommands: `conduit serve`,
`conduit validate` (config + SDL, exits nonzero on any error),
`conduit version`, `conduit doctor` (environment checks: fd limits, clock,
bus reachability). Configuration precedence: built-in defaults < config file
(YAML) < environment (`CONDUIT_*`) < flags; the merged, redacted, effective
configuration is inspectable via `/admin/v1/config` and logged at startup as
a hash.

### 5.4 Protocol extensions (versioned)

Conduit-specific data rides only in spec-sanctioned extension positions:
`Next.payload.extensions.conduit` (resume position, drop notices),
`ping.payload.conduit` (expiry warning), and the `resume` request inside
`Subscribe.payload.extensions.conduit`. An unmodified reference client that
ignores all of them remains fully functional; that property is a conformance
suite assertion (NFR-COMPAT-001).

## 6. Core User Flows

### 6.1 First run

An operator downloads the binary or image, writes a minimal config (one SDL
file, one data source, `auth.mode: none` explicitly acknowledged as
development-only), runs `conduit validate`, then `conduit serve`, and
executes a query, a mutation, and a filtered subscription from the reference
client within fifteen minutes. Every error on this path names the file, key,
or SDL location that caused it.

### 6.2 Filtered subscription end to end

A developer defines `orderUpdated(region: String! @filterable)` in SDL, a
client subscribes with `region: "eu"`, a mutation publishes an order event
with `region: "eu"`; the client receives `Next`. A second client subscribed
with `region: "us"` receives nothing. The developer can see both outcomes in
the index metrics.

### 6.3 Grant change mid-subscription

A user's session is revoked by an administrator while the user holds live
subscriptions on three nodes. Within the propagation SLO, each node fails
publish-time checks for that principal, sends `error` (`GRANT_REVOKED`) on
each subscription, and closes with 4403. The audit log on each node records
the revocation ID.

### 6.4 Token expiry mid-subscription

A client's JWT nears expiry; the server's `ping` carries the expiry warning;
the client reconnects with a fresh token and resume tokens before expiry and
misses nothing within the buffer horizon. A client that ignores the warning
is cut at expiry with typed errors and 4403.

### 6.5 Slow consumer

A client on a bad network stops draining its socket during an event burst.
Its subscription's configured policy applies: `drop_oldest` evicts and
counts, `coalesce_by_key` collapses by key, `disconnect` closes with 4704.
The node's memory stays within the per-connection budget throughout, and the
drop counters appear in metrics and (for `drop_oldest`/`coalesce`) in the
next delivered message's extensions.

### 6.6 Node loss and resume

A node dies without drain. Clients see the socket drop, reconnect through
the load balancer with jittered backoff, present resume tokens to whichever
node accepts them, and receive replay from that node's buffer plus a
`resume_gap` notice covering what could not be replayed.

### 6.7 Rolling deploy

The operator applies a new version with the documented Kubernetes rollout.
Each node receives the drain signal, stops accepting, paces 4700 closes over
the configured window, and exits. Clients reconnect to remaining capacity.
The mixed-version window rules (NFR-COMPAT-005) hold throughout; no client
sees a protocol behavior change mid-connection.

### 6.8 Capacity planning

An operator expects 120,000 connections, 3 subscriptions each, 2,000
publishes/s. The capacity model (FR-OPS-010) converts that into a node count,
per-node memory expectation, and bus bandwidth figure, each traceable to a
published benchmark row rather than to hope.

## 7. Functional Requirements

Requirement language: "must" clauses are acceptance-bearing. Each ID is owned
by exactly one terminal gate in BUILD_PLAN §19; earlier gates may advance a
requirement without closing it.

### 7.1 GraphQL execution (`FR-GQL`)

- `FR-GQL-001`: Conduit loads operator-supplied SDL plus resolver-binding
  configuration at startup, validates both completely (unknown directives,
  unbound fields, type errors, invalid directive arguments), and refuses to
  start on any error, naming file, line, and rule.
- `FR-GQL-002`: Queries execute per the GraphQL specification (October 2021)
  for supported features: field collection, argument coercion, list and
  non-null propagation, fragments, variables, aliases, and `@skip`/
  `@include`.
- `FR-GQL-003`: Mutations execute serially in document order, and each
  mutation field's configured publish mappings emit publish envelopes only
  after the mutation resolver reports success.
- `FR-GQL-004`: A relational data-source adapter (PostgreSQL) binds fields to
  parameterized statements or views with connection pooling, per-operation
  timeouts, and typed error mapping. String-assembled SQL is forbidden and
  architecturally checked.
- `FR-GQL-005`: An HTTP data-source adapter binds fields to configured
  request templates with allowlisted origins, header policies, timeout,
  retry classification, and bounded response parsing.
- `FR-GQL-006`: A function data-source adapter binds fields to
  operator-supplied endpoints over Unix domain socket or loopback HTTP with
  a versioned request/response contract, timeout, and bounded responses.
- `FR-GQL-007`: Data-source adapters are pluggable behind one `DataSource`
  port; a field's binding names exactly one source; sources never see raw
  client transport data.
- `FR-GQL-008`: Query depth is limited (configurable, default 15); exceeding
  it fails validation before execution with a typed error.
- `FR-GQL-009`: Query complexity is limited via `@complexity` costs and
  multipliers (default cost 1/field, default ceiling 10,000); the computed
  cost is returned in `extensions` when the operation is rejected.
- `FR-GQL-010`: Introspection is policy-controlled: enabled, disabled, or
  admin-only; production mode defaults to disabled, and disabling removes
  introspection fields from validation, not just execution.
- `FR-GQL-011`: All inbound operation documents are bounded before parsing:
  byte size (default 1 MiB), token count (default 20,000), and parse depth;
  exceeding any bound is a typed rejection that allocates no AST.
- `FR-GQL-012`: Execution errors are formatted per spec (`errors`, `path`,
  `locations`, `extensions.code`) and never leak internal addresses, SQL,
  stack traces, or upstream response bodies; the redaction is canary-tested.
- `FR-GQL-013`: Variables are validated and coerced against declared types
  before execution; unknown, missing-required, or type-mismatched variables
  fail the whole operation with locations.
- `FR-GQL-014`: Every operation carries a deadline (configurable per
  operation type, default 30 s query/mutation); expiry cancels in-flight
  data-source calls and returns a typed timeout error.
- `FR-GQL-015`: Operation execution over the WebSocket (single-result
  operations via `Subscribe`/`Next`/`Complete`) behaves identically to HTTP
  execution, sharing the same executor, limits, and authorization path.

### 7.2 Subscription transport (`FR-SUB`)

- `FR-SUB-001`: Conduit accepts WebSocket upgrades only with the
  `graphql-transport-ws` subprotocol; other subprotocols are rejected per
  ADR-0002 (HTTP 400 pre-handshake, 4406 post-handshake).
- `FR-SUB-002`: The unmodified reference `graphql-ws` client interoperates
  completely: connect, subscribe, receive, complete, keepalive, and every
  documented error path, proven by the conformance suite (hard acceptance).
- `FR-SUB-003`: `connection_init` must arrive within the init timeout
  (default 3 s, close 4408 on expiry); a second `connection_init` closes
  4429; any other message before `connection_ack` closes 4401.
- `FR-SUB-004`: `connection_init` payload carries credentials to the auth
  handoff; `connection_ack` is sent only after subscribe-time authentication
  succeeds; failure closes 4403 with no information about which check
  failed.
- `FR-SUB-005`: `Subscribe` with an ID already active on the connection
  closes 4409 (`Subscriber for <id> already exists`); IDs are opaque
  client-chosen strings bounded at 255 bytes.
- `FR-SUB-006`: Events are delivered as `Next` with spec-shaped
  `ExecutionResult` payloads; server-initiated termination of one
  subscription uses `Error` (terminal, array of GraphQL errors) or
  `Complete`; client `Complete` stops delivery promptly and frees the entry.
- `FR-SUB-007`: `Ping`/`Pong` work in both directions; the server pings at
  the configured keepalive interval (default 25 s) and closes 4700-class
  idle handling per FR-CONN-002 rather than a silent RST; unsolicited `Pong`
  is legal and ignored.
- `FR-SUB-008`: Malformed frames — invalid JSON, unknown `type`, missing
  required fields, wrong field types, non-text WebSocket frames — close
  4400 with a reason string that echoes no client bytes.
- `FR-SUB-009`: Inbound protocol messages are bounded (default 512 KiB);
  exceeding the bound closes 4400; the WebSocket library's own frame limit
  is configured to match so no larger frame is ever buffered.
- `FR-SUB-010`: Every close uses the documented close-code table
  (PROTOCOL_CONFORMANCE §6); no path closes with an undocumented code, and
  the conformance suite enumerates all of them.
- `FR-SUB-011`: The protocol state machine is implemented as an explicit
  typed state table; illegal transitions are structurally unrepresentable or
  rejected with typed errors, and the table is exhaustively unit-tested
  before any socket integration.
- `FR-SUB-012`: A hostile client — malformed frames, out-of-order messages,
  oversized payloads, duplicate IDs, init floods, slow reads — cannot crash
  the node, leak its memory, or affect other connections, proven by the
  hostile-client suite and fuzzing (NFR-SEC-008 evidence).

### 7.3 Authorization (`FR-AUTH`)

- `FR-AUTH-001`: OIDC/JWT mode validates signature against cached JWKS
  (bounded refresh, kid rotation), `iss`, `aud`, `exp`, `nbf`, and clock
  skew (default ±30 s), and maps configured claims to the principal.
- `FR-AUTH-002`: API-key mode validates presented keys against a store of
  salted hashes with per-key metadata (tenant, scopes, expiry, revocation
  state); plaintext keys exist only in the presentation instant.
- `FR-AUTH-003`: The custom authorizer hook calls an operator endpoint with
  a versioned request (credential material, connection metadata) and
  receives a versioned decision (principal or denial, TTL); timeout or
  malformed response fails closed.
- `FR-AUTH-004`: Auth modes are configured per tenant/listener; at most one
  mode authenticates a given connection; `auth.mode: none` requires an
  explicit `development_acknowledged: true` and logs a warning at startup.
- `FR-AUTH-005`: The principal model is normative: subject, tenant, scopes,
  claims map, expiry, auth mode, and grant-state epoch; it is immutable per
  connection and never contains raw credentials.
- `FR-AUTH-006`: Subscribe-time authorization evaluates the principal
  against the subscription field and arguments at the named enforcement
  point (`SubscriptionAuthorizer.AuthorizeSubscribe`) before any registry or
  index registration occurs.
- `FR-AUTH-007`: Field-level authorization rules (`@auth`) are evaluated for
  every requested field in queries and mutations; a denied field yields a
  spec-shaped error at that path with the rest of the operation following
  normal null-propagation.
- `FR-AUTH-008`: Authorization rules are named, operator-defined expressions
  over principal, field, arguments, and parent type, validated at startup;
  an SDL referencing an undefined rule fails startup.
- `FR-AUTH-009`: Authorization decisions and their inputs are structured
  (no string-interpolated policy), and every decision produces an auditable
  trace record class (allow/deny, rule, principal subject, decision point)
  under the logging budget.
- `FR-AUTH-010`: Publish-time authorization re-evaluates every candidate
  delivery at the named enforcement point
  (`SubscriptionAuthorizer.AuthorizePublish`) on the subscriber's node,
  against the current grant state and the concrete event payload, before
  enqueue; there is no configuration that skips it for revocable auth modes.
- `FR-AUTH-011`: Publish-time decisions may be cached per (subscription
  entry, grant-state epoch); every revocation, expiry, or policy reload
  advances the epoch; the adversarial suite proves no stale-cache delivery.
- `FR-AUTH-012`: Token expiry mid-subscription follows ADR-0008: warning
  ping at expiry minus the warning window, fail-closed at expiry, typed
  `TOKEN_EXPIRED` errors on live subscriptions, close 4403.
- `FR-AUTH-013`: Grant revocation mid-subscription follows ADR-0008: typed
  `GRANT_REVOKED` errors on affected subscriptions, 4403 close for fully
  revoked principals, no delivery after node-local application.
- `FR-AUTH-014`: Revocations propagate via the bus control subject with a
  measured p99 application latency ≤ 2 s fleet-wide (SLO measured in R5
  evidence).
- `FR-AUTH-015`: During control-channel loss, nodes enter degraded mode
  after the heartbeat timeout and apply the configured policy —
  `fail_closed` (default) or `fail_open_bounded` with a staleness ceiling —
  visibly in health output and logs.
- `FR-AUTH-016`: The degraded-mode policy is explicit configuration with no
  silent default change ever; changing it requires restart or an audited
  admin action.
- `FR-AUTH-017`: Tenant isolation is structural per ADR-0009: no code path
  exists that matches a publish envelope against another tenant's entries,
  and the cross-tenant adversarial probes prove it.
- `FR-AUTH-018`: All authorization failures are indistinguishable to the
  client beyond their typed category (no rule names, no existence oracles
  for fields the principal cannot see when introspection is disabled).

### 7.4 Filter matching (`FR-FILT`)

- `FR-FILT-001`: Subscription arguments compile at subscribe time into typed
  predicates over publish-envelope attributes; compilation failures reject
  the `Subscribe` with a typed error before registration.
- `FR-FILT-002`: Supported predicate forms: equality, membership (`in`,
  bounded list size 100), ordered comparison (`gt`, `gte`, `lt`, `lte`,
  `between`) on numbers and timestamps, boolean presence, and conjunction.
- `FR-FILT-003`: Only arguments declared `@filterable` participate in
  indexing; the SDL validator rejects `@filterable` on unsupported types.
- `FR-FILT-004`: Predicates are type-checked against the schema at subscribe
  time; a type mismatch is a subscribe-time error, never a silent
  never-match.
- `FR-FILT-005`: Disjunctive argument forms normalize into at most 8
  conjunctive entries per subscription; exceeding the bound rejects the
  subscribe with a typed error naming the bound.
- `FR-FILT-006`: Non-indexable predicates (custom matcher hook) register on
  the per-field residual list with a configured ceiling (default 1,000);
  at the ceiling, further non-indexable subscribes are rejected with a typed
  error.
- `FR-FILT-007`: The linear-scan matcher exists permanently as the
  differential oracle; property tests assert exact candidate-set equality
  between index and scan across the full predicate grammar.
- `FR-FILT-008`: The index remains correct under concurrent subscribe,
  unsubscribe, and publish (epoch-snapshot reads), proven under the race
  detector.
- `FR-FILT-009`: Index observability: entry count, residual length, shard
  sizes, match latency histogram, and candidate-set size histogram are
  published metrics.
- `FR-FILT-010`: The index-versus-scan benchmark is a published deliverable
  (BENCHMARK_PLAN §index) showing crossover and scaling on the reference
  workload; the accepted implementation must beat the scan at and above
  10,000 entries.

### 7.5 Cross-node fanout (`FR-FAN`)

- `FR-FAN-001`: A successful mutation with publish mappings emits one
  publish envelope per mapping onto the bus; local subscribers and remote
  subscribers receive equivalent delivery treatment.
- `FR-FAN-002`: The publish envelope is a versioned, schema-validated
  contract (version, tenant, field, publish ID, origin node, publish
  timestamp, attribute map, payload bytes); unknown versions are rejected
  and counted, never partially interpreted.
- `FR-FAN-003`: Every node subscribes to its tenants' publish subjects and
  matches every envelope against its local index (ADR-0005); delivery to a
  matching connection requires no cross-node coordination.
- `FR-FAN-004`: Per-publisher, per-field envelope order is preserved through
  to each connection's outbound queue; no cross-publisher or cross-field
  ordering is promised, and documentation states it.
- `FR-FAN-005`: Node loss follows ADR-0005: no state migration; surviving
  nodes are unaffected except for reconnect load; the fleet suite proves
  continued delivery during and after a node kill.
- `FR-FAN-006`: During bus partition, an isolated node continues serving
  local-publish-to-local-subscriber delivery, marks itself degraded in
  health output, and applies FR-AUTH-015 to authorization staleness; on
  heal, it resumes bus consumption with no replay expectation (gap rules
  apply).
- `FR-FAN-007`: Bus backlog or subscriber overrun (slow node) resolves by
  dropping envelopes with an explicit drop counter, a health signal, and a
  log record — never by unbounded buffering; the fault suite induces it.
- `FR-FAN-008`: Duplicate envelopes (bus redelivery, publisher retry) are
  suppressed per (tenant, field, publish ID) within a bounded dedupe window
  (default 60 s); the fault suite injects duplicates and asserts single
  delivery.
- `FR-FAN-009`: Bus subjects, envelopes, and control messages are tenant
  scoped; a node never subscribes to a tenant it does not serve.
- `FR-FAN-010`: The admin publish endpoint (`/admin/v1/publish`) injects
  envelopes through the same validation, matching, and authorization path as
  mutation-driven publishes.
- `FR-FAN-011`: Publish rate is limitable per tenant (token bucket,
  configurable); exceeding it rejects the mutation's publish step with a
  typed error that names the limit.
- `FR-FAN-012`: Fanout observability: envelopes published/received/matched/
  deduplicated/dropped, per-stage latency, and bus connection state are
  published metrics.

### 7.6 Connection lifecycle and backpressure (`FR-CONN`)

- `FR-CONN-001`: A per-node connection registry owns every local connection
  and its subscription entries; registration and teardown are atomic with
  respect to matching (no delivery to a torn-down connection, no orphan
  entries after close), proven under the race detector.
- `FR-CONN-002`: Idle timeout: a connection with no client traffic (pongs
  count) for the configured window (default 5 min) closes with 4702.
- `FR-CONN-003`: Maximum connection lifetime (default 12 h) closes with
  4701 after a warning ping, with pacing so co-created cohorts do not close
  simultaneously (jitter ±10%).
- `FR-CONN-004`: Connection quotas per principal and per tenant reject
  excess connections at `connection_init` with 4703 and a typed reason.
- `FR-CONN-005`: Subscription quotas per connection (default 100) and per
  principal reject excess `Subscribe` with a typed error, not a close.
- `FR-CONN-006`: Inbound message rate limiting per connection (token
  bucket, default 50 msg/s burst 100) closes 4400-class abuse with a typed
  reason after warning; the hostile-client suite exercises it.
- `FR-CONN-007`: Every connection has one bounded outbound queue (default
  256 messages or 1 MiB); enqueue beyond the bound triggers the owning
  subscription's backpressure policy; control frames (ping/pong/ack/close)
  bypass the data queue and are never dropped.
- `FR-CONN-008`: Backpressure policy is configurable per subscription field
  via `@backpressure` with deployment defaults: `drop_oldest` evicts the
  oldest queued `Next` for that subscription; `coalesce_by_key` replaces the
  queued event with the same coalesce key; `disconnect` closes 4704.
- `FR-CONN-009`: Policy-caused drops are counted per subscription and
  surfaced: metrics always, and a `conduit.dropped` extension notice on the
  next delivered message for that subscription.
- `FR-CONN-010`: Graceful drain: on signal or admin trigger, the node stops
  accepting upgrades, fails readiness, sends 4700 closes paced over the
  configured drain window (default 60 s), and exits when empty or at
  deadline; in-flight operations get a bounded completion grace.
- `FR-CONN-011`: Overflow and drop behavior is observable per policy and
  per field with bounded-cardinality metrics.
- `FR-CONN-012`: Slow-consumer detection thresholds (queue depth, oldest
  message age) are configurable and emit a structured event before the
  policy fires, so operators can distinguish bursts from stuck clients.
- `FR-CONN-013`: The client listener supports local TLS with reloadable
  certificates or explicit `trusted_proxy` mode with a proxy allowlist;
  plaintext without `trusted_proxy` acknowledgment refuses to start.
- `FR-CONN-014`: The node tracks its file-descriptor budget and stops
  accepting new connections (load-shed with 503 on upgrade) before fd
  exhaustion, with the threshold and current usage in health output.

### 7.7 Reconnect and resume (`FR-RESUME`)

- `FR-RESUME-001`: Every delivered `Next` carries a resume position in
  `extensions.conduit`; positions are per (tenant, field) monotonic within
  a node epoch.
- `FR-RESUME-002`: The resume token is opaque, versioned, HMAC-signed with
  rotating keys, and bounded (≤ 512 bytes); it encodes tenant, field,
  position, node epoch, and issue time; clients treat it as opaque bytes.
- `FR-RESUME-003`: Each node keeps per-(tenant, field) replay ring buffers
  bounded by count and bytes (defaults 4,096 / 16 MiB); horizon age is a
  published metric.
- `FR-RESUME-004`: A `Subscribe` carrying a valid resume token replays
  buffered envelopes after the token position through the full publish-time
  authorization and filter path, then splices to live delivery with no
  duplicate and no gap at the splice point (proven deterministically).
- `FR-RESUME-005`: When replay cannot cover the token position (horizon
  passed, different node without coverage, epoch mismatch), the server
  sends a `resume_gap` notice stating the covered range before live
  delivery begins; it never fabricates completeness.
- `FR-RESUME-006`: Replay preserves per-publisher order and merges with
  live events deterministically at the cutover envelope.
- `FR-RESUME-007`: Invalid resume tokens (bad signature, wrong tenant,
  wrong field, malformed, oversized, expired beyond the configured maximum
  age) are rejected with a typed error; the subscription proceeds as fresh
  with an explicit `resume_rejected` notice; forgery attempts are logged.
- `FR-RESUME-008`: The gap window is measured and documented: buffer
  horizon in seconds at reference publish rates is an R7 benchmark
  deliverable, cited by the public API contract documentation.
- `FR-RESUME-009`: Reconnect storm mitigation: close frames that expect
  reconnection (4700, 4701) carry jittered retry-after hints; accept-rate
  pacing bounds the post-node-loss thundering herd; measured in the R9
  node-loss scenario.

### 7.8 Admin and observability surface (`FR-ADMIN`)

- `FR-ADMIN-001`: The admin listener is a separate port with independent
  authentication (bearer or mTLS); admin endpoints never exist on the
  client listener (architecturally checked).
- `FR-ADMIN-002`: `/admin/v1/connections` and `/admin/v1/subscriptions`
  list and inspect local connections/entries with pagination, principal
  subject, tenant, counts, queue depths, and ages; payload contents are
  never exposed.
- `FR-ADMIN-003`: `/admin/v1/drain` triggers FR-CONN-010 with dry-run
  support; `/admin/v1/revocations` accepts and lists revocations
  (FR-AUTH-013/014); `/admin/v1/publish` implements FR-FAN-010.
- `FR-ADMIN-004`: `/metrics` serves the documented Prometheus catalogue
  within the named cardinality budget (OPERATIONS_TEST_PLAN §observability).
- `FR-ADMIN-005`: `/healthz` (liveness) and `/readyz` (readiness) follow
  the documented semantics: readiness fails during drain and during
  configured degraded conditions; liveness fails only on unrecoverable
  states.
- `FR-ADMIN-006`: `/admin/v1/config` returns the effective merged
  configuration with secrets redacted and the configuration hash.
- `FR-ADMIN-007`: `/admin/v1/diagnostics` produces a support bundle
  (config hash, versions, runtime stats, recent structured events, goroutine
  and heap profiles) with an explicit inventory and no payload or credential
  bytes.
- `FR-ADMIN-008`: All admin mutations (drain, revoke, publish) produce
  structured audit records with actor identity and request ID.

### 7.9 Operations and deployment (`FR-OPS`)

- `FR-OPS-001`: Conduit ships as a single static binary per Tier 1 platform
  and an OCI image (distroless base, nonroot); the image and binary are
  bit-provenanced per FR-OPS-012.
- `FR-OPS-002`: Configuration is schema-validated at startup with the
  documented precedence chain; every error names key, source, and
  expectation; `conduit validate` performs the identical validation
  standalone.
- `FR-OPS-003`: SDL and binding configuration reload atomically on admin
  trigger or SIGHUP: validation runs against the new set, cutover is
  all-or-nothing, existing subscriptions on removed/changed fields are
  completed with a typed error, and a failed validation leaves the old
  schema serving.
- `FR-OPS-004`: Reference Kubernetes manifests (Deployment, Service,
  PodDisruptionBudget, HPA guidance) and the load-balancer requirements for
  long-lived WebSockets (idle timeout floors, connection draining
  interaction, header requirements for `trusted_proxy`) are shipped and
  tested in the R10 cluster suite.
- `FR-OPS-005`: Rolling upgrade supports a mixed-version window of N and
  N+1 (NFR-COMPAT-005): bus envelopes, control messages, and resume tokens
  written by N are readable by N+1 and vice versa within the window, proven
  by cross-version fixtures.
- `FR-OPS-006`: The documented rollout integrates drain (FR-CONN-010) with
  Kubernetes lifecycle (preStop, terminationGracePeriodSeconds, readiness)
  so a standard `kubectl rollout` loses no more than the drain-window
  contract states.
- `FR-OPS-007`: Schema evolution policy: additive changes are safe;
  breaking changes (field removal, type change, filterable removal) require
  a deprecation period with `@deprecated` and admin-visible usage counters
  before removal; the policy document is normative.
- `FR-OPS-008`: A runbook covers every alert shipped in the reference
  dashboards with diagnosis and remediation steps rehearsed by the chaos
  suite; an alert without a runbook entry fails the R8 gate.
- `FR-OPS-009`: Log, metric, and trace conventions are documented with the
  named cardinality budget; CI includes a metrics-contract test that fails
  on undocumented metrics or labels.
- `FR-OPS-010`: The capacity model relates connections, subscriptions per
  connection, publish rate, match rate, and delivery rate to memory, CPU,
  and bus bandwidth per node, with every coefficient traceable to a
  benchmark row.
- `FR-OPS-011`: Uninstall and purge are documented and tested: what state
  exists (none durable beyond logs by design), how to remove it, and how to
  decommission bus subjects and credentials.
- `FR-OPS-012`: Builds are reproducible (pinned toolchain, vendored
  modules, `-trimpath`, stamped version metadata) and artifacts carry
  provenance (SLSA-style attestation, SBOM, signed checksums), verified in
  CI.
- `FR-OPS-013`: `conduit doctor` checks the environment (fd limits, clock
  sync, bus reachability, TLS material validity) and reports actionable
  findings without mutating anything.

## 8. Failure UX

Failure behavior is product surface. The categories below are normative; the
full taxonomy lives in BUILD_PLAN §4.2 and the close-code table in
PROTOCOL_CONFORMANCE §6.

- Every client-visible failure carries a stable machine-readable code
  (`extensions.code` for GraphQL errors, close code plus reason for
  connection-level failures) and a human message that names the bound or
  rule violated without leaking internals.
- Throttling and quota failures name the limit class (not the limit value
  where that would aid abuse) and, where reconnection is expected, carry
  retry-after hints.
- Authorization failures are uniform per FR-AUTH-018.
- Degraded modes (bus partition, revocation staleness, fd pressure) are
  visible in `/readyz` and metrics before they are visible as client harm.
- No failure path may fabricate success: a dropped event is counted and
  noticed, a gap is announced, a failed publish fails the mutation step
  that requested it.

## 9. Non-Functional Requirements

### 9.1 Performance (`NFR-PERF`)

- `NFR-PERF-001`: Publish-to-delivery-enqueue latency on the reference
  single-node workload (BENCHMARK_PLAN §workloads): p50 ≤ 10 ms, p95 ≤ 50
  ms, p99 ≤ 150 ms, measured with coordinated-omission-safe capture.
- `NFR-PERF-002`: Predicate index match cost is sublinear in entry count
  for indexable predicates; p99 match time ≤ 1 ms at 100,000 entries on the
  reference machine.
- `NFR-PERF-003`: Bus added latency (publish on node A to receive on node
  B, same AZ): p95 ≤ 5 ms on the reference fleet configuration.
- `NFR-PERF-004`: Gateway overhead for queries/mutations (Conduit latency
  minus data-source latency): p95 ≤ 5 ms at the reference query load.
- `NFR-PERF-005`: The delivery hot path (match → authorize → enqueue) holds
  zero heap allocations per delivery in steady state, enforced by an
  allocation regression test.
- `NFR-PERF-006`: All published latency numbers include GC evidence
  (gctrace capture, `GOGC`/`GOMEMLIMIT` settings) per ADR-0001.

### 9.2 Scale (`NFR-SCALE`)

- `NFR-SCALE-001`: 50,000 concurrent WebSocket connections on a single
  benchmarked node (BENCHMARK_PLAN §environment) with the full protocol
  (keepalive active, authenticated principals, one subscription each
  minimum), sustained for ≥ 30 minutes.
- `NFR-SCALE-002`: Memory per connection: ≤ 64 KiB idle, ≤ 100 KiB p95
  under the reference load, measured as RSS delta per 10,000 connections.
- `NFR-SCALE-003`: A three-node fleet demonstrates fanout scaling: fleet
  delivery throughput at ≥ 2.5× single-node on the reference workload, with
  the loss to bus overhead published, not hidden.
- `NFR-SCALE-004`: 100,000 subscription entries per node in the index with
  NFR-PERF-002 holding.
- `NFR-SCALE-005`: 5,000 publish envelopes/s/node sustained on the
  reference workload with latency targets holding.
- `NFR-SCALE-006`: 500 connection accepts/s/node sustained (churn and
  reconnect-storm scenarios) without latency-target violation for
  established connections.

### 9.3 Security (`NFR-SEC`)

- `NFR-SEC-001`: Every byte of client input crosses a bounded parser before
  any allocation-proportional processing (documents, protocol messages,
  headers, tokens, resume tokens).
- `NFR-SEC-002`: Authorization is not bypassable: each enforcement point is
  named in AUTHORIZATION_MODEL §enforcement, and each carries adversarial
  evidence (bypass-attempt tests) in the gate that owns it.
- `NFR-SEC-003`: Revocation propagation meets the ADR-0008 SLO and its
  degraded-mode policy defaults to fail-closed.
- `NFR-SEC-004`: No secret material (credentials, tokens, key bytes, JWKS
  keys, API-key plaintext) appears in logs, traces, metrics, errors,
  diagnostics bundles, or admin output; canary-tested at every sink.
- `NFR-SEC-005`: All network legs support TLS: client listener (or
  explicit trusted-proxy), admin listener, bus connection (TLS + credentials
  to NATS), data sources (per-source policy); plaintext requires explicit
  per-leg acknowledgment.
- `NFR-SEC-006`: Tenant isolation is structural (ADR-0009) with
  adversarial cross-tenant probes as gate evidence.
- `NFR-SEC-007`: Resume tokens are unforgeable in practice: HMAC-SHA-256
  with rotating keys, constant-time verification, forgery logging.
- `NFR-SEC-008`: Denial-of-service resistance is tested: quotas, rate
  limits, slow-read/slow-write (slowloris) timeouts, init floods, oversized
  frames, compression disabled by default (no permessage-deflate
  amplification), fuzzing on all frame parsers.
- `NFR-SEC-009`: Redaction and log rate-limiting protect the node from
  hostile-input log amplification.
- `NFR-SEC-010`: Dependencies follow the review gate (BUILD_PLAN §4.6):
  pinned versions, license and transitive review, vulnerability scanning in
  CI with a documented triage policy.

### 9.4 Compatibility (`NFR-COMPAT`)

- `NFR-COMPAT-001`: The unmodified reference `graphql-ws` client (pinned
  version range documented in PROTOCOL_CONFORMANCE) passes the full
  conformance suite against Conduit; Conduit extensions are invisible to it.
- `NFR-COMPAT-002`: GraphQL behavior conforms to the October 2021
  specification for all supported features; deviations are enumerated (none
  permitted silently).
- `NFR-COMPAT-003`: Public contracts are versioned before any compatibility
  promise: protocol extensions, resume token, publish envelope, control
  messages, configuration schema, admin API, function-source contract.
- `NFR-COMPAT-004`: Platform support follows ADR-0011's tiers; no claim
  attaches to an untested platform.
- `NFR-COMPAT-005`: A mixed-version fleet of releases N and N+1 operates
  correctly for all versioned contracts during the rolling-upgrade window;
  cross-version fixtures are release-blocking from the first tagged
  release.
- `NFR-COMPAT-006`: Deprecations (schema, config keys, admin API) follow
  the documented policy: announce, dual-support ≥ 1 minor release, remove
  with changelog notice.

### 9.5 Maintainability (`NFR-MAINT`)

- `NFR-MAINT-001`: Package import boundaries are enforced by an
  architecture check in CI (e.g. transport cannot import executor
  internals; nothing imports the WebSocket library outside `transport`;
  nothing imports gqlparser outside `graphql/ast`).
- `NFR-MAINT-002`: Test-first policy per BUILD_PLAN §4.1 with a coverage
  floor of 80% on non-generated code and mutation testing on the
  authorization and predicate paths.
- `NFR-MAINT-003`: Every error category has a typed error, a metric, and a
  documented meaning; unknown-error paths are counted and alarmed.
- `NFR-MAINT-004`: Documentation status discipline: current-versus-planned
  is updated in the same change as the behavior; CI includes a doc-status
  lint for the configured placeholder marker and premature-delivery phrase outside
  OPEN_QUESTIONS.
- `NFR-MAINT-005`: The dependency budget is explicit: runtime dependencies
  are enumerable in one screen (WebSocket, gqlparser, NATS client, pgx,
  OTel, Prometheus client, jwx for JOSE, yaml) and each addition passes the
  §4.6 review.
- `NFR-MAINT-006`: Tests are deterministic: injected clocks, no wall-clock
  sleeps for correctness, seeded randomness with logged seeds, bounded
  polling with deadlines only in integration suites.

## 10. Acceptance Criteria and Release Tiers

### 10.1 Release tiers

| Tier | Contents | Gate evidence required |
| --- | --- | --- |
| Developer preview | §4.1 scope; single-node and small-fleet use by early adopters; contracts marked unstable | R0–R7 accepted |
| Conduit 1.0 | §4.2 scope; production support statement; versioned contracts frozen | R0–R10 accepted |
| Post-1.0 | items in OPEN_QUESTIONS with reopen triggers | new ADRs + new gates |

### 10.2 Acceptance rules

- A requirement is satisfied only when its terminal gate's evidence
  checklist (BUILD_PLAN §X.9 of the owning gate) passes in CI, including
  its failure-path and adversarial rows — never on the strength of a
  happy-path demonstration.
- The release tiers above are cumulative; no tier ships with a lower tier's
  gate re-opened.
- Public claims for each tier are bounded by the MARKETING_PLAN claims
  ladder; a claim without an accepted gate behind it may not ship in any
  channel (README, docs, launch post, conference talk).

### 10.3 Requirement count summary

| Namespace | Count | Terminal owners |
| --- | --- | --- |
| FR-GQL | 15 | R1 (13), R3 (FR-GQL-010 introspection policy with auth, FR-GQL-012 error redaction final) |
| FR-SUB | 12 | R2 |
| FR-AUTH | 18 | R3 (16), R5 (FR-AUTH-014, FR-AUTH-015 fleet SLO) |
| FR-FILT | 10 | R4 |
| FR-FAN | 12 | R5 |
| FR-CONN | 14 | R6 (12), R2 (FR-CONN-001), R8 (FR-CONN-010 drain) |
| FR-RESUME | 9 | R7 (8), R9 (FR-RESUME-009 storm measurement) |
| FR-ADMIN | 8 | R8 |
| FR-OPS | 13 | R10 (11), R8 (FR-OPS-008, FR-OPS-009) |
| NFR-PERF | 6 | R4 (002), R6 (005), R9 (001, 003, 004, 006) |
| NFR-SCALE | 6 | R9 |
| NFR-SEC | 10 | R0 (010), R2 (001), R3 (002, 006), R5 (003), R6 (008, 009), R7 (007), R8 (004, 005) |
| NFR-COMPAT | 6 | R2 (001, 002), R7 (003), R10 (004, 005, 006) |
| NFR-MAINT | 6 | R0 (001, 004, 005, 006), R1 (003), R10 (002 final floor) |

The per-ID authoritative mapping, including every split noted above, is
BUILD_PLAN §19; where this table and that matrix disagree, the matrix
controls.
