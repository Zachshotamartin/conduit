# Conduit Threat Model

Document status: accepted.
Normative security analysis. Last revised: 2026-08-30.

Nothing in this document is an implementation claim. Every control below is `planned`; every
evidence line names the future gate evidence (test family plus owning gate R0–R10 per BUILD_PLAN)
that must exist before the corresponding claim may be made. A security claim without its named
evidence passing in CI is not a claim Conduit makes. Test family names (UNIT, PROTO, HOST, AUTHZ,
INDEX, FAN, BP, RESUME, CONN, CHAOS, LOAD, PKG) follow OPERATIONS_TEST_PLAN; requirement IDs are
minted only by PRODUCT_REQUIREMENTS.

## 1. Scope and Security Objective

This threat model covers Conduit, a self-hosted GraphQL gateway centered on WebSocket
subscriptions: clients connect over `graphql-transport-ws`, subscribe with per-subscription
filters, mutations publish envelopes onto a NATS bus, every node matches envelopes against its
local predicate index, and each candidate delivery passes publish-time authorization before
enqueue onto a bounded per-connection outbound queue. In scope: the client listener, protocol
state machine, GraphQL execution path, the three data-source adapter types, the bus leg, the
predicate index, the resume subsystem, the admin surface, observability sinks, and the build and
release pipeline.

Primary security objective, in priority order:

1. **Event confidentiality per grant**: no principal ever receives an event payload its current
   grant state does not permit — including after revocation or expiry, across tenants, and through
   resume replay (NFR-SEC-002, NFR-SEC-003, NFR-SEC-006).
2. **Control-plane integrity**: revocations, drain signals, admin actions, and configuration
   changes are accepted only from authenticated, authorized origins, and their propagation cannot
   be forged or suppressed without a visible degraded mode (FR-AUTH-014, FR-AUTH-015,
   FR-ADMIN-001).
3. **Node availability**: a hostile network client, a hostile tenant, or a misbehaving data source
   cannot crash a node, exhaust its memory or file descriptors, or degrade other connections
   beyond quota-bounded interference (NFR-SEC-008, FR-SUB-012).

Explicitly out of scope — Conduit does not defend against:

- A compromised host: root or memory access on a node reads every local secret and every local
  tenant's traffic.
- A malicious operator: whoever controls configuration, keys, and deployment defeats every control
  here by definition.
- Data-source-internal security: Conduit bounds and authenticates its calls; behavior inside
  Postgres, HTTP origins, and function endpoints is theirs.
- Hard cross-tenant performance isolation within one process: ADR-0009 states this non-guarantee;
  deployments requiring it run per-tenant fleets.

## 2. Assets

### 2.1 High-value assets

- Credentials in flight: JWTs and API keys in `connection_init` payloads, admin bearer tokens, bus
  credentials at NATS connect.
- Key material: cached JWKS documents, resume-token HMAC-SHA-256 rotating keys (FR-RESUME-002),
  the API-key salted hash store (FR-AUTH-002), TLS private keys, admin mTLS material.
- Event payloads: every publish envelope's payload bytes — in the bus, in replay ring buffers, and
  in outbound queues.
- Authorization state: principals, the revocation set, grant-state epochs, the publish-time
  decision cache (FR-AUTH-011), authorization rules.
- Admin credentials and the admin API's mutating endpoints.
- Bus credentials and the tenant-scoped subject namespace (`conduit.<tenant>.pub.<field>`,
  `conduit.<tenant>.ctl.<kind>`, ADR-0004).
- Supply-chain artifacts: source tree, pinned dependencies, release binary, OCI image, SBOM,
  signatures, provenance (FR-OPS-012, NFR-SEC-010).

### 2.2 Availability assets

- File-descriptor budget per node (FR-CONN-014).
- Memory budget: outbound queues, replay buffers, the predicate index, the dedupe window, the
  revocation set — every one bounded by design.
- Bus bandwidth: every node receives every tenant envelope (ADR-0005), so bus capacity is a shared
  fleet asset.
- CPU on the delivery hot path: parse, match, authorize, serialize (NFR-PERF-005).

## 3. Actors

- **Anonymous network client**: reaches the client listener with no credential; supplies every
  pre-authentication byte.
- **Authenticated low-privilege client**: holds a valid grant for some fields in one tenant;
  probes beyond it.
- **Hostile tenant co-resident**: a legitimate tenant-A principal attacking tenant B's
  confidentiality or the shared node's availability.
- **Revoked-but-connected client**: authenticated at `connection_init`; revoked or expired while
  the socket stays open.
- **Compromised data source**: a Postgres server, HTTP origin, or function endpoint returning
  hostile responses or stalling.
- **Network man-in-the-middle**: on-path between client and listener, node and bus, or node and
  data sources.
- **Compromised bus peer**: any holder of NATS credentials able to publish on Conduit subjects — a
  compromised node, a stolen bus credential, or a misconfigured co-tenant of the NATS server.
- **Malicious admin-credential holder**: holds the admin bearer token or mTLS identity,
  legitimately or by theft.
- **Supply-chain attacker**: controls a dependency, the CI pipeline, or a distribution channel for
  the binary or image.

## 4. Trust Assumptions

- The operating system, container runtime, Go runtime, and standard cryptographic implementations
  are trusted within documented limits.
- The NATS server is trusted once mutually authenticated over TLS (NFR-SEC-005): Conduit assumes
  per-publisher subject order and no message fabrication, verifies those guarantees in the R5
  broker-integration suite rather than taking them on faith (ADR-0004), and still schema-validates
  every envelope and control message on receipt (FR-FAN-002).
- The load balancer preserves bytes; in `trusted_proxy` mode its identity headers are trusted only
  from the mandatory proxy allowlist (FR-CONN-013).
- Operator configuration is trusted input but still schema-validated with fail-fast startup errors
  (FR-OPS-002); trusting the operator does not extend to accepting malformed or ambiguous
  configuration.
- Clocks are synchronized within the configured skew allowance (default ±30 s, FR-AUTH-001);
  `conduit doctor` checks sync (FR-OPS-013), and skew beyond the allowance is a §10 residual risk,
  not a defended condition.

## 5. Trust Boundaries

```text
Boundary A: Network client -> client listener (TLS / trusted_proxy)
Boundary B: WebSocket frames -> protocol parser and state machine
Boundary C: Operation document -> GraphQL parser and validator
Boundary D: Presented credentials -> auth modes (OIDC-JWT, API key, custom)
Boundary E: Node -> bus (NATS publish, subscribe, control subjects)
Boundary F: Publish envelope -> predicate matcher and index
Boundary G: Subscription entry -> publish-time authorizer
Boundary H: Node -> data sources (Postgres, HTTP, function endpoint)
Boundary I: Admin client -> admin listener (separate port, separate auth)
Boundary J: Build system -> release artifacts (binary, image, provenance)
Boundary K: Logs, metrics, traces -> operators and their tooling
```

