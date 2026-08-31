# Conduit Benchmark Plan

Document status: accepted.

Status of every deliverable in this document: `planned`. Nothing below has
been measured. No number in this document is a result; every number is a
target, a workload parameter, or a pass rule. Last revised: 2026-08-30.

## 1. Scope, Authority, and Owning Gates

This document defines what Conduit measures, how, on what hardware, with what
statistical treatment, and exactly which claims each number does and does not
support. Per the [documentation index](README.md), no performance number may
be published — in the README, docs, launch material, or a conference talk —
outside the claims ladder in §9. The [MARKETING_PLAN](MARKETING_PLAN.md)
claims ladder inherits from §9 and may only narrow it.

Companion documents:

- [PRODUCT_REQUIREMENTS.md](PRODUCT_REQUIREMENTS.md) — mints every
  requirement ID cited here (NFR-PERF-001..006, NFR-SCALE-001..006,
  FR-FILT-010, FR-RESUME-008, FR-RESUME-009).
- [BUILD_PLAN.md](BUILD_PLAN.md) — gate ownership and acceptance mechanics;
  gate acceptance links concrete report paths per §10.4 of this document.
- [ARCHITECTURE.md](ARCHITECTURE.md) — the instrumented pipeline stages and
  the `Bus` port measured here.
- [OPERATIONS_TEST_PLAN.md](OPERATIONS_TEST_PLAN.md) — CI mechanics for the
  microbenchmark and regression jobs this plan requires.
- [GLOSSARY.md](GLOSSARY.md) — controlled terms. In this document,
  "delivery" means enqueue onto a connection's outbound queue, exactly as
  the glossary defines it; socket write and client receipt are separate
  measured points (§2.3).
- ADR-0001 (runtime and GC evidence), ADR-0004 (NATS reference bus),
  ADR-0005 (envelope broadcast cost model), ADR-0006 (counting index and
  scan baseline), ADR-0007 (gap window contract).

Gate ownership of benchmark deliverables:

| Gate | Owns | Requirements |
| --- | --- | --- |
| R4 | Index-versus-scan benchmark W7, crossover analysis | FR-FILT-010, NFR-PERF-002 |
| R6 | Allocation evidence and bounded-memory-under-stall evidence (W9, L0 allocation suite) | NFR-PERF-005 |
| R7 | Measured gap window W8 | FR-RESUME-008 |
| R9 | Scale and fleet numbers: W1–W6, W10; reconnect-storm measurement | NFR-PERF-001, NFR-PERF-003, NFR-PERF-004, NFR-PERF-006, NFR-SCALE-001..006, FR-RESUME-009 |
| R10 | Report packaging, regression policy wiring, published-claim audit | release-tier claim binding per PRODUCT_REQUIREMENTS §10 |

No gate outside R0–R10 exists; no benchmark deliverable is owned by more than
one gate. A workload run before its owning gate is `in progress` engineering
data and may not be published.

## 2. Measurement Philosophy

### 2.1 Open-loop load

All throughput and latency workloads use open-loop generation: the arrival
rate of publishes, connects, and subscribes is scheduled independently of the
SUT's response times. A closed-loop generator (send, wait for response, send
again) lets a slow SUT throttle its own load and hides queueing; it is
forbidden for any published number. Publish arrivals are scheduled as a
Poisson process at the configured rate; connection-accept arrivals are
scheduled at fixed intervals with ±10% uniform jitter. If the generator
falls behind its own schedule by more than 1 ms at p99 (measured against the
intended-send timestamps), the run is invalid and is recorded as such.

### 2.2 Coordinated-omission-safe latency

Every latency sample is measured from the intended send time — the
scheduled arrival instant — not from the moment the generator actually
managed to send. Latency samples are recorded into HdrHistogram structures
(3 significant digits, range 1 µs to 60 s) so no percentile is computed from
a lossy summary. When the SUT stalls, the samples that should have been sent
during the stall are charged the stall; they are never silently omitted.
This is the capture discipline NFR-PERF-001 names.

### 2.3 Three distinct measured points

Every end-to-end latency claim names exactly one of three capture points.
They are never interchangeable:

1. **Enqueue (T_enq)**: the publish envelope has passed matching and
   publish-time authorization and the `Next` message is enqueued on the
   connection's outbound queue. Captured in-process by the gateway on the Go
   monotonic clock, exported as an HdrHistogram via the benchmark metrics
   endpoint. **NFR-PERF-001 is publish-to-enqueue.** Start of the interval:
   the envelope publish timestamp stamped by the origin node immediately
   before the `Bus.Publish` call.
2. **Socket write (T_write)**: the kernel `write`/`writev` for the frame
   containing the `Next` returns on the gateway. Captured in-process,
   monotonic clock, same histogram mechanism. Reported to expose queueing
   between enqueue and the wire; it feeds no PRD target directly.
3. **Client receipt (T_recv)**: `conduit-loadgen` timestamps the parsed
   `Next` on the generator host wall clock. Because this spans two hosts, it
   depends on clock synchronization (§4.5) and is always published with the
   clock error bound and the caveat that it is a LAN measurement on the
   reference environment, never a user-experience promise.

Within one host (single-node runs), T_enq and T_write share one monotonic
clock and their difference is exact. Across hosts, only chrony-disciplined
wall clocks are available and the error bound of §4.5 applies.

### 2.4 Environment discipline

No published number may originate from a shared, virtualized-with-neighbors,
or otherwise noisy host. Results come only from the named reference
environments of §3, with the manifest of §3.4 attached. A run on a laptop, a
CI runner, or a burstable cloud instance is development feedback and is
forbidden from every published table. Load generators are never co-resident
with the SUT (§3.3).

### 2.5 GC evidence

Per ADR-0001 and NFR-PERF-006, every published latency or memory number
carries: the `GOGC` and `GOMEMLIMIT` values in effect, the full
`GODEBUG=gctrace=1` stderr capture for the run, and a summary (GC count,
max pause, total pause, heap goal trajectory) in the run report. A number
without attached GC evidence is unpublishable regardless of how good it is.

