# Conduit Documentation Index

Document status: accepted.
Normative documentation index. Last revised: 2026-08-30.

Conduit is a self-hosted GraphQL gateway whose reason for existing is the
subscription path: clients subscribe over WebSocket with per-subscription
filters, mutations publish, and the gateway fans out to every matching
connection across a horizontally scaled fleet — with authorization re-evaluated
at publish time and an explicit, measured backpressure policy. Queries and
mutations are table stakes; the depth of this documentation set belongs to
subscriptions, authorization, filter matching, fanout, and connection
lifecycle.

The root [README](../README.md) is the quick start and the current
implementation snapshot. At the time of this document set's creation, nothing
is built: every deliverable in these documents is `planned` unless a gate
section says otherwise.

## Normative Source of Truth

Read these documents first and in this order:

1. [Product requirements and user flows](PRODUCT_REQUIREMENTS.md) defines
   Conduit's users, jobs, principles, scope, non-goals, API surface, core user
   flows, the complete numbered functional and non-functional requirement set,
   acceptance criteria, and release tiers. It is the only document that mints
   requirement IDs.
2. [Full build plan](BUILD_PLAN.md) defines implementation order, gates R0–R10,
   test-first tickets, per-gate evidence matrices, the requirement-to-evidence
   traceability matrix, and the boundary between current and planned behavior.
   It is the centerpiece of the set.
3. [Architecture](ARCHITECTURE.md) defines component boundaries, the process
   and concurrency model, the connection registry, the execution pipeline, the
   predicate index, and the bus abstraction at implementation depth, with real
   typed Go interfaces.
4. [Protocol conformance](PROTOCOL_CONFORMANCE.md) defines the complete
   `graphql-transport-ws` state machine, every message shape, every close code,
   every spec ambiguity and the decision taken, and the conformance suite
   design.
5. [Authorization model](AUTHORIZATION_MODEL.md) defines auth modes, the
   subscribe-time and publish-time decision points, field-level rules,
   revocation and expiry behavior, the tenancy model, and the
   bypass-resistance argument.

## Supporting Specifications

- [Operations and test plan](OPERATIONS_TEST_PLAN.md): developer bootstrap,
  test policy, harness rules, verification matrices, CI, performance budgets,
  packaging, install, upgrade, rollback, diagnostics, release gates, and
  requirement-to-evidence traceability mechanics. It controls verification
  mechanics while following the Build Plan's gate ownership.
- [Threat model](THREAT_MODEL.md): assets, actors, trust boundaries, abuse
  cases, controls, evidence, and residual risk. Every security claim in the
  set must bind to a named enforcement point and adversarial evidence listed
  there or in a gate's evidence matrix.
- [Benchmark plan](BENCHMARK_PLAN.md): what is measured, how, on what
  hardware, with what statistical treatment, and exactly what claims the
  numbers do and do not support. No performance number may be published
  outside the claims ladder it defines.
- [Marketing plan](MARKETING_PLAN.md): positioning, launch assets, and the
  claims ladder that binds every public statement to the gate evidence that
  earned it. Marketing copy is subject to the same no-unearned-claims rules
  as engineering documentation; a claim with no accepted gate behind it may
  not ship.
- [Glossary](GLOSSARY.md): controlled definitions. Documents in this set must
  use these terms with these meanings and no others.
- [Open questions](OPEN_QUESTIONS.md): deferred decisions. Every entry carries
  a fail-closed default position that governs until the question is reopened,
  and an explicit reopen trigger.

## Architecture Decisions

- [Decision record directory](decisions/) contains accepted and proposed ADRs.
- [ADR template](decisions/TEMPLATE.md) defines the required record structure.
- The initial binding decisions are ADR-0001 through ADR-0012: language and
  runtime, protocol support, execution approach, bus selection,
  connection-state ownership, predicate index structure, delivery guarantee,
  token refresh and revocation, multi-tenancy isolation, observability stack,
  supported platforms, and the security-supported toolchain pin.
- ADR-0013 and ADR-0014 record the provisional gate stack and the owner's
  subsequent authorization to make the repository public before R10 without
  promoting any gate or product claim.
- A reversal of any accepted decision requires a new ADR that supersedes the
  old one. Silent edits to an earlier decision are forbidden.

## Conflict and Status Rules

When documents disagree:

1. an accepted ADR controls the decision it explicitly records;
2. `PRODUCT_REQUIREMENTS.md` controls user-visible semantics and acceptance;
3. `BUILD_PLAN.md` controls implementation order, gate ownership, and status;
4. `ARCHITECTURE.md` controls component boundaries and interface contracts;
5. `PROTOCOL_CONFORMANCE.md` controls wire-protocol behavior that does not
   conflict with items 1–4;
6. `AUTHORIZATION_MODEL.md` controls authorization semantics that do not
   conflict with items 1–4;
7. `OPERATIONS_TEST_PLAN.md` and `BENCHMARK_PLAN.md` control verification,
   packaging, release, and measurement mechanics that do not conflict with
   items 1–6;
8. implemented code and passing tests control claims about what works today;
9. no document outranks the absence of evidence: an unimplemented feature is
   `planned` no matter what any sentence implies.

Documentation must follow these rules:

- Every deliverable is exactly one of `accepted`, `in progress`, `planned`, or
  `deferred`. Nothing is `accepted` without a named automated gate behind it.
  A package, type, stub, or happy-path unit test is never completion.
- Label planned behavior as planned until its named tests and evidence pass.
- Identify the gate behind every implementation claim.
- Never promote a single-node result into a fleet claim, an idle-pool load
  test into a throughput claim, or an in-process bus result into a production
  bus claim.
- Never let "authorization is checked" stand without naming the enforcement
  point and the adversarial test that proves it is not bypassable.
- Never imply delivery completeness the resume design does not provide: the
  gap window is real, documented, and measured, not hidden.
- Bind every performance number to the benchmark configuration that produced
  it, per the claims ladder in `BENCHMARK_PLAN.md`.
- Version every wire contract (protocol messages, resume tokens, bus
  envelopes, admin API, configuration schema) before any public compatibility
  promise.
- Record a reversal in a new ADR instead of silently editing away the earlier
  decision.
- Every phase closes with explicit deferrals and a requirements-traced list.