Trusted responsibility versus untrusted input, per boundary:

- **A**: the listener owns TLS termination, accept pacing, and fd load-shedding; every network
  byte and handshake field is untrusted.
- **B**: the protocol layer owns the typed state table and frame bounds (FR-SUB-011); every
  frame's size, type, shape, and order is untrusted.
- **C**: the GraphQL layer owns document bounds, validation, depth, and complexity limits; every
  document byte and variable is untrusted (FR-GQL-011).
- **D**: auth modes own verification and principal construction (FR-AUTH-005); every credential
  and authorizer response is untrusted until verified (FR-AUTH-003).
- **E**: the bus adapter owns TLS, credentials, subject scoping, and bounded pending limits; every
  received envelope and control message is untrusted input revalidated against its versioned
  schema (FR-FAN-002).
- **F**: the matcher owns tenant-sharded candidate lookup (ADR-0009); envelope attributes are
  untrusted values evaluated only against compiled, typed predicates (FR-FILT-001).
- **G**: `SubscriptionAuthorizer.AuthorizePublish` owns the delivery decision (FR-AUTH-010); the
  candidate set is an unauthorized proposal.
- **H**: data-source adapters own parameterization, origin allowlists, timeouts, and bounded
  response parsing; every source response byte is untrusted (FR-GQL-004..006).
- **I**: the admin listener owns independent authentication and audit records (FR-ADMIN-001,
  FR-ADMIN-008); every admin request body is untrusted even from an authenticated actor.
- **J**: the release pipeline owns reproducible builds, SBOM, signing, and provenance
  (FR-OPS-012); every dependency and build input is untrusted until pinned and reviewed
  (NFR-SEC-010).
- **K**: the observability layer owns redaction, cardinality budgets, and log rate limits
  (NFR-SEC-004, NFR-SEC-009); everything it emits derives from untrusted, attacker-influencable
  input.

## 6. Data Flows

### 6.1 Connection establishment

A TCP connection crosses Boundary A (TLS or allowlisted proxy), upgrades with the
`graphql-transport-ws` subprotocol or is rejected (FR-SUB-001), and enters the state machine at
Boundary B. `connection_init` must arrive within the init timeout (4408 on expiry, FR-SUB-003);
its credential payload crosses Boundary D to exactly one auth mode (FR-AUTH-004), which returns an
immutable principal or a 4403 close. `connection_ack` is sent only after authentication succeeds
(FR-SUB-004); quotas are checked before registry admission (4703, FR-CONN-004).

### 6.2 Subscribe

A `Subscribe` frame crosses Boundary B (bounds, state, duplicate-ID check — 4409), its document
crosses Boundary C (bounds, validation, depth, complexity), its arguments compile into typed
predicates (FR-FILT-001), and the request crosses `SubscriptionAuthorizer.AuthorizeSubscribe`
before any registry or index registration occurs (FR-AUTH-006). Only then does the entry enter the
tenant-sharded index at Boundary F.

### 6.3 Mutation publish and fanout

A mutation executes across Boundaries C and H; on resolver success, each publish mapping emits a
schema-validated envelope (FR-GQL-003, FR-FAN-002) onto the tenant publish subject across Boundary
E. Every node revalidates the envelope, dedupes it (FR-FAN-008), and matches it in its local
tenant shard at Boundary F. Each candidate crosses Boundary G — publish-time authorization against
current grant state and the concrete payload (FR-AUTH-010) — before enqueue on the bounded
outbound queue (FR-CONN-007).

### 6.4 Resume

A reconnecting client presents a resume token inside `Subscribe` extensions across Boundaries A–C.
The token crosses a bounded verifier (≤ 512 bytes, HMAC-SHA-256, constant time, FR-RESUME-002,
NFR-SEC-007) before any encoded field is interpreted. Valid tokens trigger replay from the local
ring buffer, every replayed envelope crossing Boundaries F and G exactly as live events do
(FR-RESUME-004); uncovered positions produce an honest `resume_gap` notice (FR-RESUME-005);
invalid tokens are rejected with `resume_rejected` and logged as forgery attempts (FR-RESUME-007).

### 6.5 Revocation propagation

A revocation enters through the admin API (Boundary I, audited, FR-ADMIN-008) or the custom
authorizer feed, and is published on `conduit.<tenant>.ctl.revoke` across Boundary E (ADR-0008).
Each node validates it, applies it to the in-memory revocation set, advances the grant-state epoch
(invalidating the decision cache, FR-AUTH-011), fails subsequent Boundary G decisions closed, then
sweeps affected subscriptions with `GRANT_REVOKED` errors and 4403 closes (FR-AUTH-013). Fleet p99
application latency ≤ 2 s (FR-AUTH-014); control heartbeat loss triggers degraded mode with
`fail_closed` default (FR-AUTH-015).

## 7. Threats by Attack Surface

### 7.1 WebSocket handshake and frames

Threats:

- Slowloris: trickled handshake or frame bytes pinning fds and buffers.
- Oversized frames; fragmentation abuse reassembling past intended bounds.
- Compression bombs via permessage-deflate negotiation.
- Binary or interleaved control frames confusing parsing.
- Subprotocol downgrade or omission to reach a laxer code path.

Controls:

- Handshake and frame read deadlines at the transport listener close connections that cannot
  produce complete frames in time (NFR-SEC-008).
- Inbound message bound (default 512 KiB) at the protocol reader, with the WebSocket library's
  frame limit configured to match so no larger frame is ever buffered; fragment reassembly counts
  against the same bound; violation closes 4400 (FR-SUB-009).
- permessage-deflate is never negotiated: the upgrade handler strips the extension offer, so no
  inflate path exists to bomb (NFR-SEC-008).
- Non-text frames close 4400 at the frame reader (FR-SUB-008); missing or wrong subprotocol is
  rejected at the upgrade handler — HTTP 400 pre-handshake, 4406 post-handshake (FR-SUB-001).
- Accept-side fd-budget check at the listener sheds load with HTTP 503 before exhaustion
  (FR-CONN-014).

Evidence (planned):

- HOST slow-read/slow-write, oversized-frame, and fragmentation suites (R2) must show bounded
  memory, deadline closes, and 4400/4406 paths.
