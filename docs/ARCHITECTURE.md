# Conduit Architecture

Document status: accepted.
Normative architecture. Last revised: 2026-08-30.

Companion specifications: [Product requirements](./PRODUCT_REQUIREMENTS.md), [Build plan](./BUILD_PLAN.md),
[Protocol conformance](./PROTOCOL_CONFORMANCE.md), [Authorization model](./AUTHORIZATION_MODEL.md),
[Operations and test plan](./OPERATIONS_TEST_PLAN.md), [Threat model](./THREAT_MODEL.md), [Benchmark
plan](./BENCHMARK_PLAN.md), [Glossary](./GLOSSARY.md), [Decision records](./decisions/).

## 1. Status and Authority

This document controls component boundaries and interface contracts (conflict rule 4 in the [documentation
index](./README.md)). It does not mint requirement IDs; every requirement cited here is defined in
PRODUCT_REQUIREMENTS.md and owned by a gate in BUILD_PLAN §19. Every component described in this document is
`planned`: nothing below is an implementation claim, and no section may be read as one until the named gate
accepts. Accepted ADRs 0001–0011 bind every decision this document elaborates; where prose here and an ADR
could be read differently, the ADR controls.

All interface listings are Go (toolchain Go 1.23, ADR-0001) and are the normative port contracts; an
implementation that must widen one changes this document in the same change set, and a widening that alters
a versioned wire contract follows NFR-COMPAT-003.

## 2. Architecture Overview

### 2.1 The two pipelines

Conduit is one process (a node, per GLOSSARY) with two pipelines meeting at the connection registry. The
subscribe path turns a client's `Subscribe` message into a registered subscription entry; the publish path
turns a mutation's publish mapping into deliveries on every matching, authorized connection in the fleet.

Subscribe path (per connection, on the node owning the socket):

```text
WebSocket upgrade (transport)
  -> protocol state machine: connection_init -> AuthMode.Authenticate
  -> connection_ack | close 4403
  -> Subscribe frame (bounded parse, FR-GQL-011)
  -> parse/validate/complexity (graphql/{ast,schema,complexity})
  -> SubscriptionAuthorizer.AuthorizeSubscribe (FR-AUTH-006)
  -> predicate compilation (filter/predicate, FR-FILT-001)
  -> registry.AddEntry -> index.Insert          [entry is now matchable]
  -> optional resume splice (resume, FR-RESUME-004)
```

Publish path (fleet-wide; every node runs the right half locally, ADR-0005):

```text
mutation resolver success (executor)      admin publish (FR-FAN-010)
        \                                /
         -> publish mapping -> Envelope (versioned, FR-FAN-002)
         -> Bus.Publish  conduit.<tenant>.pub.<field>
         ==================== bus ====================
         -> per-tenant bus consumer goroutine (every node)
         -> envelope decode + version check + dedupe window (FR-FAN-008)
         -> replay buffer append (resume positions, FR-RESUME-001)
         -> PredicateIndex.Match -> candidate set
         -> per-candidate SubscriptionAuthorizer.AuthorizePublish
            (epoch-cached, FR-AUTH-010/011)
         -> OutboundQueue.Enqueue (backpressure policy, FR-CONN-007/008)
         -> writer goroutine -> socket write
```

Delivery, per GLOSSARY, is the enqueue step; socket write completion is measured separately. The hot path is
the segment from bus consume through enqueue: it must hold zero heap allocations per delivery in steady
state (NFR-PERF-005, enforced by the R6 allocation regression test), which is why every structure it touches
— candidate sets, counter arrays, outbound messages, decision caches — is pooled or embedded.

### 2.2 Design pressures

Three quantitative pressures shape every structure in this document:

1. **50,000 connections per node** (NFR-SCALE-001, gate R9). Anything per-connection is multiplied by
   50,000, which is why timers live on one shared timing wheel (§4.3, ADR-0001) and why the goroutine
   inventory (§4.1) is closed and enumerated.
2. **64 KiB idle memory per connection** (NFR-SCALE-002, derivation in ADR-0001). The budget is allocated
   line by line in §16; a component needing more per-connection state takes it from another line in the same
   table revision, never silently.
3. **Zero-alloc delivery path** (NFR-PERF-005). Match, authorize, and enqueue run on the bus consumer at up
   to 5,000 envelopes/s (NFR-SCALE-005); per-delivery allocation converts directly into GC pressure on the
   p99 latency tail (ADR-0001 consequences).

Everything else — sharding factors, buffer sizes, pool shapes — derives from these three numbers.

### 2.3 Ports and adapters

Conduit is ports-and-adapters throughout: every external dependency (WebSocket library, GraphQL parser, NATS
client, pgx, OTel SDK) sits behind a Conduit-owned interface importable from exactly one package
(NFR-MAINT-001, CI-checked from gate R0). The full port inventory is §17. The deterministic test story
(product principle 3.6) depends on this: every port has an in-process, fault-injectable implementation, so
gates R2–R8 run without a broker, a database, or a network.

## 3. Repository Layout

Status: planned; the layout and its boundary checks are gate R0 deliverables.

```text
conduit/
├── cmd/
│   ├── conduit/               # the single binary: serve, validate, version, doctor (FR-OPS-001)
│   └── conduit-loadgen/       # load and churn generator for R6/R9 scenarios; never shipped in the image
├── internal/
│   ├── transport/             # WebSocket accept/read/write; sole importer of coder/websocket (ADR-0001)
│   ├── protocol/              # graphql-transport-ws state machine embedding; ProtocolConn (§6)
│   ├── graphql/
│   │   ├── ast/               # parse/validate wrapper; sole importer of gqlparser/v2 (ADR-0003)
│   │   ├── schema/            # SDL load, directive extraction, binding table, reload artifacts (FR-GQL-001)
│   │   ├── executor/          # Conduit-owned execution: collection, dispatch, null propagation (ADR-0003)
│   │   └── complexity/        # depth and cost accounting from @complexity (FR-GQL-008/009)
│   ├── datasource/
│   │   ├── postgres/          # pgx adapter: bound statements, pooling, timeouts (FR-GQL-004)
│   │   ├── http/              # request-template adapter with origin allowlist (FR-GQL-005)
│   │   └── function/          # UDS/loopback adapter, versioned JSON contract (FR-GQL-006)
│   ├── auth/
│   │   ├── principal/         # the normative Principal model and grant epoch (FR-AUTH-005)
│   │   ├── oidc/              # JWT validation, JWKS cache (FR-AUTH-001)
│   │   ├── apikey/            # salted-hash key store adapter (FR-AUTH-002)
│   │   ├── custom/            # operator authorizer endpoint client (FR-AUTH-003)
│   │   └── revocation/        # revocation set, epoch advance, sweep (FR-AUTH-013, ADR-0008)
│   ├── filter/
│   │   ├── predicate/         # predicate IR, compilation, type checking (FR-FILT-001..005)
│   │   ├── index/             # counting attribute index, shards, snapshots (ADR-0006)
│   │   └── oracle/            # linear scan matcher: differential oracle, benchmark baseline (FR-FILT-007)
│   ├── registry/              # per-node connection registry and subscription entries (FR-CONN-001)
│   ├── fanout/                # publish pipeline stages: dedupe, match, authorize, enqueue (§10)
│   ├── bus/                   # Bus port and envelope/control codecs (§11)
│   │   ├── memory/            # deterministic in-process bus with fault injection (ADR-0004)
│   │   └── nats/              # core-NATS adapter; sole importer of nats.go (ADR-0004)
│   ├── resume/                # replay ring buffers, resume token codec, splice (§13, ADR-0007)
│   ├── queue/                 # outbound queue, backpressure policies, writer loop (§12)
│   ├── admin/                 # admin listener, /admin/v1 handlers, drain trigger (FR-ADMIN-001..008)
│   ├── config/                # schema-validated configuration, precedence, reload (§15)
│   ├── observability/         # MetricsSink/tracing/slog wiring per ADR-0010
│   ├── platform/              # all build-tagged, OS-conditional code (ADR-0011)
│   └── clock/                 # Clock port, timing wheel, injected test clock (§4.3)
├── test/
│   ├── conformance/           # graphql-transport-ws suite vs the unmodified reference client (R2)
│   ├── hostile/               # hostile-client corpus and fuzz drivers (FR-SUB-012)
│   ├── fault/                 # bus/broker/partition fault scenarios (R5)
│   ├── load/                  # loadgen scenario definitions for R6/R9
│   └── fixtures/              # cross-version wire fixtures (NFR-COMPAT-005), SDL corpora
├── deploy/
│   ├── kubernetes/            # reference manifests and rollout integration (FR-OPS-004/006)
│   └── docker/                # OCI image build, distroless nonroot (FR-OPS-001)
└── docs/                      # this documentation set
```

### 3.1 Import boundary rules

These are NFR-MAINT-001 stated as testable constraints. Each is a CI architecture check from gate R0; a
violation fails the build, not review.

1. `github.com/coder/websocket` is imported only by `internal/transport`.
2. `github.com/vektah/gqlparser/v2` is imported only by `internal/graphql/ast`.
3. `github.com/nats-io/nats.go` is imported only by `internal/bus/nats`.
4. `github.com/jackc/pgx/v5` is imported only by `internal/datasource/postgres`.
5. `internal/transport` and `internal/protocol` never import any `internal/graphql` package; documents flow
   upward through the `OperationIntake` callback owned by the composition root in `cmd/conduit`.
6. `internal/graphql` packages never import `internal/transport`, `internal/protocol`, or `internal/queue`;
   the executor emits results, it does not write sockets.
7. Build tags and `runtime.GOOS` checks exist only in `internal/platform` (ADR-0011).
8. `internal/admin` is never imported by `internal/transport` or `internal/protocol`, and the admin mux is
   constructed on a distinct listener in `cmd/conduit`; no admin route is reachable from the client listener
   (FR-ADMIN-001, adversarially probed in R8).
9. Ports are declared in the consuming package (or `internal/bus`, `internal/datasource` as shared leaves);
   adapters import ports and never the reverse.
10. OTel and Prometheus SDK packages are imported only by `internal/observability` and `cmd/conduit`;
    hot-path packages take the `MetricsSink` port (§17) so instrumentation cannot allocate on the delivery
    path (ADR-0010).
11. No `internal` package imports anything under `test/`; `test/` may import anything.
12. Domain packages never call `time.Now`, `time.After`, or `rand` directly; they take the `Clock` port and
    seeded randomness (NFR-MAINT-006); the real clock is constructed only in `cmd/conduit`.
