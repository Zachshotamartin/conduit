# ADR-0007: At-Most-Once Live Delivery with Bounded Best-Effort Resume

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-RESUME-001–FR-RESUME-009, FR-CONN-009,
  gate R7

## Context

The delivery guarantee is a contract clients build on, so it must be stated
exactly and never implied to be stronger. End-to-end at-least-once delivery
would require durable per-subscriber cursors, durable event storage, ack
tracking for 50,000 connections, and redelivery machinery — a different
product (a message queue) with a different latency and memory profile.
Meanwhile, reconnects are routine (deploys, network blips, node loss), and a
client that must re-fetch full state on every reconnect makes the gateway
useless for real applications. The design must give reconnecting clients
useful continuity while being honest about what can be missed.

## Decision

The delivery contract, stated normatively:

1. **Live**: for an established subscription, each matched, authorized event
   is delivered at most once, in per-publisher field order. Events may be
   dropped only by the subscription's configured backpressure policy, and
   every policy-caused drop is counted and observable (FR-CONN-011).
2. **Resume**: each node keeps a per-(tenant, field) in-memory replay ring
   buffer of recent publish envelopes, bounded by count and bytes (defaults:
   4,096 envelopes or 16 MiB per field, whichever bounds first; horizon
   metric published). Delivered `Next` messages carry a resume position; on
   reconnect a client presents a signed resume token; the serving node
   replays buffered envelopes after the token position through the same
   publish-time authorization and filtering as live events.
3. **Gap honesty**: if the token position has fallen off the buffer horizon,
   or the token was issued for a field whose buffer the serving node lacks
   (fresh node, restarted node), the server sends a `resume_gap` extension
   message stating the covered range before live delivery begins. The client
   decides whether to re-fetch state. Conduit never silently pretends the
   replay was complete.
4. **Non-guarantees, stated**: no cross-node buffer merging, no durability
   across full-fleet restart, no exactly-once, no ordering guarantee across
   different publishers or different fields. These appear verbatim in the
   public API contract documentation.

## Alternatives Considered

- **At-least-once with durable log (JetStream/Kafka-backed cursors)**:
  rejected for v1; it changes the memory, latency, and operational profile
  fundamentally and duplicates existing stream-processing products. Recorded
  in OPEN_QUESTIONS with a reopen trigger (a paying use case that cannot
  tolerate the gap window and cannot use a durable stream alongside Conduit).
- **No resume at all**: rejected; reconnect-and-refetch on every deploy makes
  rolling upgrades user-visible fleet-wide, contradicting FR-OPS-006.
- **Server-side durable per-client cursors**: rejected; unbounded state for
  disconnected clients is an abuse vector (THREAT_MODEL §resource-exhaustion)
  and a quota headache; the signed token keeps the cursor client-side.
- **Replay from the bus**: rejected; core NATS retains nothing (ADR-0004),
  and coupling replay depth to broker configuration would make the gap
  window an infrastructure accident instead of a Conduit-owned contract.

## Consequences

Clients get continuity through routine reconnects and honesty when the gap is
real. The replay buffer is a named memory consumer in the capacity model
(R9). The resume token is a versioned, HMAC-signed public contract
(FR-RESUME-002): its format changes require version negotiation. The measured
gap window — buffer horizon in seconds at reference publish rates — is a
published R7 benchmark deliverable, so the honesty is quantified, not
rhetorical.