- HOST fuzzing of the frame reader (R2, NFR-SEC-001 evidence) must show zero crashes and no
  allocation proportional to claimed sizes.
- CONN fd-pressure test (R6) must show 503 load-shed before accept failure.

Residual risk:

- A distributed attacker can consume the full configured connection budget with protocol-correct
  connections; quotas bound per-principal and per-tenant abuse, not aggregate anonymous pressure.

### 7.2 Protocol state machine

Threats:

- Out-of-order messages exploiting implicit state.
- Duplicate subscription IDs racing registration and teardown.
- Init floods: repeated init on one socket, or sockets that never init.
- Ping abuse forcing pong work; unsolicited pongs as a free byte stream.
- Crafted messages hitting undefined transitions to crash or wedge.

Controls:

- An explicit typed state table (FR-SUB-011) at the protocol dispatcher: illegal transitions are
  structurally unrepresentable or rejected typed — pre-ack traffic closes 4401, second init 4429,
  init timeout 4408 (FR-SUB-003); never-init sockets are reaped at the timeout.
- Duplicate-ID check and registration are atomic within the connection's registry entry
  (FR-CONN-001); duplicates close 4409, IDs bounded at 255 bytes (FR-SUB-005).
- Per-connection inbound rate limiting (token bucket, default 50 msg/s burst 100) at the protocol
  dispatcher closes abuse with a 4400-class typed reason after warning (FR-CONN-006); pings and
  pongs count.
- Unknown types and malformed JSON close 4400 with reasons echoing no client bytes (FR-SUB-008).

Evidence (planned):

- UNIT exhaustive state-table tests (R2) must cover every (state, message) pair before socket
  integration (FR-SUB-011 evidence).
- PROTO conformance suite (R2) must enumerate 4401, 4408, 4409, and 4429 paths and prove
  reference-client interoperability (FR-SUB-002, NFR-COMPAT-001).
- HOST out-of-order, duplicate-ID, init-flood, and ping-flood suites (R2) must show no crash, no
  leak, no cross-connection effect (FR-SUB-012).

Residual risk:

- Rate limits are per connection; many principals under quota can generate aggregate protocol
  traffic up to the sum of their buckets.

### 7.3 GraphQL documents

Threats:

- Parser bombs: pathological tokens, deep nesting, huge documents.
- Depth and complexity abuse: legal documents with superlinear cost.
- Introspection probing to map fields and authorization structure.
- Alias flooding multiplying resolver work for one field.
- Variable abuse: oversized payloads, type confusion, coercion tricks.

Controls:

- Document bounds before parsing at the executor intake: 1 MiB, 20,000 tokens, and parse depth;
  exceeding any bound is a typed rejection that allocates no AST (FR-GQL-011, NFR-SEC-001).
- Depth limit (default 15, FR-GQL-008) and complexity ceiling (default 10,000, FR-GQL-009) at
  document validation; aliases each count toward the total, so alias fan-out spends the budget.
- Introspection policy at the validator: production default disabled, and disabling removes
  introspection fields from validation so probes fail as unknown fields (FR-GQL-010) with no
  existence oracle (FR-AUTH-018).
- Variable validation and coercion against declared types before execution at the executor
  boundary (FR-GQL-013); per-operation deadlines cancel in-flight source calls (FR-GQL-014).

Evidence (planned):

- HOST parser fuzzing plus a pathological-document corpus (R1/R2) must show zero crashes and
  rejection-before-allocation on every over-bound input (FR-GQL-011 evidence).
- UNIT depth, complexity, and alias tests (R1) must show typed rejections with computed cost in
  `extensions` (FR-GQL-009); HOST variable-abuse corpus (R2) must show bounded processing
  (FR-GQL-013).
- AUTHZ introspection probes (R3) must show identical unknown-field errors for hidden and
  nonexistent fields (FR-GQL-010, FR-AUTH-018).

Residual risk:

- Complexity costs are operator-configured; a schema whose declared costs understate real resolver
  cost re-opens execution-cost abuse until corrected.

### 7.4 Authentication modes

Threats:

- JWT algorithm confusion: `alg: none`, or asymmetric-to-HMAC confusion.
- `kid` confusion steering verification to a weak or wrong key.
- JWKS poisoning or outage leaving the node unable to verify.
- API-key stuffing; timing oracles on near-miss credentials.
- Custom-authorizer spoofing or stalls wedging the handshake path.

Controls:

- The JWT verifier at the OIDC auth mode accepts only the configured allowlist of asymmetric
  algorithms; `none` and HMAC verification of OIDC tokens are unrepresentable in verifier
  configuration (FR-AUTH-001).
- `kid` selects only among cached JWKS keys for the configured issuer; unknown `kid` triggers one
  bounded refresh, then fails closed. JWKS is fetched only from the configured issuer URL over TLS
  with bounded size and refresh rate (NFR-SEC-005); refresh failure serves the cache to its
  maximum age, then fails closed — a §10 availability cost, never a bypass (FR-AUTH-001).
- API keys verify against salted hashes with constant-time comparison at the API-key mode;
  plaintext exists only in the presentation instant (FR-AUTH-002); failed init attempts are
  rate-limited per source at the listener before hash work is spent.
- Uniform 4403 on every authentication failure with no indication of which check failed
  (FR-SUB-004, FR-AUTH-018); exactly one auth mode per connection (FR-AUTH-004).
- The custom authorizer is called over TLS or UDS with a versioned request; timeout (2 s) or a
  malformed response fails closed at the auth handoff without blocking others (FR-AUTH-003).

Evidence (planned):

- AUTHZ adversarial suite (R3, NFR-SEC-002 evidence) must cover alg-confusion, `none`,
  forged-`kid`, tampered signature, wrong issuer/audience, and expired/nbf fixtures — all 4403.
- AUTHZ JWKS-outage and poisoned-response fixtures (R3) must show cached service then fail-closed;
  statistical timing measurements (R3) must show no exploitable near-miss difference.
- AUTHZ custom-authorizer fault fixtures (R3: timeout, malformed body, wrong version, reset) must
  all fail closed (FR-AUTH-003); HOST key-stuffing fixture (R2/R6) must show bounded hash work.

Residual risk:

- A stolen credential authenticates until expiry or revocation — bounded, not eliminated. JWKS
  unavailability past the cache maximum age locks out new OIDC connections by design (§10).

### 7.5 Authorization state and revocation

Threats:

- Revoke-then-publish race delivering after node-local apply.
- Stale decision cache serving allows past a grant change.
- Degraded-mode abuse: partitioning the control channel to freeze revocation state, exploiting
  `fail_open_bounded` staleness.
