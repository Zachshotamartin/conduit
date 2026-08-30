# ADR-0001: Go Runtime for Connection Density

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: NFR-SCALE-001, NFR-SCALE-002, NFR-PERF-001,
  gate R9

## Context

Conduit must hold 50,000 concurrent WebSocket connections on a single node
with a published memory-per-connection figure and publish-to-delivery latency
percentiles (NFR-SCALE-001, NFR-PERF-001). The runtime choice dominates three
costs: per-connection memory (stacks, buffers, timers), tail latency under
garbage collection or scheduler pressure, and the engineering cost of a
correct concurrent fanout pipeline. The choice must be argued against the
scale target specifically, not against general preference.

Per-connection memory budget derivation at 50,000 connections: a 16 GiB node
reserving 4 GiB for the predicate index, replay buffers, bus client, runtime,
and headroom leaves 12 GiB for connections, or 251 KiB per connection at the
ceiling. The target is set far below the ceiling at 64 KiB idle and 100 KiB
p95 under load so a fleet claim survives payload variance and GC overhead.

## Decision

Conduit is implemented in Go (minimum toolchain Go 1.23, pinned in
`go.mod` and CI). One goroutine pair (reader, writer) per connection, with
per-connection state kept in pooled, explicitly sized structures. WebSocket
I/O uses a maintained minimal library (`coder/websocket`) behind a
Conduit-owned `Transport` interface so the library never appears outside the
transport package.

Memory discipline rules bound to this decision:

- goroutine stacks start at the runtime default (8 KiB in Go 1.23); handler
  code paths must not recurse or allocate large stack frames that force
  growth; a stack-growth regression test guards the read/write loops;
- read and write buffers are pooled and bounded (default 8 KiB read, 16 KiB
  write) and returned on connection close;
- per-connection timers (keepalive, idle, token expiry, max lifetime) use a
  shared timing wheel, not four `time.Timer` allocations per connection;
- the memory-per-connection figure is measured as RSS delta per 10,000
  connections in the R9 benchmark, not computed from struct sizes.

## Alternatives Considered

- **Node.js**: rejected. A single event loop cannot use the cores a 50k-node
  requires without multi-process clustering, which forces the cross-node
  fanout problem inside one machine; V8 heap pressure at 50k sockets with
  per-connection closures produces GC pauses directly on the delivery path.
- **JVM (Netty)**: technically capable of the target, rejected on footprint
  and operations: baseline heap plus direct-buffer tuning raises the
  per-connection budget floor, GC tuning becomes a deployment prerequisite,
  and the container image and startup profile conflict with the drain and
  rolling-deploy targets in FR-OPS.
- **Rust (tokio)**: best theoretical memory per connection, rejected on
  delivery risk: the GraphQL gateway ecosystem in Rust would force more
  from-scratch protocol and execution code, and the project's differentiating
  risk is in matching, authorization, and fanout semantics, not in shaving
  the last 20 KiB per connection. Revisit only if R9 measurement misses the
  memory target by more than 2x with the tuning rules above applied.
- **Elixir/BEAM**: excellent connection-density story, rejected because the
  set requires real typed interfaces as normative artifacts, the team's
  operational experience is in Go tooling, and single-binary distribution
  (FR-OPS-001) is materially simpler in Go.

## Consequences

GC pauses are a named risk on the delivery latency tail: the R9 benchmark
must record p99 with GC activity visible (`GODEBUG=gctrace` capture attached
to the run), and `GOGC`/`GOMEMLIMIT` values are part of the published
benchmark configuration. Goroutine-per-connection makes the concurrency model
reviewable but makes accidental per-connection allocation cheap to introduce;
the build gates include an allocation regression test on the delivery path
(R6). All public interfaces in this documentation set are Go signatures.
