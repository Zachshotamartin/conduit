# Conduit

A self-hosted GraphQL gateway built around the subscription path: clients
subscribe over WebSocket (`graphql-transport-ws`) with per-subscription
filters, mutations publish, and the gateway fans out to every matching
connection across a horizontally scaled fleet — with authorization
re-evaluated at publish time and an explicit, measured backpressure policy.

## Implementation status — read this first

**Nothing is built.** This repository currently contains a complete,
normative planning documentation set and no product code. Every capability
described anywhere in this repository is `planned`. The honest claim, in
full:

> Conduit is a fully specified, unimplemented design for a
> subscription-first GraphQL gateway. Nothing runs. No performance, scale,
> conformance, or security property has been demonstrated.

The build is gated R0–R10; a capability may be claimed only when its gate's
automated evidence passes. The gate table and the current status of every
gate live in [docs/BUILD_PLAN.md](docs/BUILD_PLAN.md) §1.2 (all gates:
`planned`).

## What Conduit will be, when its gates pass

- **GraphQL execution** (gate R1): schema-first SDL, resolver bindings to
  PostgreSQL, HTTP, and function data sources; depth and complexity
  limits; introspection policy.
- **Exact subscription transport** (R2): the complete
  `graphql-transport-ws` protocol, proven by a conformance suite against
  the unmodified reference `graphql-ws` client and a hostile-client
  battery. Clients on the deprecated `subscriptions-transport-ws` library
  must migrate; Conduit will not speak it.
- **Authorization where data moves** (R3): OIDC/JWT, API-key, and
  custom-authorizer modes; enforcement at subscribe time and re-evaluated
  at every publish; defined, tested behavior for token expiry and grant
  revocation mid-subscription.
- **Sublinear filter matching** (R4): subscription arguments compile to
  predicates; a counting attribute index matches publishes sublinearly,
  with the linear scan retained as differential oracle and published
  benchmark baseline.
- **Cross-node fanout** (R5): pluggable bus (NATS reference), specified
  behavior under node loss, bus partition, backlog, and duplication.
- **Explicit backpressure** (R6): bounded per-connection queues with
  per-field `drop_oldest` / `coalesce_by_key` / `disconnect` policies,
  quotas, and adversarial-load evidence of bounded memory.
- **Honest resume** (R7): signed resume tokens, bounded replay, and a
  measured, documented gap window — never an implied guarantee of
  completeness.
- **Operability** (R8): metrics catalogue with a named cardinality budget,
  sampled tracing, versioned admin API, paced drain-on-deploy, rehearsed
  runbook.
- **Measured scale** (R9): the committed target is 50,000 concurrent
  WebSocket connections on a single benchmarked node with published
  memory-per-connection and delivery-latency percentiles. Until R9 passes,
  no number is claimed.
- **A real release** (R10): reproducible signed artifacts, Kubernetes
  deployment with a stated rollout loss contract, upgrade/rollback with a
  mixed-version window, uninstall/purge, and a flagship example
  application run end to end.

## Quick start

There is no runnable quick start yet. When R1 lands, this section becomes
the 15-minute walkthrough specified in
[docs/PRODUCT_REQUIREMENTS.md](docs/PRODUCT_REQUIREMENTS.md) §6.1. For
now, the way to evaluate Conduit is to read the plan:

1. [docs/README.md](docs/README.md) — documentation index, reading order,
   and the conflict-and-status rules that keep this repository honest.
2. [docs/PRODUCT_REQUIREMENTS.md](docs/PRODUCT_REQUIREMENTS.md) — what
   Conduit is for, every numbered requirement, and the release tiers.
3. [docs/BUILD_PLAN.md](docs/BUILD_PLAN.md) — the exhaustive gate-by-gate
   implementation plan, test-first tickets, and the requirement-to-evidence
   traceability matrix.

Contributor bootstrap (toolchain, test commands, CI) is specified in
[docs/OPERATIONS_TEST_PLAN.md](docs/OPERATIONS_TEST_PLAN.md) §4 and
becomes executable at gate R0.

## Documentation set

| Document | Contents |
| --- | --- |
| [docs/README.md](docs/README.md) | Index, reading order, conflict and status rules |
| [docs/PRODUCT_REQUIREMENTS.md](docs/PRODUCT_REQUIREMENTS.md) | Requirements, flows, acceptance, release tiers |
| [docs/BUILD_PLAN.md](docs/BUILD_PLAN.md) | Gates R0–R10, tickets, evidence matrices, traceability |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, concurrency model, interfaces, memory budget |
| [docs/PROTOCOL_CONFORMANCE.md](docs/PROTOCOL_CONFORMANCE.md) | Full protocol state machine, close codes, ambiguity register, conformance suite |
| [docs/AUTHORIZATION_MODEL.md](docs/AUTHORIZATION_MODEL.md) | Auth modes, both decision points, revocation/expiry, bypass-resistance argument |
| [docs/OPERATIONS_TEST_PLAN.md](docs/OPERATIONS_TEST_PLAN.md) | Test policy, verification matrices, CI, packaging, release gates |
| [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) | Assets, boundaries, abuse cases, controls, residual risk |
| [docs/BENCHMARK_PLAN.md](docs/BENCHMARK_PLAN.md) | Workloads, environments, statistics, the claims ladder |
| [docs/MARKETING_PLAN.md](docs/MARKETING_PLAN.md) | Positioning, claims register, launch assets — gated like everything else |
| [docs/GLOSSARY.md](docs/GLOSSARY.md) | Controlled definitions |
| [docs/OPEN_QUESTIONS.md](docs/OPEN_QUESTIONS.md) | Deferred decisions with fail-closed defaults and reopen triggers |
| [docs/decisions/](docs/decisions/) | ADR-0001…0011: language, protocol, executor, bus, state ownership, index, delivery contract, refresh/revocation, tenancy, observability, platforms |

## License and status of claims

Every statement in this repository about performance, scale, security, or
compatibility is governed by the claims discipline in
[docs/MARKETING_PLAN.md](docs/MARKETING_PLAN.md) §3 and the benchmark
claims ladder in [docs/BENCHMARK_PLAN.md](docs/BENCHMARK_PLAN.md). If a
sentence and a gate disagree, the gate is right.