- Epoch manipulation via forged or replayed control messages.
- Revocation-set flooding to exhaust memory.

Controls:

- Publish-time authorization at `SubscriptionAuthorizer.AuthorizePublish` on the subscriber's node
  for every candidate delivery; no configuration skips it (FR-AUTH-010, NFR-SEC-002).
- Decision cache keyed by (subscription entry, grant-state epoch); every revocation, expiry, and
  policy reload advances the epoch atomically before the revocation is acknowledged as applied, so
  no post-apply lookup hits a pre-revocation entry (FR-AUTH-011, ADR-0008).
- Application order at the revocation manager: update set, advance epoch, then sweep
  (FR-AUTH-013). The epoch is node-local and monotonic, derived from applied control messages,
  never from message contents; control messages are schema-validated, carry `revocation_id`, and
  are idempotent.
- Degraded mode is entered visibly after the control heartbeat timeout (default 10 s);
  `fail_closed` default suspends deliveries for revocable-auth-mode principals;
  `fail_open_bounded` enforces its staleness ceiling then suspends; the policy is explicit, logged
  configuration (FR-AUTH-015, FR-AUTH-016, NFR-SEC-003).
- Revocation-set entries expire with the grants they revoke plus slack; set size is a published,
  alarmed metric (ADR-0008); the admin revocation endpoint is authenticated and audited
  (FR-ADMIN-003).

Evidence (planned):

- AUTHZ revoke-then-publish race suite (R3) with injected clocks must show zero deliveries after
  node-local apply (FR-AUTH-013), and the AUTHZ stale-cache suite (R3) must prove no delivery
  under any epoch interleaving (FR-AUTH-011).
- FAN fleet propagation measurement (R5) must show p99 application latency ≤ 2 s (FR-AUTH-014,
  NFR-SEC-003).
- CHAOS control-partition fixtures (R5/R6) must show visible degraded-mode entry, `fail_closed`
  suspension, and ceiling enforcement under `fail_open_bounded` (FR-AUTH-015); UNIT set-bound
  tests (R3) must show bounded memory under revocation floods.

Residual risk:

- Under `fail_open_bounded`, the operator-chosen staleness ceiling permits post-revocation
  delivery during partitions — an explicit, logged operator trade (§10). Between admin
  acknowledgment and node application (≤ 2 s p99), a revoked principal can still receive matching
  events.

### 7.6 Filters and predicate index

Threats:

- Predicate bombs maximizing publish-time evaluation cost.
- Residual-list flooding forcing linear work on every publish.
- Disjunction expansion into combinatorial conjunctive entries.
- Index poisoning via rapid subscribe/unsubscribe churn.
- Cross-tenant probes attempting to match another tenant's envelopes.

Controls:

- Predicates are compiled, typed, and bounded at subscribe time by the predicate compiler:
  supported forms only, membership lists bounded at 100 values, type-checked against the schema;
  failures reject the `Subscribe` before registration (FR-FILT-001, FR-FILT-002, FR-FILT-004).
- Disjunctive forms normalize into at most 8 conjunctive entries, enforced at the compiler with a
  typed error naming the bound (FR-FILT-005).
- The residual list has a per-field ceiling (default 1,000) enforced at registration
  (FR-FILT-006); subscription quotas per connection and per principal bound total entries
  (FR-CONN-005).
- The index is tenant-sharded structurally: an envelope's tenant selects its shard and no
  cross-tenant lookup path exists in the code (FR-AUTH-017, NFR-SEC-006, ADR-0009).
- Concurrency correctness uses epoch-snapshot reads (FR-FILT-008); churn cost is bounded by the
  counting-index structure (ADR-0006) and observable via index metrics (FR-FILT-009).

Evidence (planned):

- INDEX property suite (R4) must show exact candidate-set equality between index and linear-scan
  oracle across the full predicate grammar, including adversarial predicates (FR-FILT-007).
- INDEX adversarial cost fixtures (R4) must hold match latency within NFR-PERF-002 bounds at worst
  case, and the churn suite under the race detector must show correctness during storms
  (FR-FILT-008); UNIT compiler-bound tests (R4) must cover membership, disjunction, and
  residual-ceiling rejections.
- AUTHZ cross-tenant probes with an instrumented index (R3) must show a tenant-A entry never
  appears in tenant-B's candidate set even with identical field and predicates (FR-AUTH-017
  evidence).

Residual risk:

- Residual matching is linear up to its ceiling; a tenant filling its residual budget imposes that
  bounded cost on every publish to the field, degrading shared-node latency within quota limits.

### 7.7 Publish path and bus

Threats:

- Envelope forgery from bus position: fabricated envelopes carrying another tenant's field or
  hostile payloads.
- Subject squatting outside the actor's legitimate tenant scope.
- Control-message forgery: fake revocations as denial of service, or suppressed revocations.
- Partition exploitation freezing authorization state or splitting delivery.
- Replay and duplication causing duplicate delivery.

Controls:

- The bus leg requires TLS plus credentials to NATS (NFR-SEC-005), with per-deployment NATS
  authorization rules restricting each node's credentials to its served tenants' subjects
  (FR-FAN-009); a node never subscribes to a tenant it does not serve.
- Every received envelope is schema-validated at the bus adapter; unknown versions, and envelopes
  whose tenant field mismatches their subject, are rejected and counted, never partially
  interpreted (FR-FAN-002).
- Received envelopes never bypass authorization: matching yields only candidates, and every
  candidate still crosses `SubscriptionAuthorizer.AuthorizePublish` (FR-AUTH-010) — a forged
  envelope can at most deliver its own payload to principals already authorized for that field.
- Control messages are schema-validated at the control-subject consumer; revocations are
  idempotent by `revocation_id` and only ever remove capability, so forgery can deny service but
  never grant access; mass-revocation floods hit the revocation-set alarm (ADR-0008).
- Partition behavior is defined: isolated nodes keep local-to-local delivery, mark degraded
  health, and apply FR-AUTH-015 staleness policy (FR-FAN-006); backlog resolves by counted drops,
  never unbounded buffering (FR-FAN-007).
- Duplicate suppression per (tenant, field, publish ID) within the dedupe window (default 60 s) at
  the fanout intake (FR-FAN-008) preserves at-most-once delivery (ADR-0007); admin publishes
  traverse the same validation and authorization path (FR-FAN-010); publish rate is limitable per
  tenant (FR-FAN-011).

Evidence (planned):

- FAN hostile-envelope corpus (R5) — wrong version, tenant/subject mismatch, oversized payloads,
  malformed attributes — must show rejection with counters and no partial interpretation
  (FR-FAN-002).
