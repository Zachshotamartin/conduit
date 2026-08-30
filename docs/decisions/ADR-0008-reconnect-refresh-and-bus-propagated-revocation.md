# ADR-0008: Reconnect-Based Token Refresh and Bus-Propagated Revocation

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-AUTH-012–FR-AUTH-016, NFR-SEC-003,
  gate R3

## Context

Grants change while subscriptions are live. Two distinct problems: (a) a
token presented at `connection_init` expires while the connection is healthy;
(b) an administrator revokes a grant (user disabled, scope removed, API key
rotated) and the change must reach every node's publish-time authorization
decisions. The `graphql-transport-ws` protocol has no re-authentication
message: `connection_init` may be sent once (a second one closes with 4429),
and the spec reserves no client-to-server credential update.

## Decision

**Expiry**: the principal's expiry is tracked on a shared timing wheel. At
expiry minus a configured warning window (default 60 s), the server sends a
`ping` carrying `{"conduit":{"expires_in_ms":<n>}}` so well-behaved clients
can preemptively reconnect with a fresh token and resume tokens. At expiry,
the server completes no further deliveries for that principal (publish-time
checks fail closed immediately), sends `error` on each live subscription with
a typed `TOKEN_EXPIRED` error, and closes the connection with Conduit close
code 4403 `Credential expired`. There is no in-band refresh in v1; refresh is
reconnect (with ADR-0007 resume continuity). Recorded in OPEN_QUESTIONS with
a reopen trigger (protocol-level refresh adopted upstream, or measured
reconnect load from expiry churn exceeding 5% of connection churn).

**Revocation**: revocations enter through the admin API or the custom
authorizer's revocation feed and are published on the bus control subject
`conduit.<tenant>.ctl.revoke` as `{kind: principal|subject|key|scope, id,
issued_at, revocation_id}`. Every node applies them to an in-memory
revocation set consulted by both decision points (subscribe-time and
publish-time). Applying a revocation immediately fails publish-time checks
(fail closed), then a sweep closes affected subscriptions with `error`
(`GRANT_REVOKED`) and closes connections whose principal is fully revoked
with 4403. Propagation SLO: p99 node-application latency ≤ 2 s from admin
acknowledgement, measured in R5's fleet suite. During bus partition, a node
that cannot receive control messages enters degraded mode after the control
heartbeat timeout (default 10 s) and applies the configured degraded policy:
`fail_closed` (default: suspend deliveries for revocable-auth-mode
principals) or `fail_open_bounded` (continue for at most the configured
staleness ceiling, then suspend). The choice is an explicit, logged operator
decision (FR-AUTH-016).

**Caching**: publish-time authorization may cache a decision per
(subscription, grant-state epoch); any revocation or policy change advances
the epoch and invalidates. Cache correctness is proven by the R3 adversarial
suite (revoke-then-publish must never deliver).

## Alternatives Considered

- **In-band refresh via a custom extension message**: rejected for v1;
  nonstandard clients only, and the unmodified reference client
  (FR-SUB-002) could never use it, so reconnect-based refresh must exist and
  be correct anyway.
- **Per-publish authorizer callout with no cache**: rejected; an external
  authorizer on the delivery path at fanout rates is a latency and
  availability cliff; the epoch cache bounds staleness to revocation
  propagation, which is measured.
- **Revocation via shared database polling**: rejected; polling interval
  becomes hidden staleness, and the bus already exists with the control
  channel and partition-detection semantics R5 must prove.

## Consequences

Expiry churn converts into reconnect load; the R9 harness includes an
expiry-storm scenario. The revocation set is memory-resident and bounded:
entries expire with the grant lifetimes they revoke plus slack, and the set
size is a published metric with an alarm threshold. The degraded-mode policy
is a visible operator commitment: OPERATIONS_TEST_PLAN documents both
settings and the runbook covers the suspend behavior.
