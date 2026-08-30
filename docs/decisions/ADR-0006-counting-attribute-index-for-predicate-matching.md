# ADR-0006: Counting Attribute Index for Predicate Matching

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-FILT-001–FR-FILT-008, NFR-PERF-002,
  gate R4

## Context

Client subscription arguments compile into predicates over publish-envelope
attributes. The naive matcher scans every registered subscription entry per
publish: O(S) with S subscriptions fleet-node-local — unacceptable at 50,000
connections each holding multiple subscriptions against thousands of
publishes per second. The accepted matcher must be sublinear in S for the
predicate forms the schema declares filterable, must be exactly equivalent to
the naive matcher (proven differentially), and must sustain high
subscribe/unsubscribe churn without stop-the-world rebuilds.

Predicate forms in scope (from FR-FILT-002): equality on scalar attributes,
membership (`in`), ordered comparison on numbers and timestamps (`gt`, `gte`,
`lt`, `lte`, `between`), boolean presence, and conjunctions of these.
Disjunctions are normalized into multiple conjunctive entries at subscribe
time (bounded expansion, FR-FILT-005).

## Decision

Implement the counting algorithm over per-(field, attribute) sub-indexes:

- each conjunctive subscription entry with K indexable predicates is
  registered with its count K;
- equality and membership predicates enter hash sub-indexes
  (`attribute value -> entry list`);
- ordered comparisons enter an interval sub-index per attribute (sorted
  endpoint arrays with binary search; interval tree if endpoint churn
  measurably degrades, decided by the R4 benchmark, not by taste);
- on publish, each sub-index emits matching entry IDs; a per-publish counter
  array accumulates hits; an entry whose counter reaches K joins the
  candidate set;
- predicates the index cannot express (declared non-filterable arguments,
  custom matcher hooks) place their entries on a per-field residual list that
  is scanned linearly; the residual list length is a published metric with a
  configured ceiling (default 1,000 entries per field), beyond which
  subscribes with non-indexable predicates are rejected with a typed error
  rather than silently degrading fleet latency.

The naive linear scan matcher is retained permanently as (a) the differential
oracle in property tests and (b) the benchmark baseline (FR-FILT-007,
BENCHMARK_PLAN §index). It is never selected in production configuration.

Concurrency: the index is sharded per (tenant, field); each shard is guarded
by its own mutex with copy-on-write epoch snapshots for the publish path so
matching never blocks on subscribe churn; correctness under concurrent
subscribe/publish is part of the R4 race suite.

## Alternatives Considered

- **Keep the linear scan**: rejected as the accepted implementation; O(S)
  per publish makes publish cost scale with connection count, which
  contradicts the scale thesis. Retained as oracle and baseline.
- **Decision-tree / BDD matchers**: rejected; best-in-class static match
  cost but rebuild cost on churn is O(index), and subscription churn is
  continuous in this workload.
- **Trie/topic matching (MQTT-style)**: rejected; arguments are typed
  attribute sets, not hierarchical topic strings; encoding ranges into topic
  segments is lossy.
- **Rete-style network**: rejected; general rule-engine machinery for what
  is strictly conjunctive attribute matching; higher constant factors and a
  much larger correctness surface.

## Consequences

Matching cost per publish becomes O(A log N + C + R) — attribute lookups,
candidate accumulation, residual scan — instead of O(S). The counter-array
allocation per publish must be pooled (R6 allocation regression test).
Disjunction expansion multiplies entry counts; the expansion bound (default 8
conjunctions per subscription) is part of the public contract. The
differential oracle makes index bugs detectable but only if property
generators cover the operator space: the R4 property corpus is acceptance
evidence, not optional hygiene.