- FAN broker-integration suite on real NATS (R5, nightly) must cover broker restart, node kill,
  and induced slow-consumer drops (ADR-0004 verification duty); FAN duplicate-injection fixtures
  (R5) must assert single delivery (FR-FAN-008).
- AUTHZ forged-envelope probe (R3/R5) must show a bus-injected envelope cannot reach any principal
  `AuthorizePublish` denies, including cross-tenant targets.
- CHAOS partition fixtures (R5) must show degraded marking, local-delivery continuation, and
  heal-without-replay semantics (FR-FAN-006).

Residual risk:

- A holder of valid bus credentials for a tenant's subjects can publish arbitrary payloads on that
  tenant's fields to its authorized subscribers and deny service by control floods within alarm
  bounds; NATS server compromise itself is a §10 register entry.

### 7.8 Resume tokens and replay buffer

Threats:

- Token forgery granting replay access.
- Cross-field or cross-tenant replay with a valid token for another (tenant, field).
- Token harvesting: stolen tokens replayed by a different principal.
- Buffer scraping via resume probing across positions or fields.
- Oversized or malformed tokens attacking the verifier.

Controls:

- Tokens are HMAC-SHA-256 signed with rotating keys, versioned, opaque, and bounded at 512 bytes;
  verification is constant-time at the resume verifier before any encoded field is interpreted
  (FR-RESUME-002, NFR-SEC-007, NFR-SEC-001).
- The token encodes tenant, field, position, node epoch, and issue time; the verifier rejects
  tenant or field mismatch against the presenting subscription, expiry beyond maximum age, and
  epoch mismatch, with typed errors (FR-RESUME-007).
- Replay is not an authorization bypass: every replayed envelope passes the full publish-time
  authorization and filter path of the presenting connection's current principal (FR-RESUME-004) —
  a harvested token yields only currently-authorized events; a revoked principal receives nothing.
- Invalid tokens degrade to a fresh subscription with an explicit `resume_rejected` notice;
  forgery attempts are logged at the verifier (FR-RESUME-007) under NFR-SEC-009 rate limits.
- Ring buffers are bounded by count and bytes (defaults 4,096 / 16 MiB per (tenant, field),
  FR-RESUME-003) and tenant-keyed (ADR-0009), so no probe addresses another tenant's buffer;
  uncovered positions produce `resume_gap` honesty (FR-RESUME-005).

Evidence (planned):

- RESUME forgery corpus (R7, NFR-SEC-007 evidence) — bit flips, wrong key, truncation, extension,
  version confusion, oversized tokens — must show constant-time rejection, `resume_rejected`, and
  forgery logs, with a UNIT timing measurement showing no signature-dependent difference.
- RESUME cross-field and cross-tenant fixtures (R7) must show typed rejection with no replay bytes
  emitted, and the splice determinism suite (R7) must prove no duplicate and no gap at the splice
  point (FR-RESUME-004).
- AUTHZ harvested-token fixture (R3/R7) must show a revoked or differently-scoped presenter
  receives only currently-authorized events, and a fully revoked presenter none (FR-RESUME-004
  evidence).

Residual risk:

- Replay buffers hold recent payloads in node memory for their horizon; whoever reads node memory
  (out-of-scope host compromise) reads them. Key rotation bounds forged-token exposure to the
  rotation window if an HMAC key leaks.

### 7.9 Backpressure and resource exhaustion

Threats:

- Connection floods consuming fds, memory, and accept capacity.
- Subscription floods inflating the index and per-entry state.
- Slow consumers as a memory attack: broad subscriptions, stopped reader.
- Publish floods saturating fanout, bus bandwidth, and matching CPU.
- fd exhaustion from churn; log amplification from crafted input.

Controls:

- Connection quotas per principal and per tenant reject excess at `connection_init` with 4703
  (FR-CONN-004); accept-rate pacing bounds churn (NFR-SCALE-006, FR-RESUME-009); subscription
  quotas reject excess `Subscribe` typed (FR-CONN-005).
- One bounded outbound queue per connection (default 256 messages or 1 MiB) at the delivery
  enqueue point; overflow triggers the owning subscription's policy — `drop_oldest`,
  `coalesce_by_key`, or `disconnect` with 4704 — and control frames bypass the data queue so
  closes and pings are never starved (FR-CONN-007, FR-CONN-008).
- Slow-consumer detection emits structured events before the policy fires (FR-CONN-012); drops are
  counted and surfaced (FR-CONN-009); idle timeout (4702, FR-CONN-002) and maximum lifetime with
  jittered 4701 closes (FR-CONN-003) reclaim held connections.
- Per-tenant publish rate limiting at the publish step rejects excess typed (FR-FAN-011); bus
  overrun resolves by counted drops (FR-FAN-007).
- fd-budget tracking at the listener sheds load with 503 before exhaustion, with threshold and
  usage in health output (FR-CONN-014).
- Log rate limits and redaction at the logging layer bound hostile-input log volume (NFR-SEC-009);
  reason strings echo no client bytes.

Evidence (planned):

- BP suite (R6, NFR-SEC-008 evidence) must hold node memory within the per-connection budget while
  hostile slow consumers sit at full queues under bursts, per policy, with drop counters asserted.
- CONN flood suites (R6) must show 4703 rejection, 4702/4701 reclamation, accept pacing, and fd
  load-shed; LOAD sustained run (R9) must hold 50,000 connections within the published memory
  budget for ≥ 30 minutes (NFR-SCALE-001, NFR-SCALE-002).
- BP allocation-regression test (R6) must hold zero heap allocations per delivery in steady state
  (NFR-PERF-005); HOST log-amplification fixture (R6, NFR-SEC-009 evidence) must show
  rate-limited, redacted output.

Residual risk:

- Tenants share process memory, CPU, GC, and bus bandwidth; a noisy tenant degrades co-resident
  latency up to what quotas bound (ADR-0009, §10). `drop_oldest` and `coalesce_by_key` protect the
  node by discarding a slow client's events — counted and noticed, but real loss by design.

### 7.10 Admin surface

Threats:

- Admin auth bypass, or admin routes leaking onto the client listener.
- SSRF via admin publish: `/admin/v1/publish` injecting envelopes that reach unintended tenants or
  bypass authorization.
- Drain abuse: unauthorized or repeated drains as denial of service.
- Diagnostics leakage: bundles or config output exposing credentials, key material, or payloads.

Controls:

- The admin listener is a separate port with independent authentication (bearer or mTLS) at the
  admin router over TLS (NFR-SEC-005); admin endpoints never exist on the client listener,
  enforced by an architecture check in CI (FR-ADMIN-001, NFR-MAINT-001).