## 3. Reference Environments

Full machine manifests live in the repository under `bench/env/` as one
directory per environment version (`bench/env/env-a-v1/`,
`bench/env/env-b-v1/`, `bench/env/env-c-v1/`). This section defines the
required contents of those manifests; the manifest files are the binding
record, and a run whose recorded manifest hash does not match a committed
manifest is invalid. Changing any manifest field creates a new environment
version and a new baseline (§10.5).

### 3.1 Env-A: single node

The SUT host for all single-node workloads (W1–W5, W8–W10 single-node
phases) and the host class for each fleet node in Env-B.

- Dedicated bare metal or a dedicated metal-instance class (no shared
  tenancy, no burstable CPU credit mechanics): 16 vCPU (8 physical cores
  with SMT, state recorded), 32 GiB RAM, 10 GbE NIC.
- Linux kernel pinned to one exact version from the 6.8 LTS line; the exact
  `uname -r` string is a manifest field and any kernel change is a new
  environment version.
- Swap disabled. Transparent huge pages set to `madvise` and recorded. CPU
  frequency governor `performance`; C-state limit and turbo state recorded.
- File descriptor limits: `fs.nr_open` and the conduit process `nofile`
  rlimit both 1,048,576; `fs.file-max` recorded.
- Required sysctl inventory (every value recorded in the manifest; values
  below are the reference settings):
  - `net.core.somaxconn = 65535`
  - `net.ipv4.tcp_max_syn_backlog = 65535`
  - `net.core.netdev_max_backlog = 65536`
  - `net.ipv4.ip_local_port_range = 1024 65535`
  - `net.ipv4.tcp_mem`, `net.ipv4.tcp_rmem`, `net.ipv4.tcp_wmem`: the
    kernel auto-scaled values are recorded verbatim, not tuned; if a
    workload requires tuning them the tuned values become part of a new
    environment version.
  - `net.ipv4.tcp_tw_reuse`, `net.ipv4.tcp_fin_timeout`: recorded.
- NIC configuration recorded: driver, ring sizes, RSS queue count, IRQ
  affinity map, offload flags (GRO/GSO/TSO states).
- Conduit runtime settings recorded: Go toolchain version (pinned per
  ADR-0001), `GOGC` (reference: 100), `GOMEMLIMIT` (reference: 28GiB),
  `GOMAXPROCS` (default, recorded).

### 3.2 Env-B: fleet

The fleet environment for W6 and all L3 claims.

- 3× Env-A-class nodes running Conduit.
- 1× dedicated broker host (8 vCPU / 16 GiB / 10 GbE) running a single core
  NATS server at a pinned version, TLS enabled, configuration file committed
  under `bench/env/env-b-v1/nats.conf`.
- 1× dedicated load-balancer host running HAProxy at a pinned version in
  TCP (layer-4) mode with long-lived-connection affinity, configuration
  committed under `bench/env/env-b-v1/haproxy.cfg`.
- All hosts in one availability zone on one 10 GbE switch fabric;
  measured host-to-host RTT ≤ 0.5 ms at p99 (verified and recorded at run
  start with a 60 s ping sweep). NFR-PERF-003's "same AZ" means exactly
  this fabric, nothing looser.

### 3.3 Env-C: load generators

- Minimum 2 dedicated generator machines, Env-A hardware class, same
  fabric. Generators are never co-resident with any SUT host, the broker,
  or the load balancer — no exceptions, including for microbenchmarks that
  happen to be small.
- **Harness self-test (mandatory before any SUT claim):** the generator
  pool must first demonstrate, against a minimal reference echo/discard
  server on separate hardware, at least 2× every target it will later
  assert against Conduit: ≥ 100,000 held connections with keepalive,
  ≥ 10,000 injected publishes/s, ≥ 1,000 accepts/s, receipt-side parse and
  timestamp throughput ≥ 2× the expected delivery rate of the workload.
  The self-test is a stored run report (§10) with `workload: SELF`. A SUT
  run whose generator pool lacks a current self-test report for the same
  loadgen git SHA is invalid. This is how the plan guarantees a measured
  knee belongs to Conduit and not to the harness.

### 3.4 Run manifest requirements

Every run (including self-tests and invalid runs) records: environment id
and manifest hash, Conduit git SHA, `conduit-loadgen` git SHA, canonical
configuration hash (SHA-256 over the sorted, comment-stripped config),
workload id and full parameter set, PRNG seed, start/end timestamps, warmup
boundary, chrony tracking samples, and the GC evidence of §2.5.

## 4. The Load Harness: `conduit-loadgen`

Status: `planned`. `conduit-loadgen` lives in the Conduit repository under
`bench/loadgen/` and is versioned with the gateway so a run manifest pins
both SHAs.

### 4.1 Design

- Implemented in Go. Client connections are not goroutine-per-connection on
  the generator: the client runs one epoll event loop per core
  (edge-triggered readiness via `golang.org/x/sys/unix`), with connections
  sharded across loops and a minimal `graphql-transport-ws` frame
  codec owned by the harness. Keepalive and scheduled-action timers use a
  timing wheel, not per-connection timers. Rationale: the generator must
  hold 2× the SUT's connection target (§3.3) in a fraction of the SUT's
  budget, or the self-test fails.
- **Source-port scaling:** a single (source IP, destination IP,
  destination port) tuple caps near 64k ephemeral ports. Each generator
  NIC is configured with 8 secondary IPv4 addresses (IP aliasing via
  `ip addr add`; a second NIC is the fallback if the fabric forbids
  aliases), and sockets bind explicitly round-robin across source
  addresses. Two generators × 8 addresses yields ≥ 16 source tuples —
  ample for 100k self-test connections against one listener address. The
  alias inventory is a manifest field.

### 4.2 Capabilities

- **Connect and hold** with the real protocol: TCP connect, TLS, WebSocket
  upgrade, `connection_init` carrying a valid auth token for a
  harness-provisioned principal, `connection_ack` wait with timeout,
  ping/pong keepalive at the server's cadence. A connection that fails any
  step is counted by failure class, never silently retried into the
  success numbers.
