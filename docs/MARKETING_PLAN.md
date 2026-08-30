# Conduit Marketing Plan

Document status: normative marketing plan. Last revised: 2026-08-30.
Owning gates: the claims register is maintained from R0; launch assets are
owned by R10 (BUILD_PLAN §18, R10.10).

Companion specifications: [Build plan](./BUILD_PLAN.md),
[Product requirements](./PRODUCT_REQUIREMENTS.md),
[Benchmark plan](./BENCHMARK_PLAN.md), [Docs index](./README.md).

Marketing copy is subject to the same no-unearned-claims rules as
engineering documentation. Every public statement about Conduit traces to
an accepted gate through the claims register below; the claims lint
(BUILD_PLAN R0.07) enforces it in CI over the README, docs, release notes,
and every file under `marketing/`.

## 1. Positioning

### 1.1 One-line position

Conduit is the self-hosted GraphQL gateway for teams whose product is
live: filtered subscriptions at scale, with authorization that stays
enforced after the socket opens.

### 1.2 The three differentiators

Everything published leads with one of these, because they are what the
gates actually prove:

1. **Publish-time authorization.** Grants change while subscriptions are
   live; Conduit re-evaluates at every delivery, with revocation and
   expiry mid-subscription as tested, measured behavior (R3, R5).
2. **Honest delivery semantics.** At-most-once live delivery, explicit
   per-field backpressure policies with counted drops, and resume with a
   measured, documented gap window — no implied completeness anywhere
   (R6, R7).
3. **Measured scale.** The 50,000-connection single-node figure with
   published memory-per-connection and latency percentiles, statistical
   treatment included, benchmark configuration attached (R9).

### 1.3 What Conduit never claims

The forbidden-claims list (BUILD_PLAN §18.3) is normative: no
"guaranteed delivery", no "exactly-once", no "real-time" without the
measured-latency qualifier, no "infinitely scalable", no "zero
downtime", no number without its ladder level and environment. Add: no
comparison that misstates a competitor's documented behavior; factual
comparisons cite the competitor's own documentation.

## 2. Audiences and messages

| Audience | Message | Proof asset |
| --- | --- | --- |
| Application developers building live products | Subscriptions that behave exactly per protocol with the client you already use, filters as plain GraphQL arguments, reconnects you can reason about | conformance suite results, resume guide with W8 curves |
| Platform engineers | One binary, one optional broker, a capacity model with published coefficients, drain that ends the deploy-pain pager rotation | capacity model, runbook, drain transcripts |
| Security reviewers | Named enforcement points, adversarial evidence, measured revocation propagation, plain residual-risk register | AUTHORIZATION_MODEL §12, THREAT_MODEL §10, SLO report |
| Evaluators and readers | A documentation set where every claim is gated and every number carries its configuration | this repository itself |

## 3. The claims register

The register is the single source for what may be said publicly. Status
is `unearned` until the named gate is accepted, then `earned` with an
evidence link. The lint fails any asset carrying a claim marker whose
row is `unearned`.

| # | Claim (exact public sentence) | Ladder level | Gate | Status |
| --- | --- | --- | --- | --- |
| C1 | Conduit executes GraphQL queries and mutations against PostgreSQL, HTTP, and function sources with enforced depth and complexity limits (single node, development auth only). | L1 | R1 | unearned |
| C2 | Conduit implements `graphql-transport-ws`, proven by a conformance suite against the unmodified reference client. | L1 | R2 | unearned |
| C3 | Conduit enforces authorization at subscribe time and at every publish, with tested revocation and expiry mid-subscription. | L1 | R3 | unearned |
| C4 | Conduit's filter index is sublinear and benchmarked ≥10× over linear scan at 100,000 subscriptions (microbenchmark, single process). | L0 | R4 | unearned |
| C5 | Conduit fans out across nodes with tested node-loss and partition behavior; revocation propagates fleet-wide with measured p99 ≤ 2 s on the reference 3-node fixture. | L3 (fixture-scoped) | R5 | unearned |
| C6 | Conduit's per-field backpressure policies and quotas hold under adversarial load with bounded memory (CI-scale evidence). | L1 | R6 | unearned |
| C7 | Conduit resumes subscriptions across reconnects with bounded replay and an honest, measured gap window (published horizon curves). | L2 | R7 | unearned |
| C8 | Conduit is operable: documented metrics catalogue, sampled tracing, paced drain, and a rehearsed runbook. | L2 | R8 | unearned |
| C9 | Conduit sustains 50,000 concurrent WebSocket connections on a single benchmarked node with ≤ 64 KiB idle memory per connection and publish-to-delivery p95 ≤ 50 ms on the reference workload. | L2 | R9 | unearned |
| C10 | A three-node Conduit fleet delivers ≥ 2.5× single-node throughput on the reference workload, bus overhead published. | L3 | R9 | unearned |
| C11 | Conduit 1.0 ships reproducible, signed artifacts with SBOM and provenance, a tested Kubernetes rollout with a stated loss contract, and a real example application. | L4 | R10 | unearned |