13. `cmd/conduit-loadgen` may import wire codecs and `internal/bus/memory` for scenario construction but
    never `internal/registry`, `internal/fanout`, or `internal/queue`: the load generator is a client, not a
    second gateway.

## 4. Process and Concurrency Model

Status: planned. The goroutine inventory and shutdown ordering are proven by R2 (connection lifecycle), R6
(bounded memory under churn), and R8 (drain).

### 4.1 Goroutine inventory

The inventory is closed: every goroutine in a serving node is on this list, and a leak-detection test (R6)
asserts the count returns to baseline after churn.

Per connection (2 × N, dominated by N = connection count):

- **reader**: blocks on `Transport.Conn.ReadMessage`, runs the protocol state machine dispatch (§6.2), and
  executes subscribe-path work up to registration. It never writes the socket.
- **writer**: blocks on `OutboundQueue.Dequeue`, serializes messages into the pooled write buffer, writes
  the socket with a deadline, and sends the final close frame (§12.4). It never reads the socket.

Shared (constant per node, independent of N):

- **accept loops**: one per client listener; performs the HTTP upgrade, subprotocol check (FR-SUB-001),
  fd-budget load shed (FR-CONN-014), and spawns the connection pair.
- **bus consumers**: one goroutine per subscribed subject group per tenant set — one for
  `conduit.<tenant>.pub.*` and one for `conduit.<tenant>.ctl.*` per served tenant (ADR-0004, FR-FAN-009).
  The publish consumer runs the entire hot path (§2.1) serially per subject, which is what makes the
  per-publisher FIFO argument (§10.4) hold.
- **timing wheel ticker**: one goroutine advancing the shared hashed wheel (§4.3) every 500 ms.
- **revocation applier**: consumes decoded control messages from the ctl consumer via a bounded channel,
  applies them to the `RevocationStore`, advances the grant epoch, and runs the affected-subscription sweep
  (ADR-0008, FR-AUTH-013).
- **metrics/observability**: the Prometheus exposition handler goroutines on the admin listener and the OTel
  batch-span exporter goroutine (ADR-0010).
- **admin server**: the admin HTTP listener's serve loop and per-request handlers (FR-ADMIN-001).
- **executor workers**: per-operation goroutines spawned by the executor for concurrent data-source dispatch
  (§7.4), bounded by per-source concurrency limits; they live for at most one operation deadline
  (FR-GQL-014).

There is no fanout worker pool in v1: matching and enqueue run inline on the bus consumer. If R9 shows a
single consumer saturating a core before NFR-SCALE-005 is met, the recorded escape hatch is sharding the
consumer by field hash — order-safe because a field never spans shards — adopted by revising this section,
not an ADR, since the ordering contract is unchanged.

### 4.2 Panic policy

- Reader and writer goroutines run under `recover`. A recovered panic logs the stack (rate-limited,
  NFR-SEC-009), increments `conduit_panic_recovered_total`, closes the connection with 1011 (internal
  error), and tears it down (§5.3). No client input can terminate the process (FR-SUB-012).
- Shared goroutines (bus consumers, timing wheel, revocation applier) run under `recover` with
  restart-and-backoff (100 ms doubling to 5 s). More than 5 recovered panics in 60 s on one shared goroutine
  fails liveness (`/healthz`, FR-ADMIN-005): supervisor restart is the honest response to a persistently
  panicking core loop (principle 3.5).
- Executor workers recover per operation: the panic becomes a redacted spec-shaped internal error on that
  field (FR-GQL-012) and is counted.
- Startup panics in `cmd/conduit` (before serving) crash loudly by design: configuration errors must fail
  fast and named (FR-OPS-002).

### 4.3 Shared timing wheel

All per-connection deadlines — init timeout (FR-SUB-003), keepalive interval (FR-SUB-007), idle timeout
(FR-CONN-002), max lifetime and its warning (FR-CONN-003), token expiry and its warning (FR-AUTH-012) — live
on one hashed hierarchical timing wheel owned by `internal/clock`, per ADR-0001's prohibition on
per-connection `time.Timer` values.

Structure: 4 levels, 512 slots per level, tick 500 ms.

- Level 0 spans 512 × 500 ms = 256 s.
- Level 1 slots each span 256 s; the level spans ≈ 36.4 h.
- Level 2 slots each span ≈ 36.4 h; the level spans ≈ 776 d.
- Level 3 covers everything beyond; with a 12 h default max lifetime (FR-CONN-003) it is populated only by
  pathological configuration.

Each slot heads an intrusive doubly-linked list of `wheelEntry` records embedded in their owning structures
(48 B each: prev/next pointers, deadline tick, callback ID, generation), so insertion allocates nothing.

Insert algorithm:

