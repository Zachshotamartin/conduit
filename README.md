# Conduit

Conduit is an in-progress implementation of a self-hosted GraphQL gateway
specified around the subscription path: filtered `graphql-transport-ws`
subscriptions, mutation-driven publish, cross-node fanout, publish-time
authorization, bounded backpressure, and honest resume semantics.

## Implementation status — read this first

**No product capability is built.** Gate R0 repository infrastructure is
`in progress` on a working branch: the toolchain, deterministic test
foundations, checks, and workflow contracts exist, but no gateway listener
or GraphQL behavior exists and no gate has been accepted. The honest product
claim remains:

> Conduit is a fully specified, unimplemented design for a
> subscription-first GraphQL gateway. Nothing runs. No performance, scale,
> conformance, or security property has been demonstrated.

The build is gated R0–R10; a capability may be claimed only when its gate's
automated evidence passes. The gate table and the current status of every
gate live in [docs/BUILD_PLAN.md](docs/BUILD_PLAN.md) §1.2 (R0:
`in progress`; R1–R10: `planned`).

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

There is no product quick start yet. When R1 is accepted, this section
becomes the 15-minute walkthrough specified in
[docs/PRODUCT_REQUIREMENTS.md](docs/PRODUCT_REQUIREMENTS.md) §6.1. The R0
contributor harness is executable now:

```sh
make check-gh
make bootstrap
make check
make test
```

The normative evaluation path remains:

1. [docs/README.md](docs/README.md) — documentation index, reading order,
   and the conflict-and-status rules that keep this repository honest.
2. [docs/PRODUCT_REQUIREMENTS.md](docs/PRODUCT_REQUIREMENTS.md) — what
   Conduit is for, every numbered requirement, and the release tiers.
3. [docs/BUILD_PLAN.md](docs/BUILD_PLAN.md) — the exhaustive gate-by-gate
   implementation plan, test-first tickets, and the requirement-to-evidence
   traceability matrix.

Contributor bootstrap, tool versions, test commands, and CI expectations
are specified in
[docs/OPERATIONS_TEST_PLAN.md](docs/OPERATIONS_TEST_PLAN.md) §4.

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
| [docs/decisions/](docs/decisions/) | ADR-0001…0014: product/architecture decisions plus provisional gate stacking and explicitly authorized early publication |

## License and status of claims

This repository is publicly visible but currently has no project license
grant. Public access does not make it open source or grant permission to copy,
modify, or redistribute it.

Every statement in this repository about performance, scale, security, or
compatibility is governed by the claims discipline in
[docs/MARKETING_PLAN.md](docs/MARKETING_PLAN.md) §3 and the benchmark
claims ladder in [docs/BENCHMARK_PLAN.md](docs/BENCHMARK_PLAN.md). If a
sentence and a gate disagree, the gate is right.
