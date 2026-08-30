# Conduit Glossary

Document status: accepted.
Normative controlled vocabulary. Last revised: 2026-08-30.

Controlled definitions. Documents in this set must use these terms with these
meanings and no others. A new term used normatively in any document must be
added here in the same change.

- **Admin surface**: the authenticated, versioned HTTP API and CLI that expose
  operational state (connections, subscriptions, drain, quotas). Never exposed
  on the client listener port.
- **At-most-once (live)**: the delivery contract for an established
  subscription: a matched event is delivered zero or one times to that
  connection; never duplicated. Defined by ADR-0007.
- **Backpressure policy**: the configured per-subscription-field behavior when
  a connection's outbound queue is full: `drop_oldest`, `coalesce_by_key`, or
  `disconnect` (close code 4704). Defined in PRODUCT_REQUIREMENTS §7.6.
- **Bus**: the pluggable inter-node transport that carries publish envelopes
  and control messages between gateway nodes. The reference production bus is
  NATS (ADR-0004); the deterministic test bus is in-process.
- **Bus partition**: a condition in which one or more nodes cannot exchange
  bus messages with the rest of the fleet while still serving local clients.
- **Candidate set**: the set of subscription entries returned by the predicate
  index for a publish envelope before publish-time authorization filtering.
- **Close code**: a WebSocket close status code. Conduit uses the
  `graphql-transport-ws` codes (4400–4500 range) plus the Conduit-assigned
  codes enumerated in PROTOCOL_CONFORMANCE §6.
- **Coalesce key**: the value extracted from an event payload by a configured
  key expression, used by the `coalesce_by_key` backpressure policy to replace
  a queued event with a newer event carrying the same key.
- **Connection**: one accepted WebSocket on one node, from TCP accept to
  close, including its protocol state machine, principal, quota accounting,
  and outbound queue.
- **Connection registry**: the per-node in-memory structure that owns all
  local connections and their subscriptions. There is no cross-node shared
  registry (ADR-0005).
- **Delivery**: the enqueue of a `Next` message onto a connection's outbound
  queue after publish-time authorization and filtering. Socket write completion
  is a separate, measured step.
- **Drain**: the graceful shutdown mode in which a node stops accepting new
  connections, notifies clients with close code 4700 on a paced schedule, and
  exits before the deploy deadline.
- **Event**: one published payload for one subscription field, wrapped in a
  publish envelope on the bus.
- **Fanout**: the process of matching one publish envelope against subscription
  entries fleet-wide and enqueueing deliveries on every matching, authorized
  connection.
- **Gap window**: the interval of events a resuming client can have missed and
  can never recover: events beyond the replay buffer horizon, events dropped
  by a backpressure policy, and events published during bus partition healing.
  Always reported honestly via `resume_gap`; never silently absorbed.
- **Gate**: a named phase (R0–R10) in BUILD_PLAN.md whose acceptance evidence
  unlocks exactly one product capability claim.
- **Grant**: an authorization fact permitting a principal an action on a
  resource, as evaluated by the configured auth mode and policy hooks.
- **Linear scan matcher**: the reference matcher that evaluates every
  registered subscription entry against a publish envelope. It is the
  differential oracle for the predicate index and the benchmark baseline; it
  is never the accepted production matcher (ADR-0006).
- **Node**: one Conduit process instance in a fleet.
- **Operation**: a GraphQL query, mutation, or subscription document plus
  variables submitted by a client.
- **Outbound queue**: the bounded per-connection queue of serialized protocol
  messages awaiting socket write.
- **Predicate**: a compiled, typed condition over publish envelope attributes,
  derived from a subscription's field arguments.
- **Predicate index**: the counting-based attribute index (ADR-0006) that
  returns the candidate set in time sublinear in total subscription count for
  indexable predicates.
- **Principal**: the authenticated identity attached to a connection:
  subject, tenant, scopes/claims, and expiry, produced by an auth mode.
- **Publish envelope**: the versioned, schema-validated message carried on the
  bus for one event: field, tenant, attributes, payload, publish ID, origin
  node, and timestamps. Shape defined in ARCHITECTURE §bus.
- **Publish-time authorization**: the re-evaluation of a subscription's
  authorization against the current grant state for every candidate delivery,
  at the delivery decision point on the subscriber's node.
- **Quota**: a per-principal or per-tenant limit on connections,
  subscriptions, or inbound message rate.
- **Replay buffer**: the per-node, per-field bounded ring buffer of recent
  publish envelopes used to serve resume requests. It is not the bus and is
  not durable storage (ADR-0007).
- **Resume token**: the opaque, signed, versioned token a client presents on
  reconnect to request replay from its last acknowledged position.
- **Slow consumer**: a connection whose outbound queue reaches its configured
  bound, triggering the backpressure policy.
- **Subscribe-time authorization**: the authorization decision evaluated when
  a `Subscribe` message is received, before the subscription entry is
  registered.
- **Subscription entry**: the registered unit of matching: subscription ID,
  connection, field, compiled predicates, principal reference, and
  backpressure configuration.
- **Tenant**: the isolation unit for subjects, indexes, quotas, and admin
  visibility (ADR-0009). Single-tenant deployments use the implicit default
  tenant.

## Status vocabulary

Every deliverable in this documentation set has exactly one status:

- **accepted**: implemented on the mainline and backed by its named automated
  gate.
- **in progress**: present on a working branch; not a release claim until its
  gate passes.
- **planned**: specified but not implemented.
- **deferred**: intentionally outside the named gate and forbidden from being
  used to claim that gate complete.