- All admin mutations (drain, revoke, publish) produce structured audit records with actor
  identity and request ID at the admin router (FR-ADMIN-008).
- `/admin/v1/publish` injects envelopes through the same validation, matching, and publish-time
  authorization path as mutation publishes (FR-FAN-010), with tenant scoping applied (FR-FAN-009).
- Drain is idempotent with dry-run support and the paced 4700 window (FR-ADMIN-003, FR-CONN-010);
  readiness failure during drain is visible (FR-ADMIN-005).
- `/admin/v1/config` returns secrets redacted with the configuration hash (FR-ADMIN-006);
  diagnostics bundles carry an explicit inventory and no payload or credential bytes
  (FR-ADMIN-007); connection listings never expose payload contents (FR-ADMIN-002).

Evidence (planned):

- UNIT architecture check (R0/R8, NFR-MAINT-001 evidence) must fail CI if any admin route
  registers on the client listener.
- AUTHZ admin-auth probes (R8) — missing, malformed, expired, and wrong-audience credentials
  against every endpoint — must show uniform rejection; FAN admin-publish probes (R8) must show
  injected envelopes subject to full validation and publish-time authorization, including
  cross-tenant denial (FR-FAN-010 evidence).
- CHAOS drain rehearsal (R8) must show paced 4700 closes, audit records, and dry-run isolation
  (FR-CONN-010); UNIT canary scans of config and diagnostics output (R8, NFR-SEC-004 evidence)
  must show seeded secrets absent from every admin response.

Residual risk:

- A stolen admin credential holds full operational power — drain, mass revocation, tenant-scoped
  publish — until rotated; audit records make it attributable, not preventable (§10).

### 7.11 Data sources

Threats:

- SQL injection through field arguments reaching the Postgres adapter.
- SSRF via HTTP source templates reaching internal services.
- Function-endpoint compromise: hostile responses or stalls.
- Response bombs: oversized or slow-trickled responses.
- Source responses leaking into client-visible errors.

Controls:

- The Postgres adapter binds fields to parameterized statements or views only; string-assembled
  SQL is forbidden and architecturally checked in CI (FR-GQL-004, NFR-MAINT-001).
- The HTTP adapter enforces its origin allowlist at request construction: templates bind arguments
  into paths and parameters but never into scheme, host, or port; off-allowlist redirects are
  refused (FR-GQL-005).
- The function adapter speaks only to operator-supplied endpoints over Unix domain socket or
  loopback HTTP with a versioned contract (FR-GQL-006); sources never see raw client transport
  data (FR-GQL-007).
- All adapters enforce per-operation timeouts and bounded response parsing at the adapter boundary
  (FR-GQL-005, FR-GQL-006, FR-GQL-014); per-source TLS policy applies (NFR-SEC-005).
- Execution errors never leak SQL, internal addresses, stack traces, or upstream response bodies;
  the redaction is canary-tested (FR-GQL-012).

Evidence (planned):

- UNIT architecture check plus an injection corpus against the Postgres adapter (R1) must show
  every argument reaches the driver as a parameter, never as SQL text (FR-GQL-004 evidence).
- HOST SSRF corpus against HTTP templates (R1/R2) — host overrides, redirect chains, encoded
  origins, link-local and metadata addresses — must show allowlist refusal at request
  construction.
- HOST hostile-endpoint fixtures (R2) — oversized bodies, slow trickle, malformed contract
  versions, resets — must show bounded parsing and timeout cancellation for all three adapters;
  UNIT redaction canaries (R3) must show seeded source-response markers absent from client errors
  (FR-GQL-012 evidence).

Residual risk:

- Conduit cannot validate the semantic truth of source responses: a compromised source serves
  wrong data to every field bound to it, within bounds and on time; source-internal security is
  out of scope (§1).

### 7.12 Supply chain and release

Threats:

- Dependency compromise: a malicious version of the WebSocket library, gqlparser, NATS client,
  pgx, OTel, JOSE, or YAML dependency.
- Build tampering: CI producing an artifact differing from reviewed source.
- Image mutation: a mutable tag or registry compromise substituting the shipped image.
- Stolen publishing or signing credentials.

Controls:

- The dependency budget is explicit and enumerable in one screen (NFR-MAINT-005); every addition
  passes the review gate — pinned versions, license and transitive review, vulnerability scanning
  in CI with a documented triage policy (NFR-SEC-010).
- Builds are reproducible at the release pipeline (pinned toolchain, vendored modules,
  `-trimpath`, stamped version metadata); artifacts carry SLSA-style attestation, SBOM, cosign
  signatures, and signed checksums, verified in CI (FR-OPS-012).
- The image is distroless, nonroot, and referenced by digest in shipped manifests (FR-OPS-001,
  FR-OPS-004); the binary is a single static artifact per Tier 1 platform (ADR-0011).
- CI actions are pinned and least-privilege; publishing occurs from a protected environment.

Evidence (planned):

- PKG reproducibility check (R0/R10) must rebuild the release from pinned inputs and match digests
  (FR-OPS-012 evidence).
- PKG provenance verification (R10) must validate cosign signatures, SBOM presence, and
  attestation subjects for every artifact, plus a clean-machine install test verifying signature
  and digest checks.
- R0 CI evidence must include vulnerability scanning with the triage policy enforced and a failing
  build on unpinned or unreviewed dependency changes (NFR-SEC-010).

Residual risk:

- A compromise upstream of pinning — a malicious release reviewed and admitted through the
  dependency gate — defeats these controls; review reduces, does not eliminate, this risk.

### 7.13 Observability sinks

Threats:

- Secret leakage: credentials, token bytes, key material, or payloads written to logs, traces,
  metrics, or diagnostics.
- Cardinality bombs: attacker-influenced label values exploding metric series.
- Trace payload leakage: spans carrying payloads or credential attributes.
- Log-volume amplification as node-local denial of service.

Controls:

- No secret material appears in logs, traces, metrics, errors, diagnostics bundles, or admin
  output; redaction is applied at each sink boundary and canary-tested at every sink
  (NFR-SEC-004).
- Metric labels follow the documented catalogue and cardinality budget, with tenant labels capped
  into an `other` bucket beyond the cap (ADR-0009); label values derive from validated
  configuration-defined identifiers at the metrics registration layer, never free-form client
  input (FR-OPS-009, FR-ADMIN-004).
- Spans record structural attributes (decision point, rule name, typed error category) and never
  payload bytes or credential material (FR-OPS-009, FR-AUTH-009).