- **Subscribe** with parameterized filter distributions: per-workload
  configuration of subscriptions per connection, field selection
  distribution (Zipf with configured exponent and field count), and
  predicate mix (equality / range / residual-eligible fractions with the
  value distributions of §7).
- **Publish injection** by two paths, both exercised: HTTP mutations
  against the client listener (the product path) and `/admin/v1/publish`
  (the controlled-attribute path used when a workload needs exact
  attribute values). The run manifest records the path used; W2 uses
  mutations, W7-adjacent and W8 cohort targeting use admin publish.
- **Scripted churn**: scheduled connect/close sequences at configured
  rates with configured lifetime distributions (W4).
- **Expiry storms**: provisioning principals whose token expiries fall
  uniformly inside a configured window (W5).
- **Stalled-consumer mode**: a configured fraction of connections stops
  reading from the socket for a configured duration or permanently, while
  keeping the TCP connection open, to force outbound-queue growth and the
  backpressure policy (W9). Stall start, duration, and cohort membership
  are seeded and recorded.
- **Receipt timestamping**: parsed `Next` messages are timestamped on
  arrival and matched to publish records by publish ID for T_recv
  latencies and completeness accounting (W8 replay completeness).

### 4.3 Output format

- Latency series: HdrHistogram interval logs (one histogram per capture
  point per 10 s interval) in the standard HdrHistogram log format,
  mergeable without percentile averaging.
- Everything else: JSONL — one manifest record (§3.4), then per-interval
  records (achieved rates, failure counts by class, queue-behind-schedule
  gauge), then per-event records for discrete outcomes (connection
  failures, `resume_gap` notices, close codes observed).

### 4.4 Validity checks built into the harness

A run self-invalidates (recorded, kept, marked `invalid` with cause) if:
generator schedule lag exceeds §2.1's bound; clock sync exceeds §4.5's
bound; any generator process restarts; achieved fanout deviates more than
±10% from the workload's declared expected fanout (§7.2); or the SUT
manifest hash changes mid-run.

### 4.5 Clock synchronization

All Env-B/Env-C hosts run chrony against a LAN-local stratum-1 reference
(GPS-disciplined, on the same fabric). `chronyc tracking` is sampled every
10 s on every host for the duration of every run. Acceptance bound: absolute
offset ≤ 0.5 ms at every sample on every host; one violation invalidates
the run for cross-host latency purposes (single-host monotonic measurements
in the same run remain valid and are marked as such). Cross-host latency
percentiles are published as `value ± 2×max_offset` with the bound printed
next to the number. If PTP (linuxptp, hardware timestamping) is available
on the fabric, it may be used to tighten the bound to ±50 µs; the sync
method in force is a manifest field. Enqueue-point numbers (NFR-PERF-001 on
a single node) never depend on cross-host sync.

## 5. Statistical Treatment

- **Warmup discard**: every run discards the first 5 minutes, or the time
  until the rolling 1-minute p99 of the primary latency series changes by
  less than 10% across two consecutive minutes ("2× p99 settle"),
  whichever is longer. The discard boundary is recorded in the manifest.
- **Run length**: any claim labeled "sustained" (NFR-SCALE-001,
  NFR-SCALE-005, NFR-SCALE-006) requires ≥ 30 minutes of post-warmup
  measurement. Shorter runs support no sustained claim.
- **Independent runs**: every headline number (anything that appears in
  README, launch material, or a gate evidence row) requires ≥ 5
  independent runs — separate process starts, separate seeds, fresh
  connections. For each percentile, the published value is the
  **median across runs of that run's percentile**, always accompanied by
  the min and max of that percentile across runs. Percentiles are never
  averaged across runs, and histograms from different runs are never
  merged to fake a bigger sample.
