# ADR-0010: OpenTelemetry Traces, Prometheus Metrics, slog JSON Logs

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-ADMIN-001–FR-ADMIN-004, FR-OPS-009,
  NFR-MAINT-003, gate R8

## Context

A gateway holding tens of thousands of long-lived connections is operable
only through its telemetry: per-connection debugging is impossible at that
scale, and the interesting failures (slow consumers, revocation propagation
lag, bus partitions, index residual growth) are visible only in aggregates
and exemplar traces. The stack must be self-hostable (no SaaS dependency),
must not add per-delivery allocation on the hot path, and must live within an
explicit cardinality budget so the telemetry does not itself become the
memory incident.

## Decision

- **Metrics**: Prometheus exposition on the admin listener via the
  OpenTelemetry metrics SDK with the Prometheus exporter. The metric
  catalogue, label sets, and the named cardinality budget are normative in
  OPERATIONS_TEST_PLAN §observability; adding a label outside the budget is
  a reviewable contract change, not a patch detail.
- **Traces**: OpenTelemetry SDK with OTLP export, off by default, enabled by
  configuration with mandatory sampling (default parent-based ratio 0.01).
  Traced paths: HTTP operations end to end; subscribe handling; publish
  pipeline from mutation (or admin publish) through bus to delivery enqueue,
  with the bus hop as a link, sampled — never one span per delivery fanout
  leg beyond the sample.
- **Logs**: Go `log/slog` with a JSON handler; every record carries stable
  keys (`tenant`, `conn_id`, `sub_id`, `close_code`, `error_code` as
  applicable); no payload bytes, no credentials, no token contents ever
  (redaction tested with canaries, NFR-SEC-009); per-connection log volume is
  rate-limited so a hostile client cannot write the disk full (THREAT_MODEL
  §logging).

## Alternatives Considered

- **StatsD/Datadog-style push metrics**: rejected; assumes an agent or SaaS
  the self-hosting target may not run; Prometheus pull is the self-hosted
  default and scrape failure is visible.
- **Homegrown metrics registry**: rejected; the OTel/Prometheus pipeline is
  commodity, and hand-rolling it buys only subtle aggregation bugs.
- **Tracing every delivery**: rejected explicitly; at 100k deliveries/s the
  span volume is its own outage. Sampling with links is the honest design.
- **Structured logging via zap/zerolog**: viable, rejected for dependency
  surface; `slog` is standard library, fast enough off the hot path, and the
  hot path does not log per delivery at all.

## Consequences

The admin listener (FR-ADMIN-001) exists from R1 in skeleton form because
metrics land with the first executable slice. The cardinality budget makes
some questions (per-subscription latency) answerable only via exemplars or
debug endpoints, by design. OTel SDK versions are pinned and upgraded through
the dependency review gate (BUILD_PLAN §4.6).