- Log rate limits bound per-category volume under hostile input (NFR-SEC-009); structured slog
  output with typed fields (ADR-0010) prevents injection of attacker-formatted control text.

Evidence (planned):

- UNIT seeded-canary scans at every sink (R3/R8, NFR-SEC-004 evidence): synthetic credentials, key
  bytes, and payload markers injected through every input path must be absent from logs, traces,
  metrics output, diagnostics bundles, and admin responses.
- UNIT metrics-contract test in CI (R0/R8) must fail on any undocumented metric or label
  (FR-OPS-009).
- HOST cardinality probe (R6/R8) must show hostile field and tenant churn cannot create unbounded
  series; HOST log-amplification fixtures (R6) must show rate-limited output under hostile floods
  (NFR-SEC-009).

Residual risk:

- Redaction is deny-listing at sinks with canary coverage: a secret in an unanticipated shape can
  evade the canary set until added; the corpus grows with every incident (§11).

## 8. Abuse Cases

1. **Revoked contractor keeps a socket open.** A grant is revoked while its holder has live
   subscriptions on three nodes. Expected: each node applies the revocation within the p99 ≤ 2 s
   SLO, publish-time checks fail closed from epoch advance, subscriptions receive `GRANT_REVOKED`,
   the connection closes 4403; zero deliveries after node-local apply. Evidence: AUTHZ race suite
   (R3), FAN fleet SLO measurement (R5).
2. **Tenant A tries to see tenant B's orders.** A tenant-A principal subscribes with tenant B's
   exact field and predicates. Expected: the entry lives only in tenant A's index shard; tenant-B
   envelopes structurally cannot reach it — no candidate, no delivery, no timing oracle. Evidence:
   AUTHZ cross-tenant probes with instrumented index (R3, FR-AUTH-017, NFR-SEC-006).
3. **Botnet holds 40,000 idle sockets.** Anonymous connections complete TLS but never init, or
   init and idle. Expected: init timeout closes 4408 within 3 s; idle timeout closes 4702; quotas
   cap authenticated holdings at 4703; fd load-shed returns 503 before exhaustion; memory stays
   within budget. Evidence: CONN flood suites (R6), LOAD sustain (R9).
4. **Attacker replays a harvested resume token.** A stolen valid token is presented from another
   principal's connection. Expected: replay passes the presenting principal's publish-time
   authorization, emitting only its currently-authorized events; a revoked or unauthorized
   presenter receives none; tenant or field mismatch rejects typed. Evidence: AUTHZ
   harvested-token fixture, RESUME cross-field/tenant fixtures (R7).
5. **Slow consumer as memory weapon.** A client subscribes to a hot field and stops reading during
   a burst. Expected: the outbound queue caps at 256 messages / 1 MiB; the configured policy fires
   (`drop_oldest` evicts and counts, `coalesce_by_key` collapses, `disconnect` closes 4704);
   control frames still flow; memory holds the per-connection budget. Evidence: BP suite (R6,
   FR-CONN-007..009).
6. **JWT with `alg: none` and a forged `kid`.** Expected: the algorithm allowlist rejects `none`
   and HMAC confusion structurally; unknown `kid` triggers one bounded JWKS refresh then fails
   closed; uniform 4403 with no failure detail. Evidence: AUTHZ alg-confusion and forged-`kid`
   fixtures (R3, FR-AUTH-001, FR-AUTH-018).
7. **Compromised bus peer forges a cross-tenant envelope.** An actor with tenant-A bus credentials
   publishes an envelope claiming tenant B. Expected: NATS authorization rules block off-scope
   subjects; a tenant/subject-mismatched envelope is rejected and counted at the bus adapter; even
   a validly-scoped forged envelope reaches only principals `AuthorizePublish` allows for that
   field. Evidence: FAN hostile-envelope corpus, AUTHZ forged-envelope probe (R5).
8. **Publish flood from one tenant.** Mutations publish at maximum rate to saturate the fleet.
   Expected: the per-tenant publish token bucket rejects excess typed, naming the limit class
   (FR-FAN-011); bus overrun drops are counted, never buffered unboundedly (FR-FAN-007); co-tenant
   degradation stays within quota-bounded interference. Evidence: FAN overrun fixtures (R5), LOAD
   mixed-tenant scenario (R9).
9. **Admin publish used as an injection path.** A stolen admin token posts envelopes via
   `/admin/v1/publish` targeting another tenant. Expected: the injected envelope traverses full
   validation, tenant scoping, matching, and publish-time authorization identically to mutation
   publishes; unauthorized deliveries are denied; the action is audited. Evidence: FAN
   admin-publish probes, AUTHZ admin-auth suite (R8).
10. **GraphQL parser bomb over the socket.** A client streams a pathologically nested document.
    Expected: frames over 512 KiB close 4400 before assembly; a document within frame bounds but
    over the 1 MiB / 20,000-token / depth bounds is rejected typed with no AST allocation.
    Evidence: HOST parser fuzz and pathological-document corpus (R2, FR-GQL-011, NFR-SEC-001).
11. **Deploy-window prober floods a draining node.** Expected: the draining node fails readiness,
    refuses upgrades, and paces 4700 closes with jittered retry-after hints; surviving nodes
    absorb reconnects under accept pacing. Evidence: CHAOS drain rehearsal (R8), LOAD node-loss
    reconnect surge (R9, FR-RESUME-009).
12. **Expired-token client ignores the warning ping.** Expected: at expiry, publish-time checks
    fail closed immediately, live subscriptions receive typed `TOKEN_EXPIRED` errors, the
    connection closes 4403; no delivery after the expiry instant. Evidence: AUTHZ expiry suite
    with injected clocks (R3, FR-AUTH-012).

## 9. Security Invariants and Tests