- **Confidence**: where a single scalar per run exists (a run's p99, a
  run's throughput), a 95% confidence interval on the median is computed
  by bootstrap over the run values (10,000 resamples) and printed in the
  report. With 5 runs this interval is wide; the report prints it anyway
  rather than implying precision that does not exist.
- **Outliers**: an outlier run is investigated, never silently dropped. If
  a run is excluded, the report lists it with its data, the exclusion
  cause (with evidence: a chrony violation, a generator restart, a
  recorded neighbor event), and the numbers both with and without it. An
  exclusion without an identified cause is forbidden; the run counts.
- **Microbenchmarks (L0)**: `go test -bench` with `-count=10` minimum,
  compared via `benchstat`; a difference is claimed only at p < 0.05, and
  the benchstat output is the stored evidence. CPU frequency governor and
  machine state rules of §3.1 apply to microbenchmark hosts too.
- **Seeds**: every stochastic element (Zipf draws, payload sizes, Poisson
  arrivals, cohort selection) derives from one recorded per-run seed.

## 6. Memory Measurement Method

The published memory-per-connection figure (NFR-SCALE-002) is defined by
this procedure and no other:

1. **Instrument**: RSS is read from `/proc/<pid>/smaps_rollup` (`Rss:`
   field) by a sidecar sampler at 1 Hz. Go runtime metrics
   (`/memory/classes/heap/objects:bytes`, HeapAlloc, HeapIdle,
   HeapReleased, stack bytes, GC cycle count) are scraped alongside at the
   same cadence and stored in the run data.
2. **Step protocol**: connections are added in 10,000-connection steps
   from 0 to 50,000. After each step completes its connect phase, the
   harness holds, the gateway is asked to force a GC and return memory to
   the OS (`runtime/debug.FreeOSMemory` behind a benchmark-build-only
   admin hook), then a 60 s settle elapses, then 30 RSS samples (30 s)
   are taken and their median is the step's RSS.
3. **Per-connection figure**: ΔRSS/Δconnections computed per step; the
   published idle figure is the maximum per-step value across the 0→50k
   ramp (the least flattering step, not the average), from workload W1.
4. **Idle vs loaded**: *idle* means the W1 protocol state — connected,
   authenticated, one subscription registered, keepalive active, zero
   deliveries. *Loaded* means measured during W2 steady state; for the
   loaded figure no GC forcing is performed (that would be flattering),
   and the p95 across per-step samples during the sustained window is
   published against the ≤ 100 KiB bound.
5. **The honesty rule**: the published figure is RSS-based. Go heap
   figures are recorded and reported for diagnosis, but a Go-heap number
   never substitutes for RSS in any published claim, because RSS is what
   the operator's node actually spends.

## 7. Workload Catalogue

Every workload below is fully parameterized; a run that deviates from these
parameters is a different workload and supports none of these claims. All
workloads use principals provisioned in the harness auth fixture (JWT mode,
one principal per connection, single default tenant unless stated).
Expected-fanout declarations are validity checks per §4.4.

### 7.1 W1 — Idle hold

- Purpose: NFR-SCALE-001 (with W2 for the loaded half), NFR-SCALE-002 idle
  figure. Environment: Env-A + Env-C. Level: L2 (NATS bus configured,
  quiescent). Owning gate: R9.
- Parameters: 50,000 connections ramped at 500 accepts/s (100 s ramp per
  10k step, interleaved with the §6 step protocol); 1 subscription per
  connection, fields drawn Zipf(s=1.1) over 200 fields
  (`field_000`–`field_199`); predicate: single equality on attribute
  `region` drawn Zipf(s=1.0) over a 1,000-value domain; zero publishes;
  keepalive ping/pong at 30 s server cadence; hold 60 minutes after the
  ramp completes.
- Measured: RSS curve per §6; accept latency; keepalive round-trip p99;
  connection failure count (must be 0 for a valid pass); fd count;
  goroutine count.
- Pass: all 50,000 connections alive for the final 30 sustained minutes;
  idle ΔRSS/Δconn ≤ 64 KiB at every 10k step.

### 7.2 W2 — Reference mixed workload

- Purpose: the "reference workload" every PRD latency target names.
  NFR-PERF-001, NFR-SCALE-002 loaded figure, NFR-SCALE-005 at target
  rate. Environment: Env-A + Env-C. Level: L2. Owning gate: R9.
- Parameters:
  - Connections: 50,000, ramped as W1, each holding 2 subscriptions
    (100,000 subscription entries, exercising NFR-SCALE-004 in situ).
  - Subscription fields: Zipf(s=1.1) over 200 fields.
  - Predicate mix per subscription: 80% single equality on `region`
    (1,000-value domain, Zipf s=1.0); 15% range on numeric `priority`
    (uniform domain [0, 1,000,000], window width log-uniform between 10³
    and 10⁵); 5% residual-eligible (harness-registered custom matcher
    hook performing a payload attribute comparison), capped by the
    FR-FILT-006 ceiling.
  - Publish: 5,000 envelopes/s open-loop Poisson via HTTP mutations,
    fields and `region` values drawn from the same distributions as the
    subscriptions, `priority` uniform.
  - Payloads: size lognormal, μ = ln(1024), σ = 0.894 (median 1 KiB,
    p99 ≈ 8 KiB), content pseudorandom bytes from the run seed.
  - Expected fanout: mean ≈ 20 matched authorized connections per
    envelope (≈ 100,000 deliveries/s), p99 ≤ 400; achieved fanout
    verified within ±10%.
  - Duration: 30 minutes sustained after warmup.
- Measured: T_enq histogram (the NFR-PERF-001 series), T_write, T_recv
  (± clock bound), loaded RSS per §6, backpressure-policy drop counts
  (must be reported even when zero), gctrace.
- Pass: p50 ≤ 10 ms, p95 ≤ 50 ms, p99 ≤ 150 ms publish-to-enqueue;
  loaded memory p95 ≤ 100 KiB/conn; zero unexplained connection loss.

### 7.3 W3 — Publish-rate sweep

- Purpose: locate the single-node publish knee; context for NFR-SCALE-005
  and input to the W6 scaling-efficiency denominator. Environment: Env-A +
  Env-C. Level: L2 (repeated at L1 for bus-cost attribution). Owning
  gate: R9.
- Parameters: fixed 20,000 connections with the W2 subscription mix
  (40,000 entries); publish rate stepped 1,000 → 10,000 env/s in 1,000
  env/s increments; 5 minutes measured per step after a 2-minute settle;
  W2 payload and attribute distributions.
- Measured: T_enq percentiles per step; delivery throughput per step;
  drop counters; CPU utilization.
- Knee definition: the lowest step at which p99 T_enq exceeds 150 ms, or
  any non-policy drop occurs, or achieved delivery throughput falls below
  99% of expected. The knee is published as a range (last passing step,
  first failing step), never interpolated.

### 7.4 W4 — Connection churn

- Purpose: NFR-SCALE-006. Environment: Env-A + Env-C. Level: L2. Owning
  gate: R9.
- Parameters: W2 background scaled to 25,000 standing connections and
  2,500 env/s publish; on top, 500 accepts/s of churning connections
  with exponentially distributed lifetimes (mean 50 s, so churn
  steady-state ≈ 25,000 additional connections); churners perform the
  full protocol (init, auth, subscribe once, receive, clean close);
  duration 30 minutes sustained.
- Measured: accept latency histogram (TCP accept to `connection_ack`);
  T_enq percentiles for the standing population, compared against the
  same population's W2-scaled baseline; index registration/unregistration
  latency metrics; RSS trend (must be flat within ±5% over the sustained
  window).
- Pass: 500 accepts/s sustained; standing-population p99 T_enq within
  the NFR-PERF-001 bounds throughout.

### 7.5 W5 — Expiry storm

- Purpose: authorization-expiry load behavior at scale; feeds the R9
  evidence row for expiry handling under NFR-SCALE-001 conditions.
  Environment: Env-A + Env-C. Level: L2. Owning gate: R9.
- Parameters: W2 at 25,000 connections and 2,500 env/s; 10,000 of the
  25,000 principals provisioned with token expiries uniform over one
  60-second window that opens 10 minutes into the sustained period;
  harness does not proactively refresh, so the gateway's expiry path
  (close or refresh demand per AUTHORIZATION_MODEL) fires for all 10,000
  inside the window.
- Measured: expiry-action completion curve (time to 50/95/99% of the
  cohort processed); T_enq disturbance on the non-expiring population
  (max rolling 1-minute p99 during the window vs before); publish-time
  authorization decision latency metrics.
- Pass rule: no delivery to any connection after its principal's expiry
  instant beyond the documented grace (adversarially checked by receipt
  timestamps vs expiry times, clock bound applied); non-expiring
  population p99 T_enq ≤ 2× its pre-window value during the storm.

### 7.6 W6 — Fleet steady state and node-loss surge

- Purpose: NFR-SCALE-003 (Phase A), NFR-PERF-003 (Phase A), FR-RESUME-009
  and FR-FAN-005 measurement context (Phase B). Environment: Env-B +
  Env-C. Level: L3. Owning gate: R9.
- Phase A (steady state, 30 min sustained): 3 nodes × 40,000 connections
  (120,000 total) through the LB; W2 subscription and payload
  distributions; aggregate publish 12,000 env/s injected evenly across
  nodes via mutations. Measured: fleet delivery throughput (sum of
  per-node enqueue counters); per-node T_enq; bus added latency
  (origin-node publish timestamp to remote-node bus-receive timestamp,
  chrony-bounded) — the NFR-PERF-003 series; per-node bus bandwidth
  (the ADR-0005 published cost). Pass: fleet delivery throughput ≥ 2.5×
  the single-node delivery throughput measured at the W3 knee's last
  passing step, with the bus-overhead loss (3.0× ideal minus achieved)
  computed and published, not hidden; bus added latency p95 ≤ 5 ms.
- Phase B (node loss): at minute 35, `kill -9` one node. Its 40,000
  clients reconnect through the LB honoring the jittered retry-after
  hints (FR-RESUME-009), presenting resume tokens. Measured: reconnect
  completion curve (time to 50/95/99% of displaced clients reconnected
  and resubscribed on survivors); survivor T_enq disturbance (rolling
  1-minute p99 timeline); accept-pacing behavior; `resume_gap` incidence
  among the displaced cohort (cross-checked against W8 predictions).
  Duration: 20 minutes post-kill. Pass rule: survivors' established
  connections never violate NFR-PERF-001 bounds by more than 2× during
  the surge, and 99% reconnect completion within the figure this phase
  exists to measure and publish (target ≤ 120 s at reference pacing;
  the measured value is the deliverable either way).

### 7.7 W7 — Index microbenchmark (the FR-FILT-010 deliverable)

- Purpose: FR-FILT-010, NFR-PERF-002. Environment: Env-A, in-process
  (`go test -bench`), no sockets, no bus. Level: L0. Owning gate: R4.
- Matrix: entry counts {1,000; 10,000; 100,000; 500,000} × matcher
  {linear scan oracle; counting index} × candidate-set rate targets
  {0.1%, 1%, 10% of entries matching per envelope}, with the W2
  predicate mix (80/15/5) and, as a secondary axis, an all-equality mix
  and an all-range mix to expose per-operator cost.
- Method: entries generated from a recorded seed; 10,000 match
  operations per configuration per sample; ≥ 10 samples per §5;
  per-match latency recorded via the benchmark timer; allocation counts
  via `testing.B.ReportAllocs` (feeds the R6 evidence when the counter
  arrays are pooled per ADR-0006).
- Deliverables: the crossover analysis (the entry count at which the
  index beats the scan for each mix — FR-FILT-010 requires the index to
  win at and above 10,000 entries); the scaling curve (per-match p99 vs
  entry count, demonstrating sublinearity); p99 ≤ 1 ms at 100,000
  entries; residual-list cost measured separately at residual lengths
  {0, 100, 1,000}.
- These numbers support component-cost statements only (§9, L0).

### 7.8 W8 — Gap window (the FR-RESUME-008 deliverable)

- Purpose: FR-RESUME-008: measure the replay-buffer horizon and the
  resulting gap incidence at reference rates, per ADR-0007. Environment:
  Env-A + Env-C. Level: L2. Owning gate: R7.
- Parameters: W2 at 25,000 connections; publish rates {1,000; 5,000;
  8,000} env/s (three sub-runs); replay buffers at defaults (4,096
  envelopes / 16 MiB per field, FR-RESUME-003); disconnect cohorts of
  1,000 connections each detached cleanly and reconnected with resume
  tokens after {5, 30, 120, 600} seconds.
- Predicted horizon for calibration (published next to the measured
  value): at 5,000 env/s over 200 Zipf-weighted fields the hottest field
  receives ≈ 190 env/s, so its count-bound horizon is ≈ 4,096/190 ≈ 21 s,
  while cold fields' horizons are byte- or age-bound and far longer; the
  measurement exists because the prediction is not the deliverable.
- Measured: per-field horizon age metric distribution; replay
  completeness per cohort (delivered-on-resume publish IDs vs the exact
  set matched during the disconnection, computed from harness publish
  records); `resume_gap` incidence and the honesty check — every
  incomplete replay must carry `resume_gap`, and zero `resume_gap`
  notices may accompany a complete replay's covered range incorrectly.
  A single silent gap (missing envelope without `resume_gap`) fails the
  gate outright.
- Deliverable: the published gap-window table (publish rate × disconnect
  duration → replay completeness %, gap incidence %, hottest-field
  horizon seconds) cited by the public API contract documentation.

### 7.9 W9 — Slow consumer (the R6 bounded-memory evidence)

- Purpose: NFR-PERF-005 companion evidence: bounded memory and correct
  policy behavior under stalled consumers. Environment: Env-A + Env-C.
  Level: L2. Owning gate: R6.
- Parameters: W2 at 50,000 connections and 5,000 env/s; at minute 10,
  stall 5% of connections (2,500, seeded selection) in stalled-consumer
  mode (§4.2) for 10 minutes, then resume reading; backpressure policy
  matrix: one sub-run per policy (`drop_oldest`, `coalesce_by_key` with
  key cardinality 100 per field, `disconnect`).
- Measured: RSS timeline (must stay bounded: total growth during the
  stall ≤ stalled-conns × outbound-queue bound (256 messages / 1 MiB,
  FR-CONN-007) + 10% margin); drop counters per subscription vs
  harness-side expected drops; `conduit.dropped` notice presence on
  resume; close code 4704 count for the `disconnect` sub-run; T_enq
  disturbance for the unstalled 95%.
- Pass: memory bound holds; unstalled population stays within
  NFR-PERF-001 bounds; every policy-caused drop is counted (zero silent
  drops); control frames still delivered to stalled connections'
  sockets when writable.

### 7.10 W10 — Past-target degradation

- Purpose: characterize behavior beyond the published envelope so the
  claims ladder can state what the targets do *not* promise.
  Environment: Env-A + Env-C. Level: L2. Owning gate: R9.
- Parameters: push to 60,000 connections (ramp at 500 accepts/s) and
  8,000 env/s with the W2 distributions; 20 minutes at the overload
  plateau.
- Measured: load-shed correctness — 503 on upgrade at the fd threshold
  (FR-CONN-014) rather than accept-then-fail; established-connection
  latency and drop behavior; whether degradation is graceful (bounded
  latency inflation, policy drops, honest counters) or a collapse
  (unbounded queues, memory growth, cascade closes).
- Deliverable: a degradation narrative with data, published alongside
  the headline numbers. No pass/fail latency rule applies beyond: no
  memory bound violation, no silent drops, no crash. This workload
  supports no capacity claim; it exists to keep the capacity claims
  honest.

## 8. Metric Capture per Workload

| Workload | Captured series | Capture instrument | Claim(s) fed |
| --- | --- | --- | --- |
| SELF | generator capacity, schedule lag, receipt throughput | loadgen HDR logs + JSONL | run validity only (§3.3) |
| W1 | RSS curve, Go runtime metrics, accept latency, keepalive RTT, fd/goroutine counts | smaps_rollup sidecar 1 Hz; loadgen HDR; gateway metrics | NFR-SCALE-001 (idle half), NFR-SCALE-002 idle |
| W2 | T_enq/T_write HDR, T_recv HDR ± clock bound, loaded RSS, fanout counters, gctrace | gateway in-process monotonic histograms; loadgen; sidecar; GODEBUG=gctrace=1 | NFR-PERF-001, NFR-PERF-006, NFR-SCALE-002 loaded, NFR-SCALE-004, NFR-SCALE-005 |
| W3 | T_enq per step, delivery throughput, CPU, drop counters | gateway histograms; loadgen JSONL; sidecar | knee context for NFR-SCALE-005; W6 denominator |
| W4 | accept-to-ack HDR, standing-population T_enq, RSS trend, index churn metrics | loadgen HDR; gateway histograms; sidecar | NFR-SCALE-006 |
| W5 | expiry completion curve, T_enq disturbance timeline, authz decision latency | loadgen JSONL; gateway histograms | R9 expiry evidence row |
| W6-A | fleet delivery throughput, per-node T_enq, bus added latency ± clock bound, bus bandwidth/node | per-node gateway histograms; chrony-bounded cross-host timestamps; NATS + gateway counters | NFR-SCALE-003, NFR-PERF-003 |
| W6-B | reconnect completion curve, survivor T_enq timeline, resume_gap incidence, pacing behavior | loadgen JSONL + HDR; gateway histograms | FR-RESUME-009, FR-FAN-005 context |
| W7 | per-match latency, allocations/op, crossover table, residual cost | go test -bench + benchstat; ReportAllocs | FR-FILT-010, NFR-PERF-002, NFR-PERF-005 (index-path) |
| W8 | replay completeness, resume_gap incidence, horizon-age distribution | loadgen publish-ID reconciliation; gateway horizon metric | FR-RESUME-008 |
| W9 | RSS timeline, per-policy drop counters, notice/close-code counts, unstalled T_enq | sidecar; gateway counters; loadgen JSONL | NFR-PERF-005 companion (R6 bounded memory) |
| W10 | load-shed responses, latency/memory under overload, failure-mode narrative data | loadgen JSONL; gateway histograms; sidecar | honesty bounds in §9 and §11 |

Gateway histograms are exported on the admin metrics endpoint in
HdrHistogram-compatible form for the benchmark build; the sidecar and
loadgen outputs are the canonical stored artifacts (§10.3).

## 9. The Claims Ladder

This section is the binding rule set for publishing any Conduit performance
number anywhere. Every published number must name its level, its workload,
and its run report path. A number promoted above its level is a
documentation defect of the same severity as a failing release gate
(README conflict rules apply).

### 9.1 Levels

| Level | Configuration | May support | May never support |
| --- | --- | --- | --- |
| L0 | In-process microbenchmark, no sockets, no bus (W7, allocation suite) | Component-cost statements: "index match p99 X at N entries", "zero allocations per delivery on the hot path" | Any end-to-end latency, throughput, connection-count, or product claim |
| L1 | Single node, `bus/memory`, real sockets and protocol | Protocol and matching pipeline cost statements; bus-cost attribution by diff against L2 | Any NATS-configuration claim; any fleet claim; any published headline number |
| L2 | Single node, `bus/nats` with a real broker (Env-A) | Node-level product claims: the 50k connection figure, memory per connection, publish-to-enqueue percentiles, 5,000 env/s, 500 accepts/s, gap window | Fleet throughput or fleet latency; cross-node behavior; client-experience latency without the T_recv caveat |
| L3 | 3-node fleet with NATS and LB (Env-B) | Cross-node latency (NFR-PERF-003), node-loss behavior, reconnect-storm figures, scaling efficiency with published bus-overhead loss | Larger-fleet extrapolation ("N nodes scale linearly"); WAN behavior; multi-AZ behavior |
| L4 | Flagship demo application end-to-end on Env-B | The demo narrative: "the demo app sustains X with Conduit doing Y" | Any general product number; L4 measures one application, not the gateway envelope |

### 9.2 Forbidden promotions

Each of the following promotions is explicitly forbidden, named here so a
reviewer can cite the line:

1. **Idle → loaded**: a W1 result (50k idle connections, idle memory)
   never supports a statement containing a publish rate, a delivery
   latency, or the word "under load". NFR-SCALE-001 is claimed only with
   both W1 and W2 passing.
2. **Single-node → fleet**: no L2 number, multiplied by node count or
   otherwise, ever describes a fleet. Fleet claims come from W6 at L3,
   with the bus-overhead loss printed (NFR-SCALE-003's own text requires
   it).
3. **Enqueue → receipt**: a T_enq percentile is never described as
   "delivery to the client", "end-to-end", or "what users see". T_recv
   is reported separately, with the clock error bound and the LAN
   caveat, and is never the number bound to NFR-PERF-001.
4. **Memory bus → NATS**: an L1 (`bus/memory`) result never supports a
   statement about the shipped multi-node configuration. L1 exists for
   attribution diffs only.
5. **LAN → WAN**: every latency number in this plan is same-fabric,
   same-AZ. No number supports any statement about clients or fleets
   separated by WAN links, and no published sentence may leave that
   ambiguous.

### 9.3 Mandatory caveat templates

Published claims use these templates verbatim, with brackets filled from
the run report. Marketing rewording that drops a bracketed element is a
claims-ladder violation.

- **Connections (L2)**: "50,000 concurrent WebSocket connections on one
  node — measured on [env-a manifest id]: 16 vCPU / 32 GiB / 10 GbE bare
  metal, kernel [version], [N] runs ≥ 30 min sustained, full protocol
  with authentication and one subscription per connection. Report:
  [path]. Single-node figure; fleet behavior is measured separately."
- **Latency (L2)**: "Publish-to-delivery-enqueue on the reference
  workload [W2 link]: p50 [x] ms / p95 [x] ms / p99 [x] ms,
  coordinated-omission-safe, GC evidence attached. Enqueue is not client
  receipt; client-receipt figures and their clock error bound are in the
  report. Report: [path]."
- **Memory (L2)**: "[x] KiB per idle connection, [x] KiB p95 under the
  reference load — RSS delta per 10,000 connections on [env-a manifest
  id], not Go heap accounting. Report: [path]."
- **Fleet (L3)**: "A 3-node fleet delivered [x]× the single-node
  throughput on the reference workload ([x]% bus-overhead loss vs the
  3.0× ideal, published in the report). Same-AZ 10 GbE; no claim beyond
  3 nodes. Report: [path]."
- **Index (L0)**: "Predicate index p99 match [x] µs at 100,000 entries,
  in-process microbenchmark vs the linear-scan oracle; component cost,
  not end-to-end latency. Report: [path]."
- **Gap window (L2)**: "At [rate] env/s, a client disconnected [d]
  seconds recovered [x]% of matched events on resume; every incomplete
  replay carried resume_gap. Conduit is at-most-once live with bounded
  resume — this is the measured honesty of that contract, not a
  durability feature. Report: [path]."

### 9.4 Level assignment of the PRD targets

| Target | Level | Workload(s) |
| --- | --- | --- |
| NFR-PERF-001 publish→enqueue percentiles | L2 | W2 |
| NFR-PERF-002 index p99 ≤ 1 ms @ 100k | L0 | W7 |
| NFR-PERF-003 bus added latency p95 ≤ 5 ms | L3 | W6-A |
| NFR-PERF-004 gateway query overhead p95 ≤ 5 ms | L2 | query-overhead sub-run of W2 (Conduit-minus-data-source diff at 500 queries/s against the fixture source) |
| NFR-PERF-005 zero allocations per delivery | L0 | allocation regression suite + W7 |
| NFR-PERF-006 GC evidence | all | every run (§2.5) |
| NFR-SCALE-001 50k sustained | L2 | W1 + W2 |
| NFR-SCALE-002 memory/conn | L2 | W1 (idle), W2 (loaded) |
| NFR-SCALE-003 fleet ≥ 2.5× | L3 | W6-A (denominator from W3) |
| NFR-SCALE-004 100k entries | L2 in situ, L0 curve | W2, W7 |
| NFR-SCALE-005 5k env/s | L2 | W2, W3 |
| NFR-SCALE-006 500 accepts/s | L2 | W4 |
| FR-FILT-010 index vs scan | L0 | W7 |
| FR-RESUME-008 gap window | L2 | W8 |
| FR-RESUME-009 reconnect storm | L3 | W6-B |

## 10. Reporting

### 10.1 Run-report template

Every stored run report (one per run, plus one aggregate report per
headline campaign) contains, in order:

```
report/
  manifest.json        env id + manifest hash, conduit SHA, loadgen SHA,
                       config hash, workload id, seed, sync method,
                       chrony samples, warmup boundary, validity status
  percentiles.md       per-capture-point tables: p50/p90/p95/p99/p99.9/max,
                       per run and median-of-runs with min/max across runs,
                       bootstrap CI on run medians
  throughput.md        achieved vs scheduled rates per interval
  rss.csv + rss.md     RSS curve, per-step ΔRSS/Δconn, Go runtime series
  gctrace.log + gc.md  raw gctrace capture and summary (count, max pause,
                       total pause, GOGC, GOMEMLIMIT)
  hdr/                 raw HdrHistogram interval logs per capture point
  events.jsonl         failures by class, close codes, resume_gap notices,
                       drops by policy
  anomalies.md         every anomaly, investigated cause, and every
                       excluded run with its data and evidence (§5)
```

### 10.2 Aggregate campaign report

A headline campaign (≥ 5 runs of one workload) adds `campaign.md`: the
published numbers exactly as they will appear in any public channel, each
with its §9.3 template filled in, plus the per-run table backing the
median-of-runs values.

### 10.3 Storage

Reports live in the repository under `reports/<date>-<workload>/` (date as
`YYYY-MM-DD`, workload as its W-id or `SELF`), raw HDR logs and gctrace
included. Reports are append-only: a superseding campaign gets a new dated
directory; the old one is never edited or deleted.

### 10.4 Gate acceptance linkage

A BUILD_PLAN gate row that claims a benchmark deliverable is accepted only
when it links a concrete `reports/<date>-<workload>/campaign.md` path whose
manifest matches the release commit's environment and SHA rules. A gate row
citing this plan without a report path is not acceptance.

### 10.5 Regression policy

- Every published number is re-measured on each release candidate, same
  workload, same environment version, ≥ 3 runs (5 for any number being
  re-published upward).
- A regression > 10% on any published number (latency percentile up,
  throughput down, memory up) blocks the release **or** the number is
  re-published at its new value with a changelog entry — never silently
  retained at the old value, never quietly deleted.
- An environment version change (any §3 manifest field) invalidates all
  baselines: the first campaign on the new environment version is a new
  baseline and is published as such, with one overlapping
  old-environment run for continuity where the hardware still exists.
- L0 microbenchmarks run in CI on every merge to mainline (benchstat
  against the stored baseline, §5 rules); a statistically significant
  regression > 10% on the W7 configurations or the allocation count
  going above zero on the delivery path fails the build.

## 11. What These Numbers Do Not Support

This list is normative and travels with the published numbers. None of the
measurements in this plan support:

- **WAN latency claims.** Every environment is one same-AZ 10 GbE fabric
  with ≤ 0.5 ms p99 RTT. Nothing here describes clients on real networks,
  cross-region fleets, or mobile links.
- **Browser-client claims.** `conduit-loadgen` is a purpose-built Go
  client. Browser WebSocket stacks, event-loop scheduling, and JS parse
  cost are unmeasured; T_recv is a harness figure, not a browser figure.
- **Claims under permessage-deflate.** Compression is disabled by default
  (NFR-SEC-008) and disabled in every workload here. No latency, memory,
  or throughput figure applies with compression enabled.
- **Durability or at-least-once claims.** Conduit is at-most-once live
  with bounded best-effort resume (ADR-0007). W8 quantifies the gap
  window; it does not shrink the contract, and no replay-completeness
  percentage may be quoted as a delivery guarantee.
- **Payloads beyond the tested distribution.** Every figure assumes the
  W2 payload distribution (lognormal, median 1 KiB, p99 ≈ 8 KiB).
  Workloads dominated by payloads materially above that p99 are outside
  every published envelope.
- **Untested platforms.** Per ADR-0011, no figure attaches to any
  platform, kernel, architecture, or container runtime other than the
  pinned Env-A/Env-B manifests. A different kernel is a different
  environment with no numbers until measured.
- **Fleets larger than three nodes**, subscription predicate mixes
  materially different from W2's 80/15/5, tenant counts above one
  (single-tenant fixture throughout except where a workload states
  otherwise), or any TLS-termination topology other than the Env-B
  HAProxy configuration.

## 12. Deferrals and Requirements Traced

### 12.1 Explicit deferrals

All `deferred` items below are forbidden from being cited as evidence for
any R0–R10 gate:

- Multi-AZ and cross-region fleet benchmarking: deferred until a post-1.0
  ADR defines a WAN topology worth promising anything about.
- Browser-client latency measurement: deferred; would require a distinct
  harness and would still measure one browser build, not "browsers".
- Compression-enabled benchmarking: deferred until permessage-deflate has
  a security posture (NFR-SEC-008 amplification concerns) worth enabling.
- Multi-tenant-scale benchmarking (hundreds of tenants): deferred; the
  single-tenant reference workload bounds v1 claims.
- Alternative bus adapters: deferred; per ADR-0004 any future adapter
  must pass the R5 fault matrix before it may even be benchmarked for
  publication.
- Larger-fleet scaling curves (5+ nodes): deferred; the ADR-0005
  envelope-broadcast cost model predicts the pressure point, and the
  3-node measurement with published bus bandwidth per node is the v1
  deliverable.

### 12.2 Requirements traceability

| Requirement | Measured by | Ladder level | Owning gate | Status |
| --- | --- | --- | --- | --- |
| NFR-PERF-001 | W2 (T_enq, CO-safe per §2.2) | L2 | R9 | planned |
| NFR-PERF-002 | W7 scaling curve, p99 @ 100k | L0 | R4 | planned |
| NFR-PERF-003 | W6-A bus added latency | L3 | R9 | planned |
| NFR-PERF-004 | W2 query-overhead sub-run | L2 | R9 | planned |
| NFR-PERF-005 | allocation regression suite + W7 allocs/op + W9 bounded-memory evidence | L0 (+L2 W9) | R6 | planned |
| NFR-PERF-006 | §2.5 GC evidence on every run | all | R9 | planned |
| NFR-SCALE-001 | W1 + W2 sustained 30 min | L2 | R9 | planned |
| NFR-SCALE-002 | §6 method via W1/W2 | L2 | R9 | planned |
| NFR-SCALE-003 | W6-A vs W3 knee denominator | L3 | R9 | planned |
| NFR-SCALE-004 | W2 in situ (100k entries) + W7 curve | L2/L0 | R9 | planned |
| NFR-SCALE-005 | W2 sustained, W3 knee context | L2 | R9 | planned |
| NFR-SCALE-006 | W4 | L2 | R9 | planned |
| FR-FILT-010 | W7 crossover analysis | L0 | R4 | planned |
| FR-RESUME-008 | W8 gap-window table | L2 | R7 | planned |
| FR-RESUME-009 | W6-B reconnect surge measurement | L3 | R9 | planned |

Where this table and BUILD_PLAN §19 disagree, BUILD_PLAN §19 controls per
the README conflict rules. Every row above remains `planned` until its
owning gate links a campaign report per §10.4; this document contains
targets and methods, and — by its own rules — not one result.
