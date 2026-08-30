# ADR-0004: NATS as the Reference Bus Behind a Pluggable Port

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-FAN-001–FR-FAN-010, NFR-PERF-003,
  gate R5

## Context

A mutation on any node must reach subscribers on every other node. The
inter-node transport needs: fan-out to all nodes with single-digit
millisecond added latency at the target publish rates, a partition and
reconnect story Conduit can observe and test, an operational footprint an
individual self-hosting team can run, and a shape Conduit can hide behind a
port so deterministic tests never require a broker process.

The bus carries publish envelopes and control messages (revocation,
drain announcements). It does not carry the resume replay buffer
(ADR-0007) and it is not a durability layer: the delivery contract is
at-most-once live.

## Decision

Define a Conduit-owned `Bus` port (publish, subscribe-by-subject, health,
connection-state events; exact signatures in ARCHITECTURE.md). Ship two
implementations:

- `bus/memory`: in-process, deterministic, with injectable partition, delay,
  reorder, and duplication faults. All protocol, fanout, and failure-behavior
  gates (R2–R8) are proven on this bus.
- `bus/nats`: the reference production adapter over core NATS (not
  JetStream), with tenant-scoped subjects
  (`conduit.<tenant>.pub.<field>`, `conduit.<tenant>.ctl.<kind>`), bounded
  pending limits, and explicit slow-consumer and reconnect handling mapped to
  Conduit's degraded-mode behavior.

Bus guarantees Conduit assumes, and must verify in the R5 broker-integration
suite rather than take on faith: per-publisher ordering per subject,
at-most-once delivery to each connected subscriber, drop-with-notification on
subscriber overrun.

## Alternatives Considered

- **Redis pub/sub**: rejected. Slow bus subscribers are disconnected or
  buffered invisibly server-side, cluster mode fans every publish to all
  shards for pattern subscriptions, and there is no per-subscriber overrun
  signal Conduit can surface honestly.
- **Kafka**: rejected for the hot path. Consumer-group rebalance on node
  loss stalls delivery for seconds against a p95 delivery budget in tens of
  milliseconds, partition-count choices leak into Conduit's field model, and
  the operational weight (brokers, controller quorum) is disproportionate for
  an at-most-once contract. Kafka remains a candidate for a future durable
  audit stream, recorded in OPEN_QUESTIONS.
- **In-house TCP mesh**: rejected. Membership, partition detection, and
  reconnect backoff are undifferentiated heavy lifting with real failure-mode
  risk; the pluggable port preserves the option without the cost.
- **NATS JetStream for the hot path**: rejected; persistence and ack
  tracking add latency and imply a durability guarantee the delivery
  contract deliberately does not make.

## Consequences

A NATS server becomes a deployment dependency for multi-node fleets;
single-node deployments run with `bus/memory` and no broker. The R5 gate must
include a real-broker integration suite (nightly) covering broker restart,
node kill, and induced slow-consumer drops, because `bus/memory` fault
injection cannot stand in for broker-specific behavior. Any alternative bus
adapter added later must pass the same R5 fault matrix before it may be
documented as supported.
