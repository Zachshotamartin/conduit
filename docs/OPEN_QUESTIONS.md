# Conduit Open Questions

Document status: normative register of deferred decisions. Last revised:
2026-08-30.

Every entry carries a fail-closed default position that governs until the
question is reopened, and an explicit reopen trigger. Nothing outside this
file may defer a decision; "details to be determined" appears nowhere else
in the documentation set. Reopening a question produces an ADR, never an
in-place edit.

## OQ-01 — Durable, at-least-once delivery

- **Question**: should Conduit offer an at-least-once delivery tier backed
  by a durable log (JetStream/Kafka cursors)?
- **Default (fail closed)**: no. The delivery contract is at-most-once
  live with bounded resume (ADR-0007). No document, asset, or answer to a
  user implies otherwise.
- **Reopen trigger**: a concrete adopter use case that cannot tolerate the
  measured gap window and cannot run a durable stream alongside Conduit;
  reopening requires a new ADR superseding ADR-0007 and a new gate with
  its own benchmark obligations.

## OQ-02 — CEL (or other) expression language for authorization rules

- **Question**: should the structured YAML rule engine be extended with a
  general expression language?
- **Default (fail closed)**: no. v1 rules are the bounded structured form
  (AUTHORIZATION_MODEL §3); anything beyond delegates to the custom
  authorizer hook, which fails closed on timeout or malformed response.
- **Reopen trigger**: ≥3 real rule sets that cannot be expressed in the
  structured form and whose custom-authorizer delegation measurably
  breaches the publish-path latency budget in R9 data.

## OQ-03 — In-band token refresh

- **Question**: should Conduit support credential refresh without
  reconnect?
- **Default (fail closed)**: no. Refresh is reconnect with resume tokens
  (ADR-0008); expiry cuts fail closed at the expiry instant.
- **Reopen trigger**: the `graphql-transport-ws` ecosystem adopts a
  protocol-level refresh message, or R9's expiry-storm measurement shows
  expiry-driven reconnects exceeding 5% of total connection churn in the
  reference workload.

## OQ-04 — Additional bus adapters (Redis Streams, Kafka, cloud pub/sub)

- **Question**: which buses beyond NATS deserve supported adapters?
- **Default (fail closed)**: none. `bus/nats` is the only supported
  production bus; the `Bus` port keeps the option open (ADR-0004).
- **Reopen trigger**: an adopter commitment plus willingness to fund the
  full R5 fault-matrix obligation for the new adapter; an adapter without
  that evidence is never listed as supported.

## OQ-05 — Legacy `subscriptions-transport-ws`, SSE, or long-poll transports

- **Question**: should any transport beyond `graphql-transport-ws` exist?
- **Default (fail closed)**: no (ADR-0002); legacy-subprotocol upgrades
  are rejected with the documented codes.
- **Reopen trigger**: a named user population that cannot use WebSockets
  (corporate proxy environments) at meaningful scale; reopening requires a
  parallel conformance suite of R2 rigor for the new transport.

## OQ-06 — `@defer` / `@stream` and post-October-2021 spec features

- **Question**: adopt GraphQL spec drafts beyond October 2021?
- **Default (fail closed)**: no; the executor targets the ratified spec
  (NFR-COMPAT-002).
- **Reopen trigger**: ratification of the feature into a released
  specification edition plus reference-client support in the pinned
  `graphql-ws` range.

## OQ-07 — Windows-native and FreeBSD support

- **Question**: extend platform tiers (ADR-0011)?
- **Default (fail closed)**: Linux Tier 1, macOS Tier 2 dev-only; Windows
  users run the container.
- **Reopen trigger**: a supported-user request tied to a deployment that
  cannot run containers; any new Tier 1 platform carries the full
  benchmark obligation before any performance claim.

## OQ-08 — Interval tree upgrade for range-predicate churn

- **Question**: replace sorted endpoint arrays with an interval tree in
  the index?
- **Default (fail closed)**: sorted arrays (ADR-0006); simpler and
  cache-friendlier until churn data says otherwise.
- **Reopen trigger**: R4.09 or R9 churn benchmarks showing endpoint
  rebuild cost exceeding 10% of publish-path p99 budget at reference
  churn rates.

## OQ-09 — Bus interest-routing optimization

- **Question**: route publish envelopes only to interested nodes instead
  of broadcasting to all (revisiting part of ADR-0005's cost)?
- **Default (fail closed)**: broadcast; every node matches locally. No
  shared registry returns through this door by default.
- **Reopen trigger**: R9 measurement showing per-node bus bandwidth at
  the published capacity-model fleet sizes exceeding 50% of the reference
  NIC budget; reopening requires an ADR that preserves local-match
  correctness during interest-state staleness.

## OQ-10 — Per-field payload masking within delivered events

- **Question**: should field-level authorization mask fields inside event
  payloads rather than suppressing the delivery whole?
- **Default (fail closed)**: no masking; a delivery passes whole or is
  suppressed whole (AUTHORIZATION_MODEL §15). This is the safer wrong
  answer: suppression can over-hide but can never leak.
- **Reopen trigger**: adopter schemas demonstrating that whole-event
  suppression forces subscription fragmentation beyond the per-connection
  subscription quota in real designs.

## OQ-11 — Persisted-operation allowlists

- **Question**: support persisted-query/operation allowlisting as a
  production hardening mode?
- **Default (fail closed)**: not in v1; document bounds, complexity
  limits, and introspection policy are the v1 hardening set.
- **Reopen trigger**: security-reviewer demand from ≥2 real deployments;
  lands as an R-gate extension with its own conformance rows if reopened.

## OQ-12 — Continuous benchmarking automation

- **Question**: run the full W-catalogue continuously rather than per
  release candidate?
- **Default (fail closed)**: per release candidate (BENCHMARK_PLAN §10);
  continuous partial runs risk normalizing drift and unattended numbers.
- **Reopen trigger**: two consecutive release cycles where RC re-runs
  surface regressions older than one cycle — evidence that the cadence is
  too slow to localize causes.