1. Compute `delta = ceil((deadline - now) / tick)`. If `delta <= 0`, set `delta = 1` (a deadline in the past
   fires on the next tick; it never fires inline on the caller's goroutine).
2. Find the smallest level `l` with `delta < 512^(l+1)`.
3. Compute the slot: `slot = (cursor[l] + delta / 512^l) mod 512`.
4. Link the entry into that slot's list under the wheel mutex; stamp the entry's generation from its handle
   so a stale cancel cannot unlink a reused entry.

Tick algorithm (on the ticker goroutine, every 500 ms of `Clock` time):

1. Advance `cursor[0]`. Detach the entire slot list under the mutex (O(1)), release the mutex, then fire
   each entry's callback.
2. When `cursor[0]` wraps to 0, advance `cursor[1]` and cascade: detach that level-1 slot and re-insert each
   entry at level 0 by the insert algorithm (its remaining delta now fits). Cascade recursively upward on
   each wrap.
3. If the ticker observes it has fallen behind (elapsed ≥ 2 ticks, from GC pause or scheduler delay), it
   processes the missed ticks in sequence before sleeping; timers fire late, never early, and lateness is
   recorded in `conduit_timing_wheel_lag_seconds`.

Callbacks must be non-blocking: a fired callback may CAS a state, enqueue on a connection's control ring
(§12.1), or post to a bounded channel; it never writes a socket, takes a registry shard lock (§5.1), or
blocks on a full queue — a reviewed constraint, with callbacks confined to one audited file per package.
`Clock.Cancel` unlinks in O(1) and returns false if the generation no longer matches. Firing accuracy is ±1
tick, acceptable against the smallest configured deadline (3 s init timeout).

### 4.4 Shutdown ordering

Drain (FR-CONN-010, gate R8) and abrupt shutdown share one ordered sequence; abrupt shutdown (SIGTERM past
the drain deadline, or SIGINT twice) skips the paced steps.

1. Stop the accept loops and fail `/readyz` immediately; in-flight upgrades complete or are refused with
   HTTP 503.
2. Publish a drain announcement on `conduit.<tenant>.ctl.drain` for each served tenant (§11.4) so fleet
   dashboards attribute the reconnect wave.
3. Pace close code 4700 (`draining`) across all connections over the drain window (default 60 s), randomized
   order at `ceil(N / window_ticks)` per wheel tick, each close carrying the jittered retry-after hint
   (FR-RESUME-009); in-flight operations get the bounded completion grace (default 5 s) before their close
   fires.
4. When the registry is empty or the deadline passes: unsubscribe bus consumers, then close the bus
   connection.
5. Flush the metrics exposition one final scrape window and flush the OTel exporter with a 2 s cap.
6. Stop the admin listener last — operators must be able to observe the drain until it ends.
7. Exit 0 on clean drain; exit with a nonzero, documented code if the deadline forced step 4 with
   connections remaining (the count is logged).

## 5. Connection Registry

Status: planned; atomicity is FR-CONN-001, owned by gate R2 and re-proven under fleet load in R6.

The registry (`internal/registry`) is the per-node owner of every local connection and its subscription
entries (ADR-0005: no cross-node registry of any kind). The subscribe path adds entries through it; the
publish path's enqueue step checks entry state before touching a queue.

### 5.1 Data structures

Sharded map: 256 shards, selected by the low 8 bits of the 64-bit `ConnID` (derived from a per-node
monotonic counter XOR-folded with the node epoch, so IDs are unguessable across restarts but cheap). Each
shard:

```go
// registryShard holds one 1/256th of the node's connections.
// Lock order: shard.mu is a leaf lock — nothing else is acquired under it.
type registryShard struct {
	mu    sync.RWMutex
	conns map[ConnID]*Connection
}
```

Per-connection struct, with the byte accounting that feeds §16 (sizes are targets asserted by a
`unsafe.Sizeof` regression test from R2):

| Field | Type | Bytes | Purpose |
| --- | --- | --- | --- |
| `id` | `ConnID` (uint64) | 8 | registry key |
| `tenant` | `TenantID` (interned uint32 + pad) | 8 | tenant scoping (ADR-0009) |
| `state` | `atomic.Uint32` | 4 | init / active / draining / closing |
| `closeOnce` | `atomic.Uint32` | 4 | teardown idempotence latch |
| `principal` | `*auth.Principal` | 8 | immutable after ack (FR-AUTH-005) |
| `conn` | `transport.Conn` (iface) | 16 | socket handle |
| `queue` | `*queue.OutboundQueue` | 8 | §12 |
| `entries` | `[]*SubscriptionEntry` header | 24 | live entries, cap grows to quota |
| `wheel` | `[4]clock.wheelEntry` | 192 | keepalive, idle, lifetime, expiry |
| `rate` | token bucket (2 × uint64) | 16 | FR-CONN-006 |
| `stats` | 8 × `atomic.Uint64` | 64 | msgs in/out, drops, ages |
| `resumeSeq` | `atomic.Uint64` | 8 | last delivered position bookkeeping |
| `negotiated` | flags + limits | 16 | read limit, keepalive interval |
| padding/embedding | | 264 | struct alignment + growth reserve |
| **total** | | **640** | budget line in §16 |

The `entries` slice starts at capacity 4 (the §16 budget) and grows toward the subscription quota (default
100, FR-CONN-005); growth beyond the idle budget is load-state memory under the 100 KiB p95 figure
(NFR-SCALE-002).

### 5.2 Registration algorithm

Connection registration (after transport accept, before `connection_ack`):

1. Allocate the `Connection` from the connection pool; stamp `id`, `state = init`; arm the init timer (3 s,
   FR-SUB-003) on the wheel.
2. Insert into the shard map under `shard.mu`. A `ConnID` collision is a programming error (monotonic
   counter): development builds panic; release builds close the new connection with 1011 and count
   `conduit_registry_id_collision_total` (expected zero, alarmed).
3. On successful `connection_init` authentication, set `principal`, CAS `state: init -> active`, cancel the
   init timer, arm keepalive, idle, lifetime, and expiry timers. A CAS failure means teardown won the race:
   registration unwinds by falling through to the teardown path and no ack is sent.
4. Quota checks (FR-CONN-004) run before the CAS in step 3: per-principal and per-tenant connection counters
   (sharded `atomic.Int64` maps keyed by interned IDs) are incremented optimistically and decremented on
   failure, so two racing connections cannot both pass a quota of one.

Subscription entry registration (reader goroutine, after `AuthorizeSubscribe` and predicate compilation
succeed):

1. Check the duplicate-ID table on the connection (small open-addressed set keyed by subscription ID hash);
   a duplicate closes 4409 (FR-SUB-005).
2. Check the per-connection subscription quota (FR-CONN-005); at quota, reject the `Subscribe` with a typed
   error — not a close.
3. Construct the `SubscriptionEntry` with `entryState = registering`.
4. Append to `conn.entries` under `shard.mu` (write lock). If `conn.state` is `closing`, abort: release the
   entry to its pool and reply nothing — the connection is dying and the close supersedes.
5. Insert into the predicate index (`PredicateIndex.Insert`, §9.5). An insert rejection (residual ceiling
   FR-FILT-006, disjunction bound FR-FILT-005) unwinds step 4 and replies with the typed error.
6. CAS `entryState: registering -> live`. From this instant the entry is matchable and deliverable. If a
   resume token accompanied the subscribe, the splice algorithm (§13.4) runs instead, using state
   `splicing`.

### 5.3 Teardown algorithm

Teardown runs exactly once per connection regardless of trigger (client close, read error, write error,
policy close, drain, revocation, panic):

1. CAS `closeOnce: 0 -> 1`. Losers return immediately; the winner proceeds and owns the whole sequence.
2. Set `state = closing` (plain store; `closeOnce` already serialized).
3. For each entry: CAS `entryState -> closing`, then `PredicateIndex.Remove`. An in-flight match on an older
   epoch snapshot (§9.4) may still yield the entry; enqueue re-checks `entryState == live` and drops
   (counted `conduit_delivery_torn_down_total`). This check — not index removal — is what satisfies "no
   delivery to a torn-down connection" (FR-CONN-001).
4. Close the outbound queue with the close reason; subsequent `Enqueue` returns `Closed` and the writer
   drains to the close frame (§12.4).
5. Remove the connection from the shard map; decrement quota counters.
6. Cancel all wheel entries (generation-checked, §4.3).
7. Return pooled resources (read buffer, entries, compiled predicates); the `Connection` itself is released
   last, after the writer exits — a two-count `sync.WaitGroup` gates it.
8. Emit the structured close record (`conn_id`, `tenant`, `close_code`, duration, counters; never payloads —
   NFR-SEC-004).

"No orphan entries after close" (FR-CONN-001) holds because step 3 iterates the registry's own entry list —
the single source of membership — and §5.2 step 4 refuses new entries once `state = closing`. Both
properties are proven under the race detector with concurrent publish/subscribe/close schedules in R2 and
re-proven at fleet scale in R6.

### 5.4 Interaction with index and queue

The registry never matches and never writes sockets. Contract with the index: every `Insert` precedes
`entryState = live` and every `Remove` follows `entryState = closing`, so the index never holds a
deliverable entry the registry does not know. Contract with the queue: created with the connection, closed
only by teardown step 4, so `Enqueue` observing `Closed` is a definitive drop, never a race that could
deliver later.

## 6. Protocol Layer

Status: planned; owned by gate R2 with FR-SUB-001 through FR-SUB-012.

Ownership boundary: PROTOCOL_CONFORMANCE.md owns the `graphql-transport-ws` state machine — every state,
transition, message shape, ambiguity decision, and the close-code table (its §6). This section owns the Go
embedding of that state machine: which goroutine runs it, how frames reach it, how its outputs reach the
socket, and the `ProtocolConn` seam other packages program against. Where this section names a close code,
it is citing that table, not defining it.

### 6.1 ProtocolConn

`internal/protocol` exposes one interface upward; the executor, fanout, and admin packages never see a raw
socket or a transport frame.

```go
// ProtocolConn is the seam between one live protocol session and the rest
// of the node. All methods are safe for concurrent use. Enqueue is the only
// delivery entry point; SendControl is the only backpressure bypass
// (FR-CONN-007); CloseWith is idempotent, first caller wins (§5.3);
// Principal is nil before connection_ack and immutable after (FR-AUTH-005).
type ProtocolConn interface {
	ConnID() ConnID   // stable, never reused within a node epoch
	Tenant() TenantID // fixed at connection_ack (ADR-0009)
	Principal() *auth.Principal
	Enqueue(msg *queue.OutboundMessage) queue.EnqueueResult
	SendControl(msg *queue.OutboundMessage) error
	CloseWith(code CloseCode, reason string)
	State() ConnState // protocol phase for admin inspection (FR-ADMIN-002)
}
```

### 6.2 Read loop

The reader goroutine runs this loop from accept to teardown. Steps 1–2 run once; steps 3–9 repeat.

1. Set the transport read limit to the inbound bound (default 512 KiB) and the library's frame limit to the
   same value, so no larger frame is ever buffered or assembled (FR-SUB-009).
2. Arm the init timer (default 3 s) on the timing wheel; expiry fires `CloseWith(4408, "connection_init
   timeout")` (FR-SUB-003).
3. Block on `Transport.Conn.ReadMessage` with the idle deadline. A read error, EOF, or deadline expiry exits
   the loop into teardown (idle expiry closes 4702 per FR-CONN-002 before the socket read fails, via the
   wheel).
4. Reject non-text frames with 4400 (FR-SUB-008); binary frames are never inspected.
5. Charge the inbound rate limiter (token bucket, default 50 msg/s burst 100, FR-CONN-006): first violation
   warns, continued violation closes with the documented 4400-class code.
6. Decode the frame into the bounded typed message set. Invalid JSON, unknown `type`, missing required
   fields, or wrong field types close 4400 with a reason that echoes no client bytes (FR-SUB-008); the
   decoder is a fuzz target from R2 (NFR-SEC-001, NFR-SEC-008).
7. Dispatch to the state table (FR-SUB-011). Illegal-transition outcomes come from the conformance table:
   any message before `connection_ack` other than `connection_init` closes 4401; a second `connection_init`
   closes 4429; `Subscribe` reusing an active ID closes 4409 (FR-SUB-005).
8. Legal transitions execute inline: `connection_init` runs the auth handoff (§14.2); `Subscribe` runs the
   subscribe path (§2.1) or single-result execution (FR-GQL-015); `Complete` removes the entry (§9.5)
   promptly (FR-SUB-006); `Ping` answers via `SendControl`; `Pong` feeds the idle timer (FR-CONN-002) and is
   otherwise ignored, solicited or not (FR-SUB-007).
9. Any handler returning a fatal protocol error exits the loop into teardown with that error's close code;
   recoverable per-subscription errors are sent as `Error` messages and the loop continues.

Because subscribe-path work runs inline on the reader, a connection cannot pipeline unbounded concurrent
subscribes: the socket is the concurrency bound and step 5 bounds the arrival rate.

### 6.3 Write path

All socket writes happen on the writer goroutine (§12.4). Messages are serialized into `OutboundMessage`
records at enqueue time, so the hot path serializes once and the writer only copies bytes. `CloseWith`
enqueues the close frame on the control ring, closes the queue, and the writer sends the frame before
closing the socket (5 s cap on the final flush).

## 7. Execution Pipeline

Status: planned; owned by gate R1 (execution, sources, limits) with the authorization hooks landing in R3.

### 7.1 Entry points

Two entries converge on one executor (FR-GQL-015 requires identical behavior):

- **HTTP**: `POST /graphql` on the client listener. The handler enforces request-body bounds before reading
  fully (FR-GQL-011), extracts credentials for per-request authentication, and runs the operation with the
  HTTP request context carrying the deadline.
- **WebSocket**: a `Subscribe` frame whose operation is a query or mutation executes through the same
  pipeline and returns one `Next` plus `Complete` (FR-GQL-015); a subscription operation diverges at step 7
  below.

### 7.2 Bounded document intake

Before any parsing (NFR-SEC-001, FR-GQL-011), in order, each with a typed rejection that allocates no AST:

1. Byte size against the document bound (default 1 MiB).
2. Token count during a single lexing pass with a counting lexer wrapper (default 20,000 tokens); the lexer
   aborts at the bound.
3. Parse depth during parsing via a depth-tracking callback in the `graphql/ast` wrapper; the parser aborts
   at the bound.

### 7.3 Parse, validate, plan

4. Parse via `graphql/ast` (the sole gqlparser importer, ADR-0003).
5. Validate: the spec rule set from gqlparser, then Conduit rules — query depth (default 15, FR-GQL-008),
   introspection policy (FR-GQL-010, disabled fields are removed from validation, not execution), and
   variable coercion against declared types (FR-GQL-013).
6. Complexity: walk the operation with `@complexity` costs and multipliers (default cost 1 per field,
   ceiling 10,000); rejection returns the computed cost in `extensions` (FR-GQL-009).

### 7.4 Executor design

The executor (`internal/graphql/executor`) is Conduit-owned (ADR-0003). For queries and mutations:

7. Collect fields per the spec's collection rules (fragments, `@skip`, `@include`, aliases; FR-GQL-002).
8. Evaluate field-level `@auth` rules per requested field (FR-AUTH-007) through the same rule table the
   subscription path uses; a denial is a spec-shaped error at that path with normal null propagation.
9. Dispatch resolvers. Each field's binding names exactly one `DataSource` (FR-GQL-007). Sibling query
   fields dispatch concurrently on executor workers bounded by a per-source semaphore (`max_concurrency`,
   default 10); mutation root fields execute serially in document order (FR-GQL-003). The operation deadline
   (default 30 s, FR-GQL-014) propagates via `context.Context`; expiry cancels in-flight source calls and
   yields the typed timeout error.
10. Complete values: coerce results, apply list and non-null propagation (a null in a non-null position
    propagates to the nearest nullable ancestor per spec), and format errors per FR-GQL-012 with redaction
    (no SQL, addresses, stack traces, or upstream bodies; canary-tested).

### 7.5 Subscription divergence

For subscription operations, steps 1–6 and subscribe-time authorization run identically, then the pipeline
diverges: nothing executes. Step 7 becomes predicate compilation (§9.2) and registry/index registration
(§5.2). The selection set is retained with the entry; at delivery time the payload is shaped by executing it
against the envelope payload — a projection, not a data-source call. Subscription payload fields that bind
data sources are rejected at SDL validation (FR-GQL-001): the delivery path must not perform I/O
(NFR-PERF-005).

### 7.6 Publish mapping on mutations

The binding configuration may attach publish mappings to mutation fields. Semantics (FR-GQL-003,
FR-FAN-001):

1. The mutation resolver runs to completion. A resolver error emits nothing.
2. For each configured mapping, in configuration order: build the attribute map by evaluating the mapping's
   attribute expressions (path selections into the resolver result plus literals), serialize the payload
   projection, and construct the envelope (§10.2) with a fresh `PublishID`.
3. An attribute expression that selects a missing path fails that mapping with a typed error attached to the
   mutation field's result (`extensions.code = PUBLISH_MAPPING_FAILED`); the mutation data is still returned
   (the write happened), and the failure is never silent (Failure UX, PRODUCT_REQUIREMENTS §8).
4. Charge the tenant publish rate limit (FR-FAN-011); rejection is the same typed, named failure.
5. `Bus.Publish` each envelope. A bus error follows the same rule: typed error in the mutation result,
   counted, never fabricated success.

## 8. Data Source Ports

Status: planned; owned by gate R1.

### 8.1 The port

```go
// DataSource resolves one bound field against one backing system. A source
// never sees raw client transport data (FR-GQL-007): SourceRequest carries
// coerced arguments and a redacted principal view only.
type DataSource interface {
	Name() string // the configured source name referenced by @source
	// Resolve honors ctx cancellation and returns typed errors from the
	// source error taxonomy (NFR-MAINT-003).
	Resolve(ctx context.Context, req *SourceRequest) (*SourceResponse, error)
	HealthCheck(ctx context.Context) error // side-effect-free readiness
	Close(ctx context.Context) error       // release pools at shutdown
}

// SourceRequest is the adapter-neutral resolution input.
type SourceRequest struct {
	Field     FieldRef          // schema coordinates of the bound field
	Tenant    TenantID          // scoping (ADR-0009)
	Args      ArgumentValues    // coerced, validated arguments
	Parent    []byte            // canonical JSON of the parent object, nil at root
	Principal PrincipalView     // subject, tenant, scopes; never credentials
	Deadline  time.Time         // operation deadline (FR-GQL-014)
}

// SourceResponse carries the resolved value as canonical JSON plus
// source-observed metadata for tracing.
type SourceResponse struct {
	Data       []byte
	SourceTime time.Duration
}
```

### 8.2 PostgreSQL adapter

`internal/datasource/postgres`, the sole pgx importer. Contract (FR-GQL-004):

- Each binding names a parameterized statement or view from configuration; parameters bind by name from
  `Args` and `PrincipalView`. No string SQL assembly exists anywhere in the package: the architecture check
  enforces it, and no package API accepts SQL text at request time.
- One `pgxpool.Pool` per source, bounded (`max_conns`, default 10), with per-operation timeouts from the
  request deadline capped by the source `timeout` (default 5 s).
- Errors map to the typed taxonomy (constraint violation, timeout, connection failure, row-shape mismatch),
  each with a stable `extensions.code`; raw driver messages never reach clients (FR-GQL-012).

### 8.3 HTTP adapter

`internal/datasource/http`. Contract (FR-GQL-005):

- Each binding names a request template: method, URL template with URL-encoded argument substitution in path
  and query positions only, header policy (static plus an allowlist of forwardable principal fields), and a
  body template for POST bindings.
- The resolved URL's origin must match the configured allowlist after template expansion — checked against
  the parsed URL, not the template, so argument injection cannot retarget the request (THREAT_MODEL §ssrf).
- Per-request timeout (default 5 s, capped by the operation deadline), bounded response size (default 4 MiB
  via limited reader), retry classification: idempotent bindings retry once on connection errors, nothing
  retries on 4xx/5xx.

### 8.4 Function adapter

`internal/datasource/function`. Contract (FR-GQL-006): the operator runs an endpoint on a Unix domain socket
or loopback HTTP address; Conduit POSTs a versioned JSON request and requires a versioned JSON response.
This wire shape is a public versioned contract (NFR-COMPAT-003).

```go
// FunctionRequest is version conduit.fn/v1 of the function-source request.
type FunctionRequest struct {
	Version    string          `json:"version"`              // always "conduit.fn/v1"
	RequestID  string          `json:"request_id"`           // ULID, for the operator's logs
	Field      string          `json:"field"`                // "Type.field" coordinates
	Tenant     string          `json:"tenant"`
	Principal  FnPrincipal     `json:"principal"`
	Args       json.RawMessage `json:"args"`                 // coerced argument object
	Parent     json.RawMessage `json:"parent,omitempty"`     // absent at root fields
	DeadlineMS int64           `json:"deadline_ms"`          // remaining budget
}

// FnPrincipal is the redacted principal view (never credentials,
// NFR-SEC-004).
type FnPrincipal struct {
	Subject string   `json:"subject"`
	Tenant  string   `json:"tenant"`
	Scopes  []string `json:"scopes"`
}

// FunctionResponse is the required reply shape. Exactly one of Data or
// Error must be set; anything else is a typed contract violation.
type FunctionResponse struct {
	Version string          `json:"version"` // must echo "conduit.fn/v1"
	Data    json.RawMessage `json:"data,omitempty"`
	Error   *FnError        `json:"error,omitempty"`
}

// FnError maps to a spec-shaped GraphQL error at the field path.
type FnError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}
```

Example exchange:

```json
{"version":"conduit.fn/v1","request_id":"01J6WZW8B2S6H8Z0Q4T1N9GX2E",
 "field":"Query.riskScore","tenant":"acme",
 "principal":{"subject":"user-7","tenant":"acme","scopes":["orders:read"]},
 "args":{"orderId":"o-123"},"deadline_ms":4200}
```

```json
{"version":"conduit.fn/v1","data":{"score":0.87,"model":"v14"}}
```

Enforcement: responses read through a limited reader (default 4 MiB); a missing or unknown `version`, both
or neither of `data`/`error`, or a timeout each yield a typed field error, counted. UDS paths must be
operator-owned files with mode checks (`conduit doctor` verifies, FR-OPS-013).

## 9. Predicate Compilation and Index

Status: planned; owned by gate R4 (FR-FILT-001 through FR-FILT-010, NFR-PERF-002), with concurrency
correctness re-proven in R5/R6 suites.

### 9.1 Predicate IR

```go
// PredicateOp enumerates the supported predicate forms (FR-FILT-002).
type PredicateOp uint8

const (
	OpEq PredicateOp = iota + 1
	OpIn         // bounded membership, len(Set) <= 100
	OpGT
	OpGTE
	OpLT
	OpLTE
	OpBetween    // Lo/Hi with inclusivity flags
	OpPresent    // boolean presence
)

// Predicate is one typed condition over a publish-envelope attribute.
// Exactly the operand fields implied by Op are populated; the compiler
// guarantees it and the index insert re-asserts it.
type Predicate struct {
	Attr   AttributeName
	Op     PredicateOp
	Scalar AttrValue   // OpEq
	Set    []AttrValue // OpIn, sorted, deduplicated
	Lo, Hi AttrValue   // ordered comparisons; zero value when unused
	LoIncl bool
	HiIncl bool
}

// ConjunctiveEntry is one indexable conjunction: the unit the counting
// index registers. K is the number of indexable predicates that must all
// hit for the entry to join the candidate set (ADR-0006).
type ConjunctiveEntry struct {
	ID       EntryID
	K        uint16
	Preds    []Predicate
	Residual bool // true: evaluated on the residual list, not counted
}

// CompiledFilter is one subscription's normalized filter: at most 8
// conjunctions (FR-FILT-005), all sharing the subscription entry.
type CompiledFilter struct {
	Conjunctions []ConjunctiveEntry
}
```

`AttrValue` is a 24-byte tagged union (type tag; int64/float64/bool inline; interned string handle) so
predicate evaluation and sub-index keys never allocate.

### 9.2 Compilation algorithm

Runs at subscribe time on the reader goroutine (FR-FILT-001). Input: the subscription field definition, its
`@filterable` argument set, and the coerced argument values.

1. For each supplied argument: if not declared `@filterable` (FR-FILT-003), and the field has no custom
   matcher hook, reject with a typed error naming the argument (an argument that cannot filter must not
   silently never-match, FR-FILT-004); with a hook, mark the filter residual.
2. Type-check each argument value against the declared attribute type; mismatch is a subscribe-time typed
   error (FR-FILT-004).
3. Lower each argument form to predicates: scalar equality to `OpEq`; list arguments to `OpIn` (reject over
   100 members with a typed error naming the bound; reject empty lists with a typed error — an empty `in`
   can never match and a silent never-match is forbidden); range input objects to the ordered ops with
   bound-order validation (`lo <= hi`, else typed error); boolean presence forms to `OpPresent`.
4. Normalize disjunctions (`or` input objects) to disjunctive normal form. If the expansion exceeds 8
   conjunctions (FR-FILT-005), reject with a typed error naming the bound. Within each conjunction,
   contradictory predicates on the same attribute (`eq: 3` with `gt: 5`) compile to a dropped conjunction;
   if all conjunctions drop, reject with a typed never-match error.
5. Sort each conjunction's predicates by attribute for deterministic evaluation and set `K` to the count of
   indexable predicates.
6. Produce the `CompiledFilter`; every failure above rejects the `Subscribe` before any registry or index
   registration (§5.2 step 5 never runs).

### 9.3 Counting-match algorithm

The index (`internal/filter/index`) is sharded per (tenant, field) (ADR-0006, ADR-0009). Each shard's
read-side state is an immutable snapshot; `Match` runs entirely against one snapshot.

Per-shard snapshot contents: hash sub-indexes per attribute for `OpEq`/`OpIn` (`AttrValue -> posting list of
(ordinal, K)`), sorted interval endpoint arrays per attribute for the ordered ops, a presence posting list
per attribute, the residual entry list, an ordinal-to-`*SubscriptionEntry` table, and `maxOrdinal`.

Match, on the bus consumer goroutine:

1. Resolve the (tenant, field) shard. Absent shard: return an empty candidate set (no subscriber has ever
   registered; not an error).
2. Load the snapshot pointer (`atomic.Pointer`, §9.4).
3. Check out a counter array from the shard's pool: `[]counterSlot` (`{gen uint32, hits uint16}`, `len >=
   maxOrdinal+1`). Bump the pool generation; a slot participates only when `slot.gen == gen`, so the array
   is never memset (counter-array pooling, ADR-0006; the NFR-PERF-005 zero-alloc obligation).
4. For each envelope attribute, in the envelope's canonical attribute order: probe the equality sub-index
   (`OpIn` was expanded to one posting per member at insert, so membership is the same probe); for each
   posting, stamp/increment its slot; if `hits` reaches the posting's `K`, append the ordinal to the
   candidate list (also pooled).
5. For each numeric or timestamp attribute with a non-empty interval sub-index: binary-search the sorted
   start-endpoint array for the prefix with `start <= v`, walk it testing end bounds and inclusivity,
   stamping/incrementing as in step 4. Worst case O(intervals with start <= v); if the R4 benchmark shows
   churn or skew degrading NFR-PERF-002, the recorded fallback is an interval tree (ADR-0006) behind the
   same sub-index seam.
6. Probe presence postings for each attribute present on the envelope.
7. Attributes referenced by a predicate but absent from the envelope contribute nothing; the conjunction
   simply never reaches `K`. This is the correct conjunction semantics and needs no special case.
8. Walk the residual list: evaluate each residual entry's full predicate slice directly against the
   envelope; matches append to the candidate list. The list is bounded (default ceiling 1,000 per field,
   FR-FILT-006) so this stays a bounded linear tail.
9. Translate candidate ordinals to `*SubscriptionEntry` via the snapshot table into the caller's pooled
   `CandidateSet`, return the counter array to the pool (generation already invalidates it), and return.

The linear scan matcher (`internal/filter/oracle`) implements the same `Matcher` port over every registered
entry; property tests assert exact candidate-set equality across the full predicate grammar (FR-FILT-007),
and the index must beat it at and above 10,000 entries (FR-FILT-010). It is never selected in production
configuration (GLOSSARY).

### 9.4 Shard and epoch concurrency

Writers (subscribe/unsubscribe) serialize on a per-shard mutex, build the next snapshot copy-on-write at
posting-list granularity (unchanged lists shared by reference), increment the epoch, and publish one atomic
pointer store. Readers never lock: a `Match` holds one snapshot for its whole run, so a publish observes any
mutation entirely-before or entirely-after — the epoch-snapshot read that makes FR-FILT-008 provable under
the race detector. Retired snapshots are reclaimed by GC; the enqueue entry-state check (§5.3 step 3) makes
delivery to a removed entry impossible even from a stale snapshot.

### 9.5 Index mutation

Insert (from §5.2 step 5):

1. Enforce the residual ceiling if `Residual` (FR-FILT-006): at the ceiling, return the typed rejection; the
   registry unwinds.
2. Assign an ordinal (smallest free, from the shard's free list, so counter arrays stay dense).
3. For each conjunction: add postings to the equality/interval/presence structures (`OpIn` expands per
   member; `OpBetween` inserts one interval; open-ended comparisons insert half-open intervals).
4. Build and publish the new snapshot under the shard mutex.

Remove (teardown §5.3, or client `Complete`):

1. Look up the ordinal by `EntryID`; a missing ID is idempotent success (teardown and `Complete` can race by
   design).
2. Delete the entry's postings, return the ordinal to the free list, rebuild the touched lists
   copy-on-write, publish the snapshot.

Churn cost is bounded by posting-list copy sizes; the R4 churn benchmark publishes insert/remove latency
alongside match latency, and FR-FILT-009 names the published metrics (entry count, residual length, shard
sizes, match and candidate-set histograms).

### 9.6 Complexity

| Operation | Cost | Notes |
| --- | --- | --- |
| Match, equality/presence probes | O(A) probes + O(P) posting visits | A = envelope attributes, P = postings hit |
| Match, interval walk | O(log E_a + M'_a) per attribute | M'_a = intervals with start <= v; tree fallback per ADR-0006 |
| Match, candidate accumulation | O(C) | C = candidate set size |
| Match, residual tail | O(R × avg preds) | R bounded at 1,000 (FR-FILT-006) |
| Insert / Remove | O(preds + touched posting-list sizes) | copy-on-write rebuild |
| Memory | O(E × avg preds) postings + O(maxOrdinal) counters per shard | budgeted in §16.2 |

Total match cost is the ADR-0006 statement O(A log N + C + R), against the oracle's O(S); NFR-PERF-002
requires p99 match ≤ 1 ms at 100,000 entries.

## 10. Fanout Pipeline

Status: planned; owned by gate R5 (FR-FAN-001 through FR-FAN-012), with the memory/backpressure interactions
proven in R6.

### 10.1 Stages and failure behavior

The publish path (§2.1) decomposes into stages with defined failure behavior at each; no stage may buffer
unboundedly or fabricate success.

| Stage | Failure | Behavior |
| --- | --- | --- |
| publish (mutation/admin) | rate limit, bus error | typed error on the mutation field or admin response (§7.6); counted |
| envelope encode | attribute expression failure | `PUBLISH_MAPPED_FAILED`-class typed error; envelope not sent |
| bus | partition, slow consumer, backlog | drop with counter + health signal, degraded mode (FR-FAN-006/007); never unbounded buffering |
| consume/decode | unknown version, malformed | envelope rejected and counted (`conduit_envelope_rejected_total{reason}`), never partially interpreted (FR-FAN-002) |
| dedupe | duplicate publish ID in window | envelope suppressed and counted (FR-FAN-008) |
| replay append | ring eviction | oldest envelope leaves the horizon; horizon metric moves (§13.1); not an error |
| match | absent shard, pool pressure | empty candidate set; pool exhaustion falls back to a counted allocation, never a dropped match |
| authorize | deny, revoked, expired epoch | delivery skipped, counted per decision class; sweep handles terminal cases (§14.3) |
| enqueue | queue full | owning subscription's backpressure policy (§12.2); control frames bypass |

### 10.2 The publish envelope

The envelope is a versioned public contract (FR-FAN-002, NFR-COMPAT-003), encoded as canonical JSON on the
bus in v1 (a binary codec is a measured R9-era decision recorded in OPEN_QUESTIONS; the struct is
codec-neutral).

```go
// Envelope is publish-envelope contract version 1.
type Envelope struct {
	Version     uint16       `json:"v"`            // contract version, 1
	Tenant      string       `json:"tenant"`       // exactly one tenant (ADR-0009)
	Field       string       `json:"field"`        // subscription field name
	PublishID   string       `json:"publish_id"`   // ULID; dedupe key with tenant+field
	Origin      string       `json:"origin"`       // publishing node ID
	Seq         uint64       `json:"seq"`          // per (origin, tenant, field) counter
	PublishedAt int64        `json:"published_at"` // unix nanoseconds at origin
	Attributes  AttributeMap `json:"attrs"`        // flat map: string -> scalar
	Payload     []byte       `json:"payload"`      // canonical JSON event body
}
```

```json
{"v":1,"tenant":"acme","field":"orderUpdated",
 "publish_id":"01J6X0M3Y9GJ2VJ7Q0B7Z8KQ4T","origin":"node-a",
 "seq":88213,"published_at":1787654321000000000,
 "attrs":{"region":"eu","total":142.5,"priority":true},
 "payload":{"orderId":"o-123","status":"SHIPPED","region":"eu"}}
```

Unknown `v` values are rejected whole and counted (FR-FAN-002). The mixed-version window (NFR-COMPAT-005,
FR-OPS-005) is honored by additive evolution — version 1 fields are never repurposed — with cross-version
fixtures in `test/fixtures` release-blocking from the first tag.

### 10.3 Dedupe window

Per (tenant, field): a hash set of `PublishID` plus a 60-slot ring of per-second buckets (window default 60
s, FR-FAN-008). Consume checks the set; a hit counts and stops. Insertion appends to the current bucket;
each tick removes the expiring bucket's IDs. Memory is `rate × window × 48 B` (§16.2). The R5 fault suite
injects redelivery and publisher retries and asserts single delivery.

### 10.4 Ordering argument

The promise is per-publisher, per-field FIFO to each connection's outbound queue, nothing more (FR-FAN-004).
It holds by composition: (1) mutation fields execute serially in document order and assign `Seq` from a
per-(origin, tenant, field) counter (FR-GQL-003); (2) the bus preserves per-publisher order per subject —
assumed from ADR-0004, verified in the R5 broker suite; (3) one consumer goroutine per subject group
processes envelopes serially through dedupe, match, authorize, enqueue (§4.1); (4) `OutboundQueue` is FIFO
and its evictions remove or replace without reordering survivors (§12.2). Each stage is order-preserving per
(origin, field) and single-threaded where order could invert, so the composition is FIFO end to end.
Cross-publisher and cross-field ordering is explicitly not promised, and documentation states it.

## 11. Bus Abstraction

Status: planned; the port and `bus/memory` are R0/R2-era substrate, the NATS adapter is owned by gate R5
(ADR-0004).

### 11.1 The Bus port

```go
// Bus is the inter-node transport port (ADR-0004). Implementations carry
// publish envelopes and control messages; they are never a durability
// layer (ADR-0007).
type Bus interface {
	// Publish sends data on subject. It returns when the message is
	// handed to the transport, not when any node receives it.
	Publish(ctx context.Context, subject Subject, data []byte) error
	// Subscribe registers h for subject (exact or trailing wildcard).
	// h is called serially per subscription, in transport arrival order.
	Subscribe(ctx context.Context, subject Subject, h MsgHandler) (BusSubscription, error)
	// Health reports the adapter's connection state for /readyz.
	Health() BusHealth
	// Events emits connection-state transitions (connected, reconnecting,
	// slow-consumer, partition-suspected) for the degraded-mode logic.
	Events() <-chan BusEvent
	// Close drains subscriptions and disconnects.
	Close(ctx context.Context) error
}

type MsgHandler func(subject Subject, data []byte)
```

### 11.2 bus/memory and fault injection

`bus/memory` is the deterministic in-process implementation all R2–R8 behavior gates run against (ADR-0004).
Its fault API is part of the test contract:

```go
// Faults configures deterministic fault injection on the memory bus.
// All methods take effect for messages published after the call.
type Faults interface {
	Partition(nodes ...NodeID)          // isolate nodes from each other
	Heal()                              // remove all partitions
	Delay(subject Subject, d time.Duration)
	Duplicate(subject Subject, n int)   // deliver n extra copies
	Reorder(subject Subject, window int) // permute within a window
	DropNext(subject Subject, n int)    // silently drop n messages
}
```

`Reorder` exists to prove consumers detect and count per-publisher order violations, not to legitimize them:
the R5 suite asserts a counted anomaly whenever a transport breaks the ADR-0004 order assumption.

### 11.3 bus/nats mapping

Core NATS, not JetStream (ADR-0004). Subjects: publish envelopes on `conduit.<tenant>.pub.<field>`, control
messages on `conduit.<tenant>.ctl.<kind>` with `kind` in `revoke`, `drain`. A node subscribes only to
subjects of tenants it serves (FR-FAN-009, ADR-0009). Adapter obligations:

- pending limits are bounded (default 64 MiB / 65,536 messages per subscription); hitting them is NATS
  slow-consumer state, surfaced as a `BusEvent`, counted as drops, and — on ctl subjects — starting the
  degraded-mode heartbeat clock (FR-AUTH-015, FR-FAN-007);
- reconnect uses jittered backoff with buffered publish disabled: a publish during disconnection fails typed
  rather than queueing invisibly;
- broker TLS and credentials are required outside explicitly acknowledged plaintext development mode
  (NFR-SEC-005).

### 11.4 Control message shapes

Versioned public contracts (NFR-COMPAT-003), canonical JSON.

```go
// Revocation is control contract v1 on conduit.<tenant>.ctl.revoke
// (ADR-0008).
type Revocation struct {
	Version      uint16 `json:"v"`
	Kind         string `json:"kind"` // principal | subject | key | scope
	ID           string `json:"id"`   // the revoked identifier
	IssuedAt     int64  `json:"issued_at"`     // unix seconds
	RevocationID string `json:"revocation_id"` // ULID, audit key
}

// DrainAnnounce is control contract v1 on conduit.<tenant>.ctl.drain.
type DrainAnnounce struct {
	Version    uint16 `json:"v"`
	Node       string `json:"node"`
	StartedAt  int64  `json:"started_at"`
	DeadlineAt int64  `json:"deadline_at"`
}
```

```json
{"v":1,"kind":"subject","id":"user-7","issued_at":1787654400,
 "revocation_id":"01J6X1Q2K8ZC1M5R7T9WYB3D6F"}
```

## 12. Outbound Queue and Backpressure

Status: planned; owned by gate R6 (FR-CONN-007 through FR-CONN-012, NFR-PERF-005) with protocol integration
proven in R2.

### 12.1 Structure

One queue per connection (FR-CONN-007), defaults 256 messages / 1 MiB.

```go
// OutboundQueue is the bounded per-connection queue of serialized
// protocol messages awaiting socket write.
type outboundQueue struct {
	mu        sync.Mutex
	ring      []slot        // 256 fixed slots: {msg *OutboundMessage, seq uint64}
	head, len int
	bytes     int64         // sum of queued payload bytes
	maxMsgs   int           // default 256
	maxBytes  int64         // default 1 MiB
	keys      map[keyRef]int // coalesce key -> ring position, per live keys only
	control   [16]*OutboundMessage // control ring: ping/pong/ack/close bypass
	ctlHead, ctlLen int
	notify    chan struct{} // writer wakeup, capacity 1
	closed    bool
	closeMsg  *OutboundMessage // the final close frame, set once
}
```

Data messages (`Next`) are evictable. `Error` and `Complete` ride the data ring for ordering but are
non-evictable, admitted above the message bound as a bounded overdraft (at most one per live entry, bounded
by the quota of 100; bytes still accounted). Control frames — ping, pong, `connection_ack`, close — use the
16-slot control ring and are never dropped (FR-CONN-007); a full control ring means the writer is wedged
past every protocol timeout, and the connection tears down with 1011, counted.

### 12.2 Enqueue algorithms

Common prologue for every enqueue, under `mu`: (a) if `closed`, return `Closed` — a definitive, counted drop
(§5.4); (b) if the message is non-evictable terminal, admit with overdraft and return `Enqueued`; (c) if
`len < maxMsgs` and `bytes+msg.bytes <= maxBytes`, append, signal `notify`, return `Enqueued`. Otherwise the
owning subscription's policy runs (FR-CONN-008):

`drop_oldest`:

1. Scan from `head` for the oldest evictable `Next` of the enqueuing subscription; evict by tombstoning the
   slot (the writer skips tombstones — O(position) scan, O(1) removal), count
   `conduit_backpressure_dropped_total{policy="drop_oldest"}`, and record the drop for the `conduit.dropped`
   notice on the subscription's next delivered message (FR-CONN-009).
2. If the byte bound still rejects the incoming message (it is larger than what one eviction freed), repeat
   step 1 until it fits or no same-subscription evictable message remains.
3. If no queued evictable message belongs to this subscription (the queue is full of other subscriptions'
   traffic), drop the incoming message instead, with the same counting and notice. The policy's scope is one
   subscription; it never steals another subscription's queue budget.
4. Append, signal, return `DroppedOldest` (or `DroppedIncoming` for step 3) so the fanout stage counts
   precisely.

`coalesce_by_key`:

1. Extract the coalesce key from the incoming payload via the field's configured key expression. Extraction
   failure — missing path, non-scalar value, payload not an object — falls back to the `drop_oldest` steps
   for this message and counts `conduit_coalesce_key_error_total` (the edge is named, not silent).
2. Probe `keys` for (subscription, key). On a hit, replace the queued message in place: the new payload
   takes the old slot and queue position, preserving FIFO for survivors (§10.4); adjust bytes, count, return
   `Coalesced`. This also resolves queue-full-with-all-same-key: replacement needs no free slot, so a full
   queue whose entries share the incoming key coalesces without eviction.
3. On a miss with the queue full: evict the oldest evictable `Next` of this subscription (removing its
   `keys` entry), as in `drop_oldest` steps 1–3, then insert the incoming message and its key.
4. If eviction found no same-subscription victim, drop the incoming message, counted, with the notice
   recorded.

`disconnect`:

1. Count `conduit_backpressure_dropped_total{policy="disconnect"}` for the rejected message (it is not
   delivered).
2. Invoke `CloseWith(4704, "slow consumer")`; teardown (§5.3) closes the queue and the close frame travels
   on the control ring, so the full data ring cannot block the close (edge: close never enqueues data).
3. Return `Disconnecting`; subsequent enqueues observe `closed`.

### 12.3 Slow-consumer detection

Before any policy fires, threshold crossings emit a structured `slow_consumer_warning` event (FR-CONN-012):
depth ≥ 75% of `maxMsgs`, bytes ≥ 75% of `maxBytes`, or oldest-message age ≥ 5 s (checked by the writer via
`Clock`, not a per-connection timer); at most one event per connection per 30 s window (NFR-SEC-009).

### 12.4 Writer loop

1. Block on `notify` (and context cancellation from teardown).
2. Drain the control ring first, completely; control frames outrank data.
3. Pop the next non-tombstone data slot; check out a pooled 16 KiB write buffer (ADR-0001); write with the
   write deadline (default 10 s).
4. A write error or deadline expiry counts, records the error as the close reason, and exits into teardown —
   no retry; the socket is dead or the client unrecoverably slow.
5. Return the buffer to the pool; update depth/bytes/age gauges.
6. On `closed` with the data ring drained (or on teardown demanding immediate exit): send `closeMsg` with a
   5 s cap, close the socket, release the writer's connection reference (§5.3 step 7).

## 13. Replay Buffer and Resume

Status: planned; owned by gate R7 (FR-RESUME-001 through FR-RESUME-008; FR-RESUME-009 measured in R9), per
ADR-0007.

### 13.1 Ring buffer design

One replay ring per (tenant, field) per node, fed by the bus consumer after dedupe (§2.1), bounded by count
and bytes — defaults 4,096 envelopes or 16 MiB, whichever bounds first (ADR-0007). The ring stores
references to decoded envelopes (immutable after decode, so no copy) plus assigned positions; append evicts
from the tail until both bounds hold. Horizon age is a published metric (FR-RESUME-003). Replay iterates
under a seqlock-stamped head/tail snapshot so it never blocks append; a reader observing a stamp change
restarts its bounded scan.

### 13.2 Position scheme

A position is `(node epoch, per-(tenant, field) sequence)`. The node epoch is a 64-bit value minted at
process start (boot nanoseconds folded with random bits); the sequence is assigned at ring append, monotonic
within the epoch (FR-RESUME-001). Positions are meaningful only to the minting node: a token presented to a
different node, or after restart, is an epoch mismatch resolved by the gap rules (§13.5) — the honesty
ADR-0005 and ADR-0007 require. Every delivered `Next` carries its position and encoded token in
`extensions.conduit`.

### 13.3 Resume token

Opaque to clients, versioned, HMAC-signed, ≤ 512 bytes (FR-RESUME-002, NFR-SEC-007). Byte layout, version 1:

| Offset | Len | Field | Encoding |
| --- | --- | --- | --- |
| 0 | 1 | token version | `0x01` |
| 1 | 1 | key ID | active HMAC key identifier |
| 2 | 1 | tenant length T | uint8, 1–64 |
| 3 | T | tenant | UTF-8 |
| 3+T | 1 | field length F | uint8, 1–128 |
| 4+T | F | field | UTF-8 |
| 4+T+F | 8 | node epoch | big-endian uint64 |
| 12+T+F | 8 | sequence | big-endian uint64 |
| 20+T+F | 8 | issued at | big-endian int64, unix seconds |
| 28+T+F | 32 | HMAC-SHA-256 | over bytes 0 .. 27+T+F |

Maximum size 252 bytes, under the 512-byte bound with version headroom. Verification is constant-time
compare (NFR-SEC-007). The codec holds a keyring (`key ID -> secret`) with one active signing key; rotation
adds a new key and retires old keys after the token maximum age, so every unexpired token stays verifiable.
A token failing signature, tenant, field, size, structure, or age checks is rejected typed: the subscription
proceeds fresh with a `resume_rejected` notice and the attempt is logged (FR-RESUME-007).

### 13.4 Resume splice algorithm

Runs when a `Subscribe` carries a valid token (`Subscribe.payload.extensions.conduit.resume`); the step 3/4
ordering is the correctness core.

1. Decode and verify the token (§13.3); invalid tokens follow FR-RESUME-007 and continue as a fresh
   subscribe at §5.2.
2. AuthorizeSubscribe, compile predicates (§9.2) — identical to fresh.
3. Register the entry (§5.2 steps 1–5) with `entryState = splicing`. Live matches for a splicing entry
   divert into a bounded splice staging buffer on the entry (capacity 256 messages; overflow is handled in
   step 7).
4. Read the ring's current tail sequence `T` and horizon head `H` (one seqlock read). Every envelope with
   `seq <= T` is (or was) in the ring; every envelope with `seq > T` arrives after step 3 and is therefore
   diverted to staging. This is the no-gap invariant.
5. Determine the replay start: if the token epoch differs from the node epoch, or `token.seq + 1 < H`, emit
   the `resume_gap` notice first, stating the covered range `[max(H, token.seq+1), T]` and the reason class
   (`horizon_passed`, `epoch_mismatch`, `no_coverage`) (FR-RESUME-005); start at `max(H, token.seq + 1)`. A
   token for a field absent from the current schema completes the subscription with a typed error (schema
   changed; FR-OPS-003 semantics).
6. Replay: iterate ring envelopes in `[start, T]` in sequence order; each passes the entry's predicates and
   `AuthorizePublish` exactly as live traffic (FR-RESUME-004); matched, authorized envelopes enqueue with
   their positions. An authorization denial skips and counts, same as live. Queue-full during replay applies
   the entry's backpressure policy — replay does not get to violate FR-CONN-007.
7. Splice: drain the staging buffer in arrival order, discarding every envelope with `seq <= T` (already
   covered by replay — the no-duplicate half), enqueueing every envelope with `seq > T` (the no-gap half).
   If staging overflowed during replay, the overflow range `(T, first-retained-seq)` is reported as an
   additional `resume_gap` notice — bounded honesty rather than unbounded buffering.
8. CAS `entryState: splicing -> live`; subsequent matches enqueue directly. If teardown raced any step, the
   CAS fails against `closing` and the splice unwinds through normal teardown (§5.3).

No-duplicate/no-gap at the cutover: replay covers exactly `[start, T]`; staging receives exactly the matches
with `seq > T` (step 4's ordering means no envelope can be missed by both the ring read and staging); the
step-7 filter removes exactly the overlap `<= T`. The union is contiguous from `start`, the delivered
intersection empty. Proven deterministically in R7 with `bus/memory` schedules interleaving append, replay,
and staging at every boundary (FR-RESUME-004/006).

### 13.5 resume_gap determination

`resume_gap` is emitted in exactly three named cases: the token predates the horizon (`horizon_passed`), the
token's epoch is not this node's (`epoch_mismatch` — restart or different node, ADR-0005), or the field's
ring does not exist here (`no_coverage`). The gap window (GLOSSARY) also includes policy-caused drops and
partition-healing losses, reported by their own counters and notices (FR-CONN-009, FR-FAN-006); the public
contract documentation states the union honestly, and the measured horizon is an R7 benchmark deliverable
(FR-RESUME-008).

## 14. Auth Integration Points

Status: planned; owned by gate R3 (fleet SLO measurements in R5). AUTHORIZATION_MODEL.md owns semantics —
modes, rules, decisions, tenancy, bypass-resistance; this section owns placement and the Go seams.

### 14.1 The seams

Three ports, defined once in §17: `AuthMode` (one mode authenticates a given connection, FR-AUTH-004),
`SubscriptionAuthorizer` (the pair of named enforcement points, FR-AUTH-006 and FR-AUTH-010;
`AuthorizePublish` runs on the hot path and must not allocate in steady state, NFR-PERF-005), and
`RevocationStore` (the node-local revocation set, ADR-0008; `Apply` is idempotent, `Epoch` advances on every
applied revocation or policy reload, `Sweep` expires entries past their grant lifetimes plus slack).

### 14.2 Placement

- **`AuthMode.Authenticate`** runs at `connection_init` on the reader (§6.2 step 8), before
  `connection_ack`; failure closes 4403 with no information about which check failed (FR-AUTH-004,
  FR-AUTH-018).
- **`AuthorizeSubscribe`** runs after validation and before any registry or index registration (§5.2;
  FR-AUTH-006).
- **`AuthorizePublish`** runs per candidate on the bus consumer, between match and enqueue (§2.1), against
  the current grant state and the concrete envelope (FR-AUTH-010). The per-entry decision cache is 16 bytes
  embedded in the entry — `{epoch GrantEpoch, decision uint8}` — valid only while `epoch ==
  RevocationStore.Epoch()` (FR-AUTH-011); an epoch advance invalidates every cached decision
  fleet-node-locally in one atomic load comparison, with no sweep required for correctness.
- **Token expiry** is a timing-wheel entry per connection (§4.3): the warning ping fires at expiry minus 60
  s with `{"conduit":{"expires_in_ms":n}}`; at expiry, publish-time checks fail closed immediately
  (epoch-independent expiry comparison in `AuthorizePublish`), live subscriptions receive typed
  `TOKEN_EXPIRED` errors, and the connection closes 4403 (FR-AUTH-012, ADR-0008).

### 14.3 Revocation flow

The ctl consumer decodes `Revocation` messages (§11.4) into the applier's bounded channel (§4.1). The
applier: `Apply` to the store, advance the epoch, then sweep the registry — `Error` (`GRANT_REVOKED`) per
affected live subscription, 4403 close for fully revoked principals (FR-AUTH-013). Correctness does not
depend on the sweep: the epoch advance already fails every subsequent `AuthorizePublish` closed — the
property the R3 revoke-then-publish suite proves (NFR-SEC-002). Propagation p99 ≤ 2 s is measured fleet-wide
in R5 (FR-AUTH-014). Loss of the ctl subject beyond the heartbeat timeout (default 10 s) enters degraded
mode under the configured policy — `fail_closed` default, `fail_open_bounded` by explicit audited choice —
visible in `/readyz` and logs (FR-AUTH-015/016).

## 15. Configuration Model

Status: planned; validation and precedence are owned by gate R1 (FR-OPS-002), atomic reload by R8
(FR-OPS-003).

### 15.1 Structure and precedence

One typed tree in `internal/config`, decoded strictly (unknown keys are errors, never warnings):

```go
// Config is the root of the effective configuration.
type Config struct {
	Server        ServerConfig        // client listener, TLS/trusted_proxy (FR-CONN-013)
	Admin         AdminConfig         // admin listener, auth (FR-ADMIN-001)
	Tenants       []TenantConfig      // tenancy mode, per-tenant schema refs (ADR-0009)
	Auth          AuthConfig          // modes, degraded-mode policy (FR-AUTH-004/015)
	Schema        SchemaConfig        // SDL paths, binding config, publish mappings
	Sources       []SourceConfig      // postgres | http | function bindings
	Bus           BusConfig           // memory | nats, subjects, pending limits
	Limits        LimitsConfig        // quotas, rate limits, document bounds, deadlines
	Backpressure  BackpressureConfig  // queue defaults 256 msgs / 1 MiB (FR-CONN-007)
	Resume        ResumeConfig        // ring bounds, token keys and max age
	Observability ObservabilityConfig // metrics, tracing sampling, log level
}
```

Precedence (PRODUCT_REQUIREMENTS §5.3): built-in defaults < YAML file < `CONDUIT_*` environment < flags. The
merged, redacted effective configuration is served by `/admin/v1/config` with its hash, and the hash is
logged at startup (FR-ADMIN-006).

### 15.2 Validation phases

1. **Decode**: strict YAML/env/flag decoding into the tree; type errors name key, source, and expectation.
2. **Semantic**: per-field range and enum checks; secret presence checks (NFR-SEC-005 acknowledgments;
   `auth.mode: none` requires `development_acknowledged: true`, FR-AUTH-004).
3. **Cross-reference**: SDL parses and validates; every `@source` names a configured source; every `@auth`
   rule is defined (FR-AUTH-008); every `@filterable` type is indexable (FR-FILT-003); every `@backpressure`
   coalesce key expression compiles; publish mappings type-check against mutation results (FR-GQL-001).
4. **Environment probes** are deliberately not startup checks; they live in `conduit doctor` (FR-OPS-013) so
   a slow bus cannot block a restart.

`conduit validate` runs phases 1–3 identically and exits nonzero on any error (FR-OPS-002).

### 15.3 Atomic schema reload

SIGHUP or `/admin/v1/config` reload trigger (FR-OPS-003); scope is SDL, bindings, and auth rules — listener,
bus, and tenancy changes require restart, stated in the config reference.

1. Load and validate the candidate set (phases 1–3) off the serving path. Any failure: log with
   file/line/rule, return the admin error, leave the old artifacts serving. Nothing is partially applied.
2. Build complete new artifacts beside the old: parsed schema, binding table, complexity table, auth rule
   table, filterable attribute sets.
3. Swap one atomic pointer to the artifact bundle. Operations beginning after the swap use the new bundle;
   in-flight operations complete on the old bundle, which is released when its refcount drains.
4. Diff subscription-relevant surfaces: entries on removed or filter-incompatible fields are completed with
   a typed `SCHEMA_RELOADED` error and removed (§9.5); compatible entries continue untouched.
5. If auth rules changed, advance the grant epoch (FR-AUTH-011): every cached publish-time decision
   revalidates under the new rules.
6. Emit the audit record and the new config hash (FR-ADMIN-008).

## 16. Memory Budget

Status: planned; the idle figure is asserted by R6 accounting tests and measured as RSS delta per 10,000
connections in R9 (NFR-SCALE-002, ADR-0001 — measurement, not struct arithmetic, is the claim).

### 16.1 Per-connection idle budget

| Component | Bytes | Source |
| --- | --- | --- |
| reader goroutine stack | 8,192 | ADR-0001; stack-growth regression test |
| writer goroutine stack | 8,192 | ADR-0001 |
| read buffer (pooled, held while parked in read) | 8,192 | ADR-0001 default |
| write buffer (pooled; idle residency amortized at 1 per 8 conns) | 2,048 | 16 KiB pooled, checked out only during writes |
| TLS record buffers (crypto/tls, amortized) | 12,288 | local TLS termination (FR-CONN-013); zero in trusted_proxy mode |
| `Connection` struct | 640 | §5.1 table |
| protocol session state (state table, dup-ID set) | 384 | §6 |
| principal (bounded claims map) | 1,536 | FR-AUTH-005; claims bounded at decode |
| subscription entries, idle budget 4 × 640 | 2,560 | growth to quota is load-state (NFR-SCALE-002 p95 figure) |
| compiled predicates, 4 × 512 | 2,048 | §9.1 IR, pooled |
| outbound queue struct + 256-slot ring + key map header | 4,288 | §12.1; queued payload bytes are load-state |
| timing wheel entries (4 × 48, embedded) | 192 | §4.3 |
| registry shard map entry + ID interning | 768 | §5.1 |
| per-connection metric atomics | 256 | ADR-0010 hot path budget |
| resume position bookkeeping | 256 | §13.2 |
| allocator rounding and headroom | 8,192 | size-class waste, GC floats |
| **Total** | **60,032** | **≤ 65,536 (64 KiB), margin 5,504** |

Rules: a component needing more takes it from another line in the same table revision; queued messages,
grown entry slices, and splice staging are load-state under the 100 KiB p95 budget; the table is arithmetic
guidance — the R9 measurement is the claim.

### 16.2 Node-level consumers

| Consumer | Formula | At reference values |
| --- | --- | --- |
| predicate index | E × ≈224 B (entry + postings) + shard overhead | ≈ 22 MiB at E = 100,000 (NFR-SCALE-004) |
| counter-array pools | Σ per shard maxOrdinal × 8 B | ≈ 0.8 MiB at 100,000 dense ordinals |
| replay buffers | F × min(4,096 × avg envelope, 16 MiB) | 16 MiB per hot field, worst case (ADR-0007); F capped by capacity model (FR-OPS-010) |
| dedupe windows | publish rate × 60 s × 48 B | ≈ 14 MiB at 5,000/s (NFR-SCALE-005) |
| revocation set | V × 64 B | 6.4 MiB at V = 100,000; size metric alarmed (ADR-0008) |
| buffer pools (read/write) | pool residency, bounded by high-water marks | published gauges |
| bus pending buffers | configured pending limits | ≤ 64 MiB per subscription (§11.3) |

These consumers plus the 12 GiB connection budget must fit the ADR-0001 derivation (16 GiB node, 4 GiB
reserved); every coefficient here becomes a capacity-model row traceable to an R9 benchmark row
(FR-OPS-010).

## 17. Core Interfaces Appendix

Status: planned. This is the consolidated port inventory; each interface is normative and consistent with
its owning section. Supporting types (`ConnID`, `TenantID`, `Subject`, `Decision`, result enums) are defined
in their owning packages.

```go
// Transport owns WebSocket I/O; sole importer of coder/websocket (§3.1).
// Accept rejects non-graphql-transport-ws subprotocols pre-handshake
// (FR-SUB-001).
type Transport interface {
	Accept(w http.ResponseWriter, r *http.Request, opts AcceptOptions) (Conn, error)
}

// Conn is one accepted socket (§6).
type Conn interface {
	ReadMessage(ctx context.Context) (kind MessageKind, data []byte, err error)
	WriteMessage(ctx context.Context, kind MessageKind, data []byte) error
	SetReadLimit(bytes int64)
	Close(code CloseCode, reason string) error
}

// ProtocolConn is the protocol session seam (§6.1).
type ProtocolConn interface {
	ConnID() ConnID
	Tenant() TenantID
	Principal() *auth.Principal
	Enqueue(msg *queue.OutboundMessage) queue.EnqueueResult
	SendControl(msg *queue.OutboundMessage) error
	CloseWith(code CloseCode, reason string)
	State() ConnState
}

// AuthMode authenticates connection credentials (§14.1).
type AuthMode interface {
	Name() string
	Authenticate(ctx context.Context, cred Credentials) (*Principal, error)
}

// SubscriptionAuthorizer holds both named enforcement points (§14.1).
// AuthorizePublish is hot-path and allocation-free in steady state
// (NFR-PERF-005).
type SubscriptionAuthorizer interface {
	AuthorizeSubscribe(ctx context.Context, p *Principal, field FieldRef, args ArgumentValues) (Decision, error)
	AuthorizePublish(entry *SubscriptionEntry, env *Envelope, epoch GrantEpoch) Decision
}

// RevocationStore is the node-local revocation set (§14.1, ADR-0008).
// Apply is idempotent (false if already applied); Epoch advances on every
// applied revocation or policy reload; Sweep expires aged entries.
type RevocationStore interface {
	Apply(rev Revocation) bool
	IsRevoked(p *Principal) bool
	Epoch() GrantEpoch
	Sweep(now time.Time) int
	Len() int
}

// DataSource resolves bound fields (§8.1).
type DataSource interface {
	Name() string
	Resolve(ctx context.Context, req *SourceRequest) (*SourceResponse, error)
	HealthCheck(ctx context.Context) error
	Close(ctx context.Context) error
}

// Matcher returns the candidate set for an envelope; implemented by the
// index and by the linear scan oracle (§9.3).
type Matcher interface {
	Match(env *Envelope, out *CandidateSet) error
}

// PredicateIndex is the counting attribute index (§9).
type PredicateIndex interface {
	Matcher
	Insert(f *CompiledFilter, e *SubscriptionEntry) error
	Remove(id EntryID) error
	Stats() IndexStats
}

// Bus is the inter-node transport (§11.1).
type Bus interface {
	Publish(ctx context.Context, subject Subject, data []byte) error
	Subscribe(ctx context.Context, subject Subject, h MsgHandler) (BusSubscription, error)
	Health() BusHealth
	Events() <-chan BusEvent
	Close(ctx context.Context) error
}

// ReplayBuffer is one per-(tenant, field) replay ring (§13.1). Bounds
// returns the horizon head and tail sequences; Replay visits [from, to]
// in sequence order, and visit returning false stops early.
type ReplayBuffer interface {
	Append(env *Envelope) Position
	Bounds() (head, tail uint64, ok bool)
	Replay(from, to uint64, visit func(*Envelope, Position) bool) error
	HorizonAge(now time.Time) time.Duration
}

// ResumeCodec signs and verifies resume tokens (§13.3). Decode verifies
// structure, signature (constant-time), and maximum age, returning typed
// errors per FR-RESUME-007.
type ResumeCodec interface {
	Encode(pos ResumePosition) ([]byte, error)
	Decode(token []byte, now time.Time) (ResumePosition, error)
	Rotate(active KeyID, keys map[KeyID][]byte) error
}

// OutboundQueue is the bounded per-connection queue (§12).
type OutboundQueue interface {
	Enqueue(msg *OutboundMessage, pol BackpressurePolicy) EnqueueResult
	EnqueueControl(msg *OutboundMessage) error
	Dequeue(ctx context.Context) (*OutboundMessage, error)
	Depth() (msgs int, bytes int64)
	OldestAge(now time.Time) time.Duration
	Close(reason error)
}

// ConnectionRegistry owns local connections and entries (§5). ForEach
// visits a tenant's live connections (revocation sweep, drain pacing);
// visit returning false stops.
type ConnectionRegistry interface {
	Register(c *Connection) error
	Deregister(id ConnID) (*Connection, error)
	AddEntry(id ConnID, e *SubscriptionEntry) error
	RemoveEntry(id ConnID, sub SubID) (*SubscriptionEntry, error)
	Get(id ConnID) (*Connection, bool)
	ForEach(tenant TenantID, visit func(*Connection) bool)
	Len() int
}

// Clock is injected time: wall reads and wheel scheduling (§4.3,
// NFR-MAINT-006). Schedule fires fn on the timing wheel; fn must be
// non-blocking.
type Clock interface {
	Now() time.Time
	Schedule(d time.Duration, fn func(now time.Time)) TimerHandle
	Cancel(h TimerHandle) bool
}

// MetricsSink is the allocation-free instrumentation seam (ADR-0010).
// Instruments are resolved once at construction, never per event.
type MetricsSink interface {
	Counter(name MetricName, labels ...Label) Counter
	Gauge(name MetricName, labels ...Label) Gauge
	Histogram(name MetricName, buckets []float64, labels ...Label) Histogram
}
```

## 18. Deferrals and Requirements Referenced

### 18.1 What this document elaborates

This document is the implementation-depth elaboration of: FR-GQL (§7, §8; gate R1, with R3 closing
FR-GQL-010/012), FR-SUB embedding (§6; gate R2; the state machine itself is PROTOCOL_CONFORMANCE.md),
FR-AUTH placement (§14; gates R3/R5; semantics in AUTHORIZATION_MODEL.md), FR-FILT (§9; gate R4), FR-FAN
(§10, §11; gate R5), FR-CONN (§5, §12; gates R2/R6/R8), FR-RESUME (§13; gates R7/R9), FR-ADMIN and FR-OPS
surfaces it touches (§3, §4.4, §15; gates R8/R10), NFR-PERF and NFR-SCALE structures (§2.2, §16; gates
R4/R6/R9), NFR-SEC enforcement placement (§6.2, §13.3, §14; gates R2/R3/R5/R6), NFR-COMPAT versioned shapes
(§8.4, §10.2, §11.4, §13.3; gates R2/R7/R10), and NFR-MAINT boundaries (§3.1; gate R0).

### 18.2 Explicit deferrals

Deferred, per the status vocabulary, and forbidden from being used to claim any gate complete:

- **Binary envelope codec on the bus**: deferred; v1 is canonical JSON (§10.2); reopening is an R9
  measurement decision recorded in OPEN_QUESTIONS.
- **Sharded bus consumers**: deferred escape hatch (§4.1); adopted only if R9 shows single-consumer
  saturation, preserving the §10.4 ordering argument.
- **Interval tree sub-index**: deferred pending the R4 endpoint-churn benchmark (§9.3, ADR-0006).
- **In-band token refresh**: deferred by ADR-0008; refresh is reconnect with resume until the recorded
  reopen trigger fires.
- **Durable delivery, cross-node replay merging, additional transports, federation, Windows-native builds**:
  out of scope for 1.0 per PRODUCT_REQUIREMENTS §4.3 and ADRs 0002, 0007, 0011; listed here so no section of
  this document is read as implying them.

Every structure above remains `planned` until its owning gate's evidence matrix in BUILD_PLAN accepts; this
document changes in the same change set as any interface it defines.
