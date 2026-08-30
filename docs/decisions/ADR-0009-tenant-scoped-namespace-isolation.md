# ADR-0009: Tenant-Scoped Namespace Isolation in One Fleet

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-AUTH-017, FR-FAN-009, NFR-SEC-006,
  gate R3

## Context

Self-hosted deployments range from one team's single application to a
platform team serving many internal applications. The isolation question:
does one Conduit fleet serve multiple isolation domains, and if so, what
separates them? Full per-tenant process isolation (one fleet per tenant) is
always available by deployment choice and needs no design. The design
question is what a single fleet guarantees when it carries more than one
domain.

## Decision

Conduit is namespace-isolated by tenant within a fleet:

- every principal carries a tenant ID: from a configured OIDC claim, from the
  API key record, or from the custom authorizer's response; a missing tenant
  resolves to the implicit `default` tenant only when multi-tenancy is
  disabled in configuration — otherwise `connection_init` is rejected
  (4403);
- every scoping structure is tenant-keyed: predicate index shards, replay
  buffers, bus subjects (`conduit.<tenant>.…`), quotas, metrics dimensions
  (within the cardinality budget), and admin API visibility;
- a publish envelope carries exactly one tenant and can only ever be matched
  against subscription entries of the same tenant — enforced structurally
  (per-tenant index shards; no cross-tenant lookup path exists), not by a
  runtime check that could be skipped;
- schemas are per-tenant: one fleet can serve distinct SDL sets keyed by
  tenant, or one shared schema for all tenants; both modes are configuration.

Explicit non-guarantees: tenants share process memory, CPU, GC behavior, and
bus bandwidth. A noisy tenant can degrade latency for others up to the
protection quotas provide. Compliance regimes requiring hard isolation must
deploy per-tenant fleets; the root README and OPERATIONS_TEST_PLAN say so.

## Alternatives Considered

- **No tenancy concept**: rejected; retrofitting tenant keys into the index,
  bus subjects, resume tokens, and quotas after they are public contracts is
  a breaking rework, while carrying the key costs little now.
- **Hard multi-tenancy claims in one process**: rejected; a shared-heap Go
  process cannot honestly promise cross-tenant performance isolation, and
  pretending otherwise violates the no-unearned-claims rule.
- **Per-tenant OS processes managed by Conduit**: rejected as scope; that is
  an orchestration product. Kubernetes already provides it as per-tenant
  deployments.

## Consequences

Every structure keyed in ARCHITECTURE.md carries the tenant dimension from R1
onward, and the R3 adversarial suite includes cross-tenant probes (a tenant-A
subscription must never appear in tenant-B's candidate set even with
identical field and predicates — proven with an instrumented index). Metrics
labeled by tenant consume cardinality budget; the budget table in
OPERATIONS_TEST_PLAN caps labeled tenants and falls back to an `other`
bucket beyond the cap.