Register maintenance rules: a gate's acceptance PR updates its rows in
the same change (BUILD_PLAN §18.1); numbers in C9/C10 are replaced by
the measured values at R9 acceptance if they differ (with the ADR that
revised the target); a regressed number is re-measured or retracted per
BENCHMARK_PLAN §10, and a retraction is a release-notes item.

Every earned claim must carry, in any asset where it appears, the
caveat template of its ladder level (BENCHMARK_PLAN §9). L0 =
microbenchmark caveat; L1 = deterministic/CI-scale caveat; L2 =
single-node reference-environment caveat; L3 = fleet fixture caveat;
L4 = end-to-end demonstration caveat.

## 4. Asset inventory

### 4.1 Continuous (from R0)

- Root README status snapshot — updated at every gate acceptance; the
  only always-current asset.
- The documentation set itself — deliberately public-facing at
  publication; "read the build plan" is a marketing action for the
  evaluator audience.

### 4.2 Launch assets (R10.10, all under `marketing/`)

- **Site copy**: landing page with the three differentiators, the
  claims with caveats, the honest non-goals (PRD §4.3 verbatim where
  relevant).
- **Launch post**: the engineering narrative — why publish-time
  authorization, why at-most-once with honest gaps beats implied
  completeness, how the 50k number was actually measured (link the
  reports; show the gctrace).
- **Demo script**: runs the six flagship scenarios (PRD §1.2) live
  against the Env-B fleet; no staged footage; the script is in-repo and
  reproducible by any reader.
- **Architecture explainer**: the two-pipeline diagram and the
  enforcement points, written for the security-reviewer audience.
- **Comparison page**: factual, documentation-cited comparison of
  subscription behavior across self-hostable GraphQL servers; reviewed
  against §1.3 rules.
- **Release notes for 1.0**: claims-audited like everything else.

### 4.3 Post-1.0 cadence

Benchmark re-runs per release candidate refresh or retract published
numbers (never silent); one engineering post per substantial ADR
(reversals are content, not embarrassments — the honesty is the brand).

## 5. Channels and sequencing

1. Repository public at R10 (BUILD_PLAN §15.7 pre-publication audit
   first).
2. Launch post + site at 1.0.0 tag.
3. Community submissions (the GraphQL weekly newsletters, relevant
   subreddits/HN) with the launch post — the demo script and reports
   are the substance; no submission before the claims audit is green.
4. Conference talk proposals only after 1.0, built from the launch
   post's measured-honesty narrative.

## 6. Explicit deferrals

Deferred until after 1.0: paid-support positioning, hosted-offering
messaging (a hosted offering is itself a PRD non-goal), case studies
(require real deployments), logo/brand-identity work beyond a
wordmark. Each returns through OPEN_QUESTIONS or a post-1.0 marketing
revision of this plan.

## 7. Requirements traced

This plan implements the marketing scope added to the build order
(BUILD_PLAN §18, tickets R0.11 and R10.10, and the per-gate §X.8/§X.9
register obligations). It mints no requirement IDs; its enforcement
mechanism is the claims lint (NFR-MAINT-004 machinery) and the
release-candidate checklist rows 7 and 9 (BUILD_PLAN §20).