| # | Invariant | Test families | Owning gates |
| --- | --- | --- | --- |
| 1 | No delivery after node-local revocation apply | AUTHZ race suite, FAN fleet propagation | R3, R5 |
| 2 | No delivery after principal expiry instant | AUTHZ expiry suite with injected clocks | R3 |
| 3 | No cross-tenant candidate ever: a tenant's entry never appears in another tenant's candidate set | AUTHZ instrumented-index probes, INDEX property suite | R3, R4 |
| 4 | No allocation proportional to unauthenticated input beyond configured bounds | HOST fuzzing and oversize corpora on frames, documents, tokens, headers | R2 |
| 5 | No secret bytes in any sink (logs, traces, metrics, errors, diagnostics, admin output) | UNIT seeded-canary scans at every sink | R3, R8 |
| 6 | No path skips publish-time authorization for a revocable auth mode: every enqueue is preceded by `AuthorizePublish`, including replay and admin publish | AUTHZ bypass-attempt suite, RESUME replay-auth fixtures, FAN admin-publish probes | R3, R7, R8 |
| 7 | No stale-epoch cached decision serves after epoch advance | AUTHZ stale-cache interleavings | R3 |
| 8 | No unbounded queue, buffer, index, cache, or set: every accumulation point has a configured bound and defined overflow | BP overflow suites, UNIT bound tests, CONN flood suites | R6 |
| 9 | No undocumented close code and no reason string echoing client bytes | PROTO close-code enumeration, HOST malformed-frame corpus | R2 |
| 10 | No resume token acceptance without constant-time HMAC verification, and no replay beyond the presenting principal's current grants | RESUME forgery corpus, AUTHZ harvested-token fixture | R7 |
| 11 | No partially interpreted bus input: envelopes and control messages are schema-valid or rejected-and-counted whole | FAN hostile-envelope corpus, CHAOS partition fixtures | R5 |
| 12 | No admin endpoint reachable on the client listener and no unauthenticated admin mutation | UNIT architecture check in CI, AUTHZ admin probes | R0, R8 |
| 13 | No silent degraded mode: control-channel staleness is entered explicitly, logged, and visible in health output | CHAOS control-partition fixtures | R5, R6 |
| 14 | No unverified release artifact: every shipped binary and image matches reviewed source with valid signature, SBOM, and provenance | PKG reproducibility and provenance verification | R0, R10 |

A violated invariant is a release-blocking defect for the owning gate, not a backlog item.

## 10. Residual Risk Register

| Risk | Why it remains | Mitigation posture | Review trigger |
| --- | --- | --- | --- |
| Shared-process tenant interference (latency, GC, bus bandwidth) | ADR-0009 chooses namespace isolation in one process; a shared Go heap cannot promise hard performance isolation | Quotas, rate limits, and publish token buckets bound interference; docs direct hard-isolation deployments to per-tenant fleets | Any measured cross-tenant latency violation in R9; any compliance-driven deployment request |
| Gap-window data loss by design | ADR-0007 chooses at-most-once live with bounded resume; events beyond the buffer horizon, backpressure drops, and partition-heal windows are unrecoverable | Honest `resume_gap` and drop notices, counted drops, published measured horizon (FR-RESUME-008) | The ADR-0007 reopen trigger: a paying use case that cannot tolerate the gap window |
| `fail_open_bounded` degraded-mode delivery after revocation | FR-AUTH-015 offers the mode as an explicit operator availability trade during control-channel loss | Default is `fail_closed`; the alternative requires explicit, logged configuration with a staleness ceiling (FR-AUTH-016) | Any incident where the ceiling was exceeded or the mode surprised an operator |
| LB-terminated TLS sees plaintext | `trusted_proxy` mode necessarily hands the proxy every client byte | Mandatory proxy allowlist (FR-CONN-013); documentation states the proxy joins the trusted computing base | Any change to proxy-mode header handling; any proxy-layer incident |
| Compromised node sees all local tenant traffic | All tenants on a node share its process memory: principals, payloads, buffers, keys | Out-of-scope by §1; distroless nonroot image and no durable node state shrink the surface and the persistence value of a compromise | Any host-compromise incident; any change adding durable node state |
| GC pauses under memory pressure violate latency targets | Go runtime GC (ADR-0001) can pause under pressure an attacker helps create | Bounded allocations everywhere, zero-alloc hot path (NFR-PERF-005), published GC evidence with every latency claim (NFR-PERF-006) | Any R9 latency result failing under adversarial memory pressure |
| NATS server compromise | The bus is trusted once mutually authenticated (§4); a compromised broker can drop, delay, reorder within limits, or inject on subjects it serves | Schema validation of all bus input, tenant subject scoping, publish-time authorization as the delivery decision, degraded-mode detection on control loss | Any NATS advisory affecting the deployed version; any bus-credential incident |
| Clock skew beyond the stated allowance | Token expiry, `nbf`, and staleness ceilings assume ±30 s sync; Conduit cannot correct a broken clock | `conduit doctor` checks clock sync (FR-OPS-013); the skew allowance is explicit configuration | Any incident with expiry or revocation timing anomalies |
| JWKS unavailability lockout | Fail-closed verification means an issuer outage past the cache maximum age blocks new OIDC connections | Cached-document grace window, health surfacing, and API-key or custom-authorizer modes as operator alternatives | Any production lockout incident; any change to JWKS cache policy |
| Admin-credential theft | A valid admin credential legitimately holds drain, revocation, and publish power; possession is the control | Separate port and auth (FR-ADMIN-001), TLS, full audit records with actor identity (FR-ADMIN-008), short-lived bearer guidance in OPERATIONS_TEST_PLAN | Any admin-credential incident; any addition of a mutating admin endpoint |
| Redaction false negatives | Sink redaction plus canaries cannot prove absence of every secret shape | Canary corpus grows with every incident and every new sink; structured logging narrows free-text surfaces | Any secret found in any sink; any new sink or log field |
| Dependency compromise admitted through review | Pinning and review vet versions; they cannot detect a well-hidden malicious release | Minimal dependency budget (NFR-MAINT-005), vulnerability scanning with triage (NFR-SEC-010), reproducible builds limiting post-review tampering | Any advisory against a pinned dependency; every dependency addition |

Each gate acceptance reviews this register; no row may be deleted without a recorded reason, and
no mitigation posture may be stated more strongly than its named evidence supports.

## 11. Review Cadence

This threat model must be revisited, with changes recorded in the same change set as their cause:

- At each gate acceptance (R0–R10): the accepting gate's evidence is checked against every §7
  evidence line and §9 invariant it owns; a claim whose evidence did not materialize is
  downgraded, not excused.
- At each new dependency: the §7.12 analysis and the NFR-SEC-010 review gate run before the
  dependency merges.
- At each new or superseding ADR: any decision touching an asset, actor, boundary, or
  non-guarantee updates §2–§6 and the register in the same change.
- After each security incident or discovered bypass: the incident's class gains an abuse case in
  §8, a canary or fixture in the owning test family, and a register review.
- Every 6 months regardless of activity, with the review recorded: stale assumptions in §4 (broker
  trust, clock sync, load balancer behavior) are the explicit checklist.

A threat-model change that adds an asset, boundary, actor, or residual risk must add or amend the
corresponding planned gate evidence in BUILD_PLAN and OPERATIONS_TEST_PLAN in the same change;
this document never carries a security claim its named evidence does not.
