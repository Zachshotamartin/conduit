# Conduit: Installation, Testing, Operations, and Release Plan

Document status: accepted.
This is the normative lifecycle, verification, and release specification.
Gate R0 repository infrastructure is `in progress`; R1 through R10 remain
`planned`. Last revised: 2026-08-30.

Companion sources of truth:

- [Product requirements and user flows](PRODUCT_REQUIREMENTS.md) — the only
  document that mints requirement IDs; every FR/NFR cited here is defined
  there.
- [Full build plan](BUILD_PLAN.md) — implementation order, gate ownership
  (R0–R10), ticket-level test-first workflow (§4.1), dependency review gate
  (§4.6), and the authoritative requirement-to-evidence matrix (§19).
- [Architecture](ARCHITECTURE.md) — component boundaries and the typed ports
  (Transport, Bus, DataSource, SubscriptionAuthorizer) this plan tests against.
- [Protocol conformance](PROTOCOL_CONFORMANCE.md) — the
  `graphql-transport-ws` state machine, message shapes, and the close-code
  table (§6) that the PROTO and CONN matrices enforce.
- [Authorization model](AUTHORIZATION_MODEL.md) — enforcement points and
  bypass-resistance arguments the AUTHZ matrix proves.
- [Threat model](THREAT_MODEL.md) — abuse cases behind the adversarial rows.
- [Benchmark plan](BENCHMARK_PLAN.md) — measurement method, environments,
  statistical treatment, and the claims ladder for every number in §12.
- [Glossary](GLOSSARY.md) — controlled terms; this document uses them with
  those meanings and no others.
- ADR-0001 through ADR-0011 in [decisions/](decisions/) — binding decisions
  this plan operationalizes.

## 1. Operating Model and Lifecycle Invariants

### 1.1 What this document controls

This plan controls verification mechanics, developer bootstrap, repository
protection, dependency policy, configuration and secret locations, CI,
packaging, installation, upgrade, rollback, uninstall, diagnostics,
observability contracts, incident response, and per-gate operational evidence.
It does not control implementation order or gate ownership (BUILD_PLAN
controls both) and does not control measurement method, benchmark hardware,
or statistical treatment (BENCHMARK_PLAN controls those). Where a LOAD row in
§10.11 needs a measurement method, the row cites BENCHMARK_PLAN and defines
only the pass/fail mechanics here. Where this document and BUILD_PLAN §19
disagree on which gate owns a requirement, BUILD_PLAN §19 controls.

Lifecycle stages this document supplies concrete controls for:

1. Create, protect, and maintain the Git repository (§3).
2. Bootstrap a clean developer machine to green tests (§4).
3. Review, pin, install, update, and remove dependencies (§5).
4. Locate configuration, TLS material, and signing keys; confirm the
   no-durable-state design (§6).
5. Validate configuration and environment before serving (§7).
6. Test the protocol, authorization, index, fanout, backpressure, resume,
   lifecycle, and operations surfaces (§8–§10).
7. Run deterministic CI separate from container, nightly, and release
   workflows (§11).
8. Enforce performance and resource budgets as regression ceilings (§12).
9. Build, sign, and attest artifacts (§13).
10. Install, upgrade, roll back, uninstall, and purge (§14).
11. Diagnose incidents with redacted support bundles (§15).
12. Operate within the observability contract (§16).
13. Respond to incidents and revoke releases (§17).
14. Close gates R0–R10 only with requirement-linked evidence (§18–§20).
15. Audit that every product surface element is owned and tested (§21).

### 1.2 Lifecycle invariants

These invariants hold at every stage and are restated wherever a procedure
could tempt a shortcut:

- **No unearned claims.** A single-node result is a single-node claim and is
  never promoted to a fleet claim; the fleet claim requires the R5/R9 cluster
  harness evidence. An idle-connection result is an idle claim and is never
  promoted to a loaded-throughput claim; the two are separate SOAK rows with
  separate budgets (NFR-SCALE-001 versus NFR-SCALE-005). A `bus/memory`
  result is never promoted to a NATS claim; every FAN row proven on the
  memory bus has a named broker-suite counterpart before R5 closes
  (ADR-0004). A macOS result never carries a performance or scale claim
  (ADR-0011).
- **Bounded everything.** Every queue, buffer, index, parser input, and cache
  has a configured bound and a defined overflow behavior; the BP and SOAK
  matrices hunt unbounded growth explicitly (product principle §3.4,
  NFR-SEC-001).
- **Honest failure.** Drops are counted (FR-CONN-009), gaps are announced
  (FR-RESUME-005), degraded modes are entered explicitly and surfaced
  (FR-AUTH-015, FR-FAN-006). No test in this plan may pass by silently
  absorbing a contract violation.
- **Determinism before infrastructure.** Protocol, matching, authorization,
  fanout, backpressure, and resume behavior are proven with injected clocks,
  the deterministic memory bus, and scripted clients before any broker or
  cluster is involved (product principle §3.6). Brokers and clusters validate
  the deterministic model; they never substitute for it.
- **Status honesty.** Every deliverable in this document carries exactly one
  status: `accepted`, `in progress`, `planned`, or `deferred` (GLOSSARY
  status vocabulary). R0 repository infrastructure is `in progress`; all
  later-gate deliverables remain `planned`. Nothing becomes `accepted`
  without its named automated gate (NFR-MAINT-004).

### 1.3 Evidence purpose

Tests prove named claims at the boundary that enforces them. Coverage
percentage alone proves nothing about authorization bypass resistance
(NFR-SEC-002), memory boundedness (NFR-SCALE-002), delivery ordering
(FR-FAN-004), or resume honesty (FR-RESUME-005). Every claim in the set maps
to: a requirement ID; an owning package boundary; at least one deterministic
enforcement-boundary test; applicable container or cluster tests; a
user-visible failure category (PRODUCT_REQUIREMENTS §8); and a release gate
with a stored evidence artifact (§19).

## 2. Supported Environment and Capability Matrix

### 2.1 Platform tiers (ADR-0011)

| Tier | Platforms | What runs there | What may be claimed |
| --- | --- | --- | --- |
| Tier 1 (production) | Linux amd64, Linux arm64 | Full CI including race, integration, container, cluster, and benchmark suites; release artifacts | Correctness, performance, and scale claims per the claims ladder |
| Tier 2 (development) | macOS arm64 | Unit, protocol, and deterministic integration suites in CI; local development | Correctness only; no performance or scale claim ever attaches to macOS results |
| Unsupported | Windows native, FreeBSD, 32-bit | Nothing; Windows users run the Linux container | Nothing |

Platform-conditional code lives only in `internal/platform` behind build
tags; the R0 architecture check fails any other package containing
`runtime.GOOS` (ADR-0011, NFR-MAINT-001).

### 2.2 Toolchain and external-system version ranges

| Component | Pinned or supported range | Enforcement |
| --- | --- | --- |
| Go toolchain | 1.23.x, exact patch pinned by the `toolchain` directive in `go.mod` | CI fails if `go version` differs from the pin; ADR-0001 |
| NATS server | 2.10.x through 2.11.x; container suites pin one exact digest per range endpoint | Nightly `nats-matrix` job runs both endpoints (§11.4) |
| nats.go client | Exact version pinned in `go.mod` | Dependency review gate §5 |
| PostgreSQL (relational source, FR-GQL-004) | 14, 15, 16; container suites pin exact digests | `integration-postgres` job runs 14 and 16; 15 nightly |
| Node.js (reference-client fixture) | 20.x LTS, exact version pinned in the fixture container image | Conformance suite fails on unpinned Node |
| `graphql-ws` reference client | Pinned version range documented in PROTOCOL_CONFORMANCE; the fixture container pins one exact version and the nightly matrix runs the range endpoints | NFR-COMPAT-001 |
| Kubernetes (deploy tests) | 1.29 through 1.31 via kind; manifests validated against all three | R10 cluster suite |
| kind | 0.23 or later | Cluster harness bootstrap check |
| Container runtime | Docker Engine 24+ or Podman 4.9+ | `make integration` preflight |
| syft / cosign / benchstat / staticcheck / golangci-lint / govulncheck | Exact versions pinned in `Makefile` tool block | `make bootstrap` installs the pins; CI verifies versions |

### 2.3 Developer-machine expectations

- Any Tier 1 or Tier 2 machine with 8 GiB free memory and 20 GiB free disk
  runs everything through the container suites. The cluster/load harness
  (§8.4d) additionally wants 16 GiB and is not required for local
  development.
- Docker or Podman is required only for `make conformance` and
  `make integration`; the deterministic suites (`make test`, `make check`)
  require no container runtime, no broker, and no database.
- No cluster is required for any evidence through R8 except the
  broker-specific rows (the R5 nightly NATS suite and CHAOS rows marked
  cluster harness); everything else through R8 runs on one developer
  machine.
- Local file-descriptor limit must be ≥ 8,192 for the socket-level harness;
  `conduit doctor` and the harness preflight both check it.

## 3. Repository, Git, and Branch Protection

### 3.1 Model

Trunk-based development on `main` with short-lived branches and mandatory PR
review. No direct pushes to `main`, including by administrators. Gate
evidence workflows are release-blocking: a red gate-evidence check on the
release SHA blocks the tag, with no override path other than fixing the code
or formally reopening the gate in BUILD_PLAN.

### 3.2 Bootstrap prerequisite and repository creation

Before repository creation, the operator must hold an authenticated GitHub
account with `repo` and `workflow` scopes:

```sh
gh auth status
```

The output must show a logged-in account whose token scopes include `repo`
and `workflow`; if not, run `gh auth login --scopes repo,workflow` and
re-check. Then create and protect the repository:

```sh
gh repo create Zachshotamartin/conduit --private --source . --remote origin --push
gh api -X PUT "repos/Zachshotamartin/conduit/branches/main/protection" \
  --input .github/branch-protection.json
```

`.github/branch-protection.json` (checked into the repository, applied by the
command above and re-applied by a scheduled drift check):

```json
{
  "required_status_checks": {
    "strict": true,
    "contexts": [
      "pr / lint",
      "pr / vet",
      "pr / arch-check",
      "pr / unit-race",
      "pr / proto-race",
      "pr / authz-race",
      "pr / index-race",
      "pr / docs-status-lint",
      "pr / metrics-contract",
      "pr / deps-audit",
      "pr / trace-check",
      "integration / conformance-node",
      "integration / integration-nats",
      "integration / integration-postgres",
      "integration / socket-hostile"
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1,
    "dismiss_stale_reviews": true
  },
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "restrictions": null
}
```

The exact job names above are the required-check contract; renaming a CI job
is a change to this file and to §11 in the same PR, enforced by a CI check
that diffs the workflow job list against the protection contexts.

### 3.3 Commit conventions

Commit subjects use `<type>: <description>` with types `feat`, `fix`,
`refactor`, `docs`, `test`, `chore`, `perf`, `ci`. A commit that changes
behavior must land in the same PR as the tests that prove it and the
documentation status change that reports it (NFR-MAINT-004). PR descriptions
name the requirement IDs the change advances and the gate that owns them.

### 3.4 Release-blocking rules

- The gate-evidence workflows for every gate at or below the release tier
  must be green on the release SHA (§20).
- `nightly.yml` failures open a triage issue automatically; a nightly job red
  for more than 3 consecutive runs blocks merging changes to the subsystem
  that owns it until triaged.
- A quarantined flaky test (§11.6) blocks the gate that owns its family.

## 4. Exact Developer Bootstrap

From a clean Tier 1 or Tier 2 machine to green tests:

1. Install Git 2.40+ and the `gh` CLI 2.40+ via the platform package
   manager, then authenticate: `gh auth login --scopes repo,workflow`.
2. Install any Go 1.21+ bootstrap toolchain (`brew install go` or the
   linux tarball); the `toolchain` directive in `go.mod` makes `go`
   download and use the exact pinned 1.23.x automatically. Verify:

   ```sh
   go version
   ```

3. Clone:

   ```sh
   git clone https://github.com/Zachshotamartin/conduit.git
   cd conduit
   ```

4. Bootstrap the pinned developer tools:

   ```sh
   make bootstrap
   ```

   `make bootstrap` performs exactly: `go mod download` against the vendored
   module set; installation of the pinned versions of `staticcheck`,
   `golangci-lint`, `govulncheck`, `benchstat`, `syft`, and `cosign` into
   `./bin`; verification that each installed tool reports its pinned
   version; and a preflight report of Docker/Podman availability and the
   file-descriptor limit. It writes nothing outside the repository and
   `$GOPATH`.
5. Run the deterministic suites (no containers, no network):

   ```sh
   make test
   ```

   `make test` runs all unit and deterministic in-process integration
   suites with `-race` and `-shuffle=on`, printing the shuffle seed
   (NFR-MAINT-006). It must pass on a machine with no Docker, no NATS, and
   no PostgreSQL.
6. Run the static and architecture checks:

   ```sh
   make check
   ```

   `make check` runs, in order: `go vet ./...`; `./bin/staticcheck ./...`;
   `go run ./internal/tools/arch-check` (import-boundary rules,
   NFR-MAINT-001); `./bin/golangci-lint run`; the docs-status lint
   (forbidden-phrase scan, NFR-MAINT-004); and the metrics-contract test
   (§16.4).
7. Start a container runtime (Docker Desktop, `dockerd`, or
   `podman machine start`), then run the protocol conformance suite against
   the unmodified reference client:

   ```sh
   make conformance
   ```

   `make conformance` builds the Node 20 fixture container (pinned
   `graphql-ws` reference client plus the scripted scenario driver), starts
   a Conduit test binary on a loopback listener, and runs the full
   conformance scenario set (FR-SUB-002, NFR-COMPAT-001).
8. Run the container-backed integration suites:

   ```sh
   make integration
   ```

   `make integration` uses testcontainers to start pinned NATS and
   PostgreSQL images and runs the broker- and database-backed suites
   (FR-GQL-004, FR-FAN family broker rows). It skips with a hard error, not
   a silent pass, when no container runtime is present.
9. Optional, not required for any evidence through R8 except broker-specific
   rows: bootstrap the cluster/load harness (`make cluster-up` creates a
   3-node kind fleet with a NATS deployment; `make cluster-down` removes
   it). R5 fleet rows, CHAOS cluster rows, R9 load rows, and R10 deploy
   rows are the only consumers.

A bootstrap that ends with steps 5–6 green is sufficient to develop every
deterministic surface. Steps 7–8 are required before merging changes to the
transport, adapter, or bus packages. Step 9 is required only for gate
evidence that names the cluster harness.

## 5. Dependency and Supply-Chain Policy

### 5.1 Runtime dependency budget (NFR-MAINT-005)

The complete direct runtime dependency set fits on one screen and is
enumerated here normatively:

| Dependency | Purpose | Confinement |
| --- | --- | --- |
| `github.com/coder/websocket` | WebSocket I/O | Imported only by `internal/transport` (arch-checked, ADR-0001) |
| `github.com/vektah/gqlparser/v2` | GraphQL lexing, parsing, spec validation | Imported only by `internal/graphql/ast` (arch-checked, ADR-0003) |
| `github.com/nats-io/nats.go` | Reference bus adapter | Imported only by `internal/bus/nats` (ADR-0004) |
| `github.com/jackc/pgx/v5` | PostgreSQL data-source adapter | Imported only by `internal/source/postgres` (FR-GQL-004) |
| OpenTelemetry Go SDK + Prometheus exporter | Metrics and traces | Imported only by `internal/telemetry` (ADR-0010) |
| `github.com/lestrrat-go/jwx/v2` (or an equivalent maintained JOSE library passing the same review) | JWT/JWKS validation | Imported only by `internal/auth/oidc` (FR-AUTH-001) |
| `gopkg.in/yaml.v3` | Configuration parsing | Imported only by `internal/config` (FR-OPS-002) |

Everything else at runtime is the Go standard library. Test-only
dependencies (testcontainers, property-testing library) are excluded from
the budget but pass the same review. Any addition to this table is a
reviewable contract change to this document, not a patch detail.

### 5.2 Review checklist per addition (BUILD_PLAN §4.6, NFR-SEC-010)

Every new or upgraded dependency PR must answer, in the PR description:

1. What capability does it provide that the standard library or an existing
   dependency does not?
2. Maintenance signal: release cadence, open CVE history, bus-factor read of
   the maintainer set.
3. Transitive closure: `go mod graph` delta attached; each new transitive
   module named and license-checked.
4. License: on the allowlist (§5.4)?
5. Confinement: which single package imports it, and is the arch-check rule
   added in the same PR?
6. Upgrade test corpus: what suite proves behavior is unchanged after a
   version bump (for gqlparser: the bounded-input and spec-execution corpus;
   for nats.go: the broker integration suite)?

### 5.3 Pinning and update policy

- All modules are pinned by `go.mod`/`go.sum` and vendored; CI builds with
  `-mod=vendor` so the build never consults a proxy.
- Renovate runs weekly, opening one PR per dependency with the changelog
  diff. Renovate PRs merge only after the full PR pipeline plus the owning
  integration suite pass; no auto-merge for runtime dependencies.
- Toolchain (Go patch releases) and base-image digests are updated through
  the same Renovate flow.

### 5.4 Vulnerability scanning and license allowlist

- `govulncheck ./...` runs in the `pr / deps-audit` job on every PR and
  nightly. Findings that are reachable from Conduit code fail the job.
- Triage SLAs from first detection: critical — fix or mitigate within 24
  hours and before any release; high — 72 hours; medium — 14 days; low —
  next scheduled dependency sweep. Every triage decision is a tracked issue
  labeled `vuln-triage` citing the Go vulnerability ID.
- License allowlist: Apache-2.0, MIT, BSD-2-Clause, BSD-3-Clause, ISC.
  MPL-2.0 requires explicit maintainer review recorded in the PR. GPL, LGPL,
  AGPL, SSPL, and source-unavailable licenses are forbidden for anything
  linked into the binary. The `deps-audit` job verifies the allowlist with a
  license scanner over the vendored tree.

## 6. Local Data, Configuration, and Secret Locations

### 6.1 No durable node state by design

A Conduit node persists nothing durable except its logs (stdout/stderr JSON,
captured by the operator's log pipeline). The connection registry, predicate
index, replay buffers, revocation set, and dedupe window are in-memory and
die with the process (ADR-0005, ADR-0007). Uninstall and purge (§14.6)
therefore reduce to removing the binary, configuration, TLS material, key
files, bus subjects, and credentials — the PKG-007 row proves the inventory
is complete (FR-OPS-011).

### 6.2 Configuration file search order

`conduit serve` and `conduit validate` resolve the configuration file in
this exact order, stopping at the first hit:

1. `--config <path>` flag;
2. `$CONDUIT_CONFIG` environment variable;
3. `./conduit.yaml` in the working directory;
4. `/etc/conduit/conduit.yaml`.

Precedence for values, lowest to highest: built-in defaults < config file <
environment (`CONDUIT_*`) < flags (PRODUCT_REQUIREMENTS §5.3). Environment
keys map to config paths by uppercasing and joining with underscores
(`CONDUIT_LISTENER_CLIENT_PORT` sets `listener.client.port`); the mapping is
generated from the config schema so it cannot drift (UNIT-001 covers it).

### 6.3 TLS material

- Client-listener certificate and key: default paths
  `/etc/conduit/tls/client.crt` and `/etc/conduit/tls/client.key`,
  configurable; reloaded on SIGHUP or admin trigger without dropping
  established connections (FR-CONN-013, CONN-008 row).
- Admin-listener material and optional mTLS client CA:
  `/etc/conduit/tls/admin.crt`, `/etc/conduit/tls/admin.key`,
  `/etc/conduit/tls/admin-ca.crt` (FR-ADMIN-001).
- Bus TLS credentials for NATS: `/etc/conduit/tls/bus.crt`,
  `/etc/conduit/tls/bus.key`, plus the NATS credentials file at
  `/etc/conduit/bus.creds` (NFR-SEC-005).

All paths above are conventions shipped in the reference manifests; every
one is configurable and validated at startup (§7 phase 6).

### 6.4 Resume-token HMAC key file and rotation (FR-RESUME-002, NFR-SEC-007)

The resume-token signing keys live in one YAML key file, default
`/etc/conduit/keys/resume-hmac.yaml`, mode 0600, containing an ordered list
of `{kid, key_base64, not_after}` entries. The first entry signs; all
entries verify. Rotation procedure:

1. Generate a new 32-byte key: `openssl rand -base64 32`.
2. Prepend it to the key file with a new `kid` on every node (configuration
   management or mounted secret update).
3. Reload each node (`SIGHUP` or `POST /admin/v1/config/reload`); the node
   logs the active `kid` set.
4. After the maximum resume-token age (default 24 h) has elapsed
   fleet-wide, remove the retired entry and reload again.

Tokens signed by any listed key verify during the overlap (RESUME-011 row);
a token signed by a removed key is rejected as expired-key with a typed
error and a `resume_rejected` notice (FR-RESUME-007). Key bytes never appear
in logs, diagnostics, or `/admin/v1/config` output (NFR-SEC-004; canary row
UNIT-014).

## 7. Configuration and Startup Validation

### 7.1 Validation phases (FR-OPS-002, FR-GQL-001)

Startup validation runs these phases in order, failing fast at the first
error and naming file, key or SDL location, and the violated rule:

1. **File parse**: YAML syntax; unknown top-level keys are errors, not
   warnings.
2. **Schema validation**: every key checked against the versioned
   configuration schema — types, ranges, enum membership, required keys.
3. **Cross-field validation**: bounds coherence (outbound queue byte bound ≥
   largest permitted message; drain window > 0; keepalive < idle timeout;
   warning window < token lifetime floor), tenant-mode coherence
   (multi-tenancy off forbids per-tenant schema sets), and
   plaintext-acknowledgment rules (FR-CONN-013, FR-AUTH-004).
4. **SDL and binding validation**: parse and spec-validate every SDL file;
   validate every directive argument; verify every field's `@source` binding
   names a configured source; reject `@filterable` on unsupported types
   (FR-FILT-003); reject unbound fields and unknown directives.
5. **Auth rule validation**: every `@auth(rule:)` reference resolves to a
   defined rule; every rule expression compiles against the principal model
   (FR-AUTH-008); `auth.mode: none` requires
   `development_acknowledged: true` and logs a startup warning
   (FR-AUTH-004).
6. **Listener and TLS validation**: ports bindable-in-principle (well-formed,
   non-conflicting), certificate/key pairs parse and match, `trusted_proxy`
   mode requires a non-empty proxy allowlist, admin listener never shares
   the client port (FR-ADMIN-001).
7. **Bus reachability probe (optional)**: when
   `bus.validate_reachability: true`, attempt one bounded connect to the
   configured bus and report the result; the probe never blocks startup
   beyond its own timeout and is informational for `conduit validate`.

### 7.2 `conduit validate` and `conduit doctor`

- `conduit validate --config /etc/conduit/conduit.yaml` runs phases 1–6
  identically to `conduit serve` (same code path, architecturally shared)
  and phase 7 when enabled, printing every error with file, key, and rule,
  and exits nonzero on any error. It performs no network listen and mutates
  nothing.
- `conduit doctor --config /etc/conduit/conduit.yaml` checks the
  environment without mutating anything (FR-OPS-013): file-descriptor soft
  and hard limits against the configured connection ceiling; clock
  synchronization (warns when the reported offset exceeds the configured
  JWT clock skew of ±30 s, FR-AUTH-001); bus reachability and
  round-trip time; TLS material validity windows and key/cert match; key
  file permissions (rejects world-readable key files); and kernel socket
  parameters relevant to accept rates. Findings are actionable sentences
  with the exact limit and the exact observed value; exit code is nonzero
  when any check fails at `error` severity.

### 7.3 Effective-configuration hash

At startup the node computes a SHA-256 over the merged, canonicalized,
secret-redacted effective configuration and logs it as
`config_hash` at `INFO`. `/admin/v1/config` returns the same hash with the
redacted configuration (FR-ADMIN-006). Two nodes serving the same fleet role
must log the same hash; the CHAOS-008 row asserts the hash changes on reload
and is stable across restarts with identical inputs.

## 8. Test Policy and Harness Rules

### 8.1 Test-first workflow

Every ticket follows BUILD_PLAN §4.1: the failing test lands first (RED),
the minimal implementation second (GREEN), refactor third, and the coverage
floor of 80% on non-generated code holds per package (NFR-MAINT-002).
Mutation testing runs on the authorization and predicate packages on `main`
and at release (NFR-MAINT-002); surviving mutants in enforcement branches
are release-blocking for the owning gate.

### 8.2 Determinism rules (NFR-MAINT-006)

- **Injected clocks**: every timeout, timer-wheel, expiry, jitter, and
  pacing behavior is driven by an injected clock interface; deterministic
  suites advance it explicitly. Wall-clock sleeps for correctness are
  forbidden in all suites; bounded polling with deadlines is permitted only
  in container and cluster suites.
- **Seeded randomness**: every randomized generator (property tests, jitter,
  shuffle) takes a seed; the seed is logged on every run and printed on
  failure so any failure replays exactly.
- **Race detector on**: all unit and deterministic integration suites run
  with `-race`, always, in CI and in `make test`. Container suites run
  `-race` where the runtime cost fits the budget class; the exceptions are
  named in the family intro.
- **No shared mutable state**: each test receives unique temporary
  directories, loopback ports, tenant IDs, and bus namespaces; a suite-level
  leak detector fails the run on leaked goroutines, open sockets, or
  temporary files at exit.
- Tests never weaken production validation to obtain deterministic output.

### 8.3 Flake policy classes

- **Deterministic suites (harness classes a and b)**: zero tolerated flakes.
  A flake is by definition a bug — in the code or the test — and opens a
  bug ticket that blocks the gate owning the affected family until root
  cause is fixed. No retries, ever.
- **Container suites (harness class c)**: one automatic retry is permitted
  per job to absorb container-runtime startup faults; every retry that
  passes opens a mandatory triage issue labeled `flake-triage` with the
  captured logs, due within the SLA in §11.6. Two consecutive retried runs
  of the same test quarantine it (§11.6).
- **Load and chaos suites (harness class d)**: acceptance is statistical —
  each row defines its acceptance band — and there are no retries without a
  recorded cause attached to the run artifact. An out-of-band result is a
  finding, not a reroll.

### 8.4 The four harness classes

**(a) Deterministic in-process harness.** Composition: the memory bus with
scripted fault injection (partition, delay, reorder, duplication —
ADR-0004), a fake clock, scripted in-process clients speaking the protocol
message types directly to the connection state machine, in-memory data-source
stubs, and a synthetic authorizer with scriptable decisions and epochs.
*May prove*: protocol state-machine behavior, matching equivalence,
authorization decisions and epoch caching, fanout ordering/dedupe/partition
logic, backpressure policies, resume splice correctness, quota accounting —
every behavior whose contract is independent of real sockets and real
brokers. *May not prove*: real WebSocket framing, TLS, broker-specific
semantics, kernel-level backpressure, memory-per-connection figures, or any
latency number. Runtime budget class A: ≤ 90 s per package, whole class ≤ 8
min. Gates CI on every PR. Flake policy: deterministic — zero tolerance
(§8.3).

**(b) Socket-level harness.** Composition: a real Conduit listener on
loopback (TLS and plaintext-acknowledged modes), the Conduit-owned scripted
client (`conduit-wsprobe`, §9.2) driving real WebSocket frames, and the
hostile client (malformed frames, oversized payloads, slow reads/writes,
init floods — FR-SUB-012). *May prove*: real framing, message bounds at the
socket, close-code emission on the wire, TLS handshake and reload,
slow-read/slowloris behavior, fd accounting. *May not prove*: reference-
client compatibility (that needs class c), broker behavior, fleet behavior,
scale numbers. Runtime budget class B: ≤ 5 min for the suite. Gates CI on
every PR (job `integration / socket-hostile`). Flake policy: deterministic —
zero tolerance; the harness synchronizes on explicit readiness events, never
sleeps.

**(c) Container harness.** Composition: testcontainers-managed pinned
images — the Node 20 reference-client fixture running the unmodified
`graphql-ws` client, NATS, and PostgreSQL — against a real Conduit binary.
*May prove*: reference-client conformance (NFR-COMPAT-001), broker-specific
bus behavior (reconnect, slow-consumer signals, restart), real relational
adapter behavior (FR-GQL-004). *May not prove*: fleet behavior under node
loss (needs class d), performance claims (needs BENCHMARK_PLAN
environments). Runtime budget class C: ≤ 25 min for the full workflow. Gates
CI on every PR for the conformance and single-broker suites; the broker
version matrix runs nightly. Flake policy: container — one retry with
mandatory triage issue (§8.3).

**(d) Cluster/load harness.** Composition: a kind-based 3-node Conduit fleet
with a NATS deployment and a standard ingress LB, plus `conduit-loadgen`
(the load generator whose workload definitions are shared with
BENCHMARK_PLAN §workloads) and fault injectors (node kill, network
partition via network policies, bus restart). *May prove*: fleet fanout,
node-loss and partition behavior, revocation propagation SLO fleet-wide,
drain and rolling-deploy behavior, reconnect storms, and — on the
benchmark-designated hardware only — the R9 scale numbers. *May not prove*:
anything deterministic classes already own; a cluster pass never substitutes
for a missing deterministic row. Runtime budget class D: minutes to hours;
runs nightly and on gate-evidence demand, never on PR. Flake policy:
statistical acceptance bands per row, no retries without recorded cause
(§8.3).

## 9. Shared Harnesses and Oracles

### 9.1 Linear-scan differential oracle (FR-FILT-007)

The linear-scan matcher is retained permanently in the tree
(`internal/match/scan`) as the differential oracle: property tests generate
subscriptions and envelopes across the full predicate grammar (equality,
`in` with list sizes 0–100, `gt`/`gte`/`lt`/`lte`/`between` on numbers and
timestamps, boolean presence, conjunctions, disjunctions up to the
8-conjunction bound) and assert exact candidate-set equality between index
and scan (INDEX-001). It is also the benchmark baseline (FR-FILT-010) and is
never selectable in production configuration (ADR-0006); the arch-check
enforces that only test and benchmark packages import it.

### 9.2 Protocol scripted client (`conduit-wsprobe`)

A Conduit-owned test client in `internal/testing/wsprobe` that speaks
`graphql-transport-ws` frame-by-frame under script control: exact frame
bytes, deliberate ordering violations, timed silences against the fake or
real clock, partial writes, and capture of every received frame and the
close code/reason. It backs both harness class b and the deterministic PROTO
rows. It is explicitly not evidence for FR-SUB-002 — only the unmodified
reference client in harness class c is.

### 9.3 Fault-injectable memory bus (`bus/memory`)

The deterministic in-process `Bus` implementation (ADR-0004) exposes a fault
API used by FAN, RESUME, and AUTHZ rows: `Partition(nodeSet)`,
`Heal()`, `Delay(subjectPattern, d)`, `Reorder(subjectPattern, window)`,
`Duplicate(subjectPattern, n)`, and `DropNext(subjectPattern, n)`. Every
fault is scripted against the fake clock so partition-and-heal sequences are
exactly reproducible. Control-subject faults drive the degraded-mode rows
(FR-AUTH-015, FR-FAN-006).

### 9.4 Stalled-consumer fixture

A scripted client mode (in both wsprobe and the reference-client fixture
driver) that completes the handshake, subscribes, then stops reading the
socket entirely or reads at a scripted trickle rate. BP rows use it to force
queue-full conditions; SOAK-005 uses hundreds of them concurrently with heap
assertions (NFR-SCALE-002 boundedness under FR-CONN-007/008).

### 9.5 Canary secret corpus (NFR-SEC-004, NFR-SEC-009)

Each run generates unique synthetic canaries — JWT-shaped tokens, API keys,
resume-token bytes, HMAC key bytes, JWKS key material, PostgreSQL
credentials, NATS credentials — in raw, base64, URL-encoded,
JSON-escaped, and split-fragment forms. The harness plants them in
credentials, configuration, data-source responses, and hostile payloads,
then scans every sink: logs, error responses, `Next` payload extensions,
metrics text, trace attributes, `/admin/v1/config`, `/admin/v1/diagnostics`
bundles, and CI artifacts. Any hit fails the run and is treated as a
security bug (§17).

### 9.6 Fixed workload definitions (shared with BENCHMARK_PLAN)

`conduit-loadgen` executes named workloads whose parameters are defined once
in BENCHMARK_PLAN §workloads and vendored as machine-readable specs under
`bench/workloads/`; this plan references them by name and never redefines
their parameters:

- `W-IDLE`: authenticated connections, one subscription each, keepalive
  only (NFR-SCALE-001/002).
- `W-REF`: the reference mixed load — connections, subscriptions per
  connection, publish rate, match selectivity, payload sizes per
  BENCHMARK_PLAN (NFR-PERF-001, NFR-SCALE-005).
- `W-CHURN`: connection and subscription churn at the accepts/s target
  (NFR-SCALE-006).
- `W-BURST`: publish bursts against slow consumers (FR-CONN-008 policies).
- `W-STORM`: node-loss reconnect surge and expiry storm profiles
  (FR-RESUME-009, ADR-0008 consequences).

## 10. Detailed Verification Matrices

Each matrix row is a required named test family. "Pass" means the stated
oracle holds on every assigned platform with the leak detector clean.
"Earliest gate" is the first gate that must pass the row; every later gate
keeps it as regression coverage. Every family intro states its harness,
fixture, runtime budget, CI-versus-nightly placement, and flake policy — no
family runs without all five stated.

### 10.1 UNIT — pure-logic and codec matrix

Harness: class a (deterministic in-process; no sockets, fake clock, seeded
randomness). Fixtures: checked-in configuration corpora under
`testdata/config/`, SDL corpora under `testdata/sdl/`, hostile documents
under `testdata/adversarial/`, and the canary corpus (§9.5). Runtime budget:
class A, ≤ 90 s per package. Placement: gates CI on every PR
(`pr / unit-race`, `-race` on). Flake policy: deterministic — zero tolerated
flakes; any flake opens a blocking bug ticket for the owning gate.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| UNIT-001 | Parse valid, unknown-key, wrong-type, out-of-range, and cross-field-incoherent configurations through phases 1–3 of §7.1; verify env-var mapping and precedence (defaults < file < `CONDUIT_*` < flags). | Every error names key, source, and expectation (FR-OPS-002); precedence is exact; unknown top-level keys fail; no partial config object escapes a failed parse. | R0 |
| UNIT-002 | Validate SDL corpora: unknown directives, unbound fields, bad directive arguments, `@filterable` on unsupported types, `@auth` referencing undefined rules, duplicate type definitions. | Startup refuses with file, line, and rule named (FR-GQL-001, FR-FILT-003, FR-AUTH-008); valid corpora load; no error message leaks internal paths. | R1 |
| UNIT-003 | Feed operation documents at and beyond the byte (1 MiB), token (20,000), and parse-depth bounds, including a document crafted to allocate before bounds if checks are misordered. | Exceeding any bound is a typed rejection that allocates no AST (FR-GQL-011, NFR-SEC-001); at-bound documents parse; allocation is asserted with `testing.AllocsPerRun`. | R1 |
| UNIT-004 | Compute complexity with `@complexity` costs and multipliers across nested lists, defaults (cost 1/field, ceiling 10,000), and rejection paths. | Cost computation matches the documented formula; rejected operations return the computed cost in `extensions` (FR-GQL-009). | R1 |
| UNIT-005 | Validate depth limiting at, below, and beyond the default 15, including fragment-cycle and inline-fragment nesting attempts. | Exceeding depth fails validation before execution with a typed error (FR-GQL-008); fragment cycles are rejected by spec validation. | R1 |
| UNIT-006 | Format execution errors for resolver failure, timeout, denied field, and adapter errors carrying canary-laced upstream bodies. | Output is spec-shaped (`errors`, `path`, `locations`, `extensions.code`) and contains no SQL, stack traces, internal addresses, or canaries (FR-GQL-012, NFR-SEC-004). | R1 |
| UNIT-007 | Compile subscription arguments to predicates across the full grammar: equality, `in` (sizes 0, 1, 100, 101), ordered comparisons, `between`, boolean presence, conjunctions, disjunctions expanding to 1–9 conjunctive entries. | Compilation failures reject before registration with typed errors (FR-FILT-001); `in` beyond 100 and disjunction beyond 8 entries reject naming the bound (FR-FILT-002, FR-FILT-005); type mismatches are subscribe-time errors (FR-FILT-004). | R4 |
| UNIT-008 | Round-trip the resume-token codec: encode/decode with each listed key, tamper every field, oversize beyond 512 bytes, age beyond maximum, wrong `kid`. | Valid tokens round-trip; every tampered/oversized/expired/unknown-kid token fails constant-time verification with a typed reason (FR-RESUME-002, NFR-SEC-007); verification time is input-independent within measurement noise. | R7 |
| UNIT-009 | Encode/decode publish envelopes: current version, unknown version, missing fields, wrong types, oversized attribute maps, canary payload bytes. | Unknown versions are rejected and counted, never partially interpreted (FR-FAN-002); codec never copies payload bytes into error strings. | R5 |
| UNIT-010 | Exercise the close-code mapping table: every internal close reason maps to exactly one code from {4400, 4401, 4403, 4406, 4408, 4409, 4429, 4700, 4701, 4702, 4703, 4704} plus normal closure. | The mapping is total and injective per reason class; no path yields an undocumented code (FR-SUB-010); the table equals PROTOCOL_CONFORMANCE §6 by generated comparison. | R2 |
| UNIT-011 | Drive quota accounting: per-principal and per-tenant connection counts, per-connection (default 100) and per-principal subscription counts, token-bucket rate limiter (50 msg/s burst 100) across boundary sequences under the fake clock. | Counts never go negative, race-free under `-race` with concurrent add/remove; at-limit admits, over-limit rejects with the right category (FR-CONN-004/005/006). | R6 |
| UNIT-012 | Drive the shared timing wheel with 100,000 synthetic timers (keepalive, idle, expiry, lifetime) advancing the fake clock across wraps and cancellations. | Fires occur in order within tick tolerance; cancellation is exact; no per-connection `time.Timer` allocation occurs (ADR-0001); allocation asserted. | R2 |
| UNIT-013 | Extract coalesce keys from event payloads: present key, missing key, wrong-typed key, nested path, oversized value. | Extraction is total: missing/invalid keys yield the documented fallback (treat as `drop_oldest` for that event, counted) and never panic (FR-CONN-008). | R6 |
| UNIT-014 | Run the redaction pass over every sink formatter with the canary corpus (§9.5) in credentials, config dumps, errors, and diagnostics inventories. | Zero canary bytes or reversible encodings survive in any sink; redaction is idempotent; safe structural metadata remains (NFR-SEC-004, NFR-SEC-009). | R3 |
| UNIT-015 | Validate subscription-ID bounds: empty, 1-byte, 255-byte, 256-byte, non-UTF-8, and control-character IDs on `Subscribe`. | IDs are opaque strings bounded at 255 bytes (FR-SUB-005); over-bound and malformed IDs produce the documented typed rejection; no ID bytes echo into close reasons (FR-SUB-008). | R2 |
| UNIT-016 | Map every process-exit path — validate failure, doctor failure, bind failure, drain-complete, fatal — to exit codes. | Each category has one stable documented exit code; `conduit validate` exits nonzero on any error (PRODUCT_REQUIREMENTS §5.3); codes are asserted from a table shared with the docs. | R0 |
| UNIT-017 | Coerce variables against declared types: unknown, missing-required, type-mismatched, null-for-non-null, nested input objects. | The whole operation fails with locations before execution (FR-GQL-013); coercion matches the October 2021 rules for supported features (NFR-COMPAT-002). | R1 |
| UNIT-018 | Construct principals from each auth mode's output and attempt post-construction mutation via retained references. | The principal is immutable per connection, carries subject/tenant/scopes/claims/expiry/mode/epoch, and never contains raw credentials (FR-AUTH-005); mutation attempts do not alter the registered principal. | R3 |

### 10.2 PROTO — protocol conformance matrix

Harness: rows PROTO-001 through PROTO-010 run on class a (scripted
in-process client against the state machine, fake clock) and re-run on class
b (real sockets via `conduit-wsprobe`); PROTO-011 and PROTO-012 additionally
require class c (unmodified reference client in the Node fixture container).
Fixtures: the protocol scenario corpus under `testdata/proto/`, the hostile
frame corpus under `testdata/adversarial/frames/`. Runtime budget: class A
for in-process, class B for socket, class C for the reference-client rows
(the conformance job ≤ 12 min of the integration budget). Placement: classes
a/b gate CI on every PR (`pr / proto-race`, `integration / socket-hostile`);
class c gates CI in `integration / conformance-node`. Flake policy:
deterministic rows — zero tolerance; conformance-node — container policy
(one retry, mandatory triage issue).

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| PROTO-001 | Exhaustively drive the typed state table: every (state, message) pair including all illegal pairs, generated from the table itself so no pair is skipped. | Legal transitions produce the specified state and emissions; illegal pairs are structurally unrepresentable or rejected with the mapped close code; the test enumerates the full cross product (FR-SUB-011). | R2 |
| PROTO-002 | Send inbound messages at 512 KiB − 1, 512 KiB, and 512 KiB + 1; configure the WebSocket library frame limit and verify no larger frame is buffered (class b). | Over-bound closes 4400; at-bound processes; heap growth during the oversized send stays under the read-buffer bound (FR-SUB-009, NFR-SEC-001). | R2 |
| PROTO-003 | Advance the fake clock across keepalive intervals with and without client `Pong`; send unsolicited `Pong` and client-initiated `Ping`. | Server pings at 25 s intervals; `Pong` in either direction is handled; unsolicited `Pong` is legal and ignored; missing traffic feeds the idle path, never a silent RST (FR-SUB-007). | R2 |
| PROTO-004 | Open a connection and send nothing for the init timeout (3 s fake-clock). | Close 4408 exactly at timeout; no `connection_ack` ever sent (FR-SUB-003). | R2 |
| PROTO-005 | Send a second `connection_init` after ack, and concurrent duplicate inits in the same read burst. | Close 4429 in both shapes; the first init's state is not corrupted before close (FR-SUB-003). | R2 |
| PROTO-006 | Send `Subscribe`, `Ping`-payloadless variants, and `Complete` before `connection_ack`. | Any non-init message before ack closes 4401 (FR-SUB-003); nothing registers in the registry (FR-AUTH-006 ordering). | R2 |
| PROTO-007 | Subscribe twice with the same ID while the first is active; subscribe again with the same ID after `Complete`. | Active-ID reuse closes 4409 with reason `Subscriber for <id> already exists`; reuse after completion succeeds (FR-SUB-005). | R2 |
| PROTO-008 | Execute a query and a mutation over the WebSocket via `Subscribe`/`Next`/`Complete`, comparing results, limits, deadlines, and authorization outcomes against the identical HTTP operation. | Byte-comparable `ExecutionResult` payloads and identical limit/deadline/authz behavior; one shared executor path is asserted architecturally (FR-GQL-015). | R2 |
| PROTO-009 | Run the unmodified reference client through connect, subscribe, receive, complete, keepalive, server `Error`, and server `Complete` while Conduit emits every `extensions.conduit` and `ping.payload.conduit` extension. | The reference client completes every scenario untouched by the extensions; extension invisibility is asserted by scenario success plus client-log scan (NFR-COMPAT-001, PRODUCT_REQUIREMENTS §5.4). | R2 |
| PROTO-010 | Send malformed frames from the corpus: invalid JSON, unknown `type`, missing fields, wrong field types, binary WebSocket frames, and fuzz-derived regressions. | Every malformed frame closes 4400 with a reason echoing no client bytes (FR-SUB-008); the node survives the full corpus in one process with stable heap. | R2 |
| PROTO-011 | Offer the legacy `graphql-ws` subprotocol, no subprotocol, and an unknown subprotocol at upgrade; force a post-handshake mismatch. | Pre-handshake rejection is HTTP 400 naming the supported subprotocol; post-handshake closes 4406 (FR-SUB-001, ADR-0002); tested with a real legacy-client fixture. | R2 |
| PROTO-012 | Enumerate every close code in PROTOCOL_CONFORMANCE §6 with a scenario that provokes it end to end on a real socket: 4400, 4401, 4403, 4406, 4408, 4409, 4429, 4700, 4701, 4702, 4703, 4704. | Each code is observed on the wire with its documented reason class; the enumeration is generated from the table so adding a code without a scenario fails the build (FR-SUB-010). | R2 |
| PROTO-013 | Client sends `Complete` mid-stream during active delivery; server continues publishing matching events. | Delivery stops promptly, the entry is freed in the registry, and index entries are removed; no `Next` for that ID after the `Complete` is processed (FR-SUB-006, FR-CONN-001). | R2 |
| PROTO-014 | Server terminates one subscription via `Error` (typed GraphQL error array) and another via `Complete` while sibling subscriptions on the same connection stay live. | Only the targeted subscription ends; the connection and siblings are unaffected; `Error` is terminal for that ID (FR-SUB-006). | R2 |

### 10.3 AUTHZ — authentication and authorization matrix

Harness: class a with the synthetic authorizer and scripted JWKS/API-key
stores; AUTHZ-005/006/007 re-run on class b with real sockets; AUTHZ-013
runs on class d (fleet SLO measurement). Fixtures: JWT corpus (valid,
expired, `nbf`-future, wrong `iss`/`aud`, bad signature, rotated `kid`),
salted API-key store fixture, custom-authorizer stub server, canary corpus.
Runtime budget: class A; class B rows within the socket suite budget;
AUTHZ-013 within the nightly cluster window. Placement: gates CI on every PR
(`pr / authz-race`) except AUTHZ-013 (nightly cluster, release-blocking for
R5). Flake policy: deterministic rows — zero tolerance; AUTHZ-013 —
statistical band, no unrecorded retries.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| AUTHZ-001 | Validate the JWT corpus: signature against cached JWKS, `iss`, `aud`, `exp`, `nbf`, ±30 s skew boundaries, unknown then rotated `kid` with bounded JWKS refresh. | Every invalid token is rejected pre-ack with close 4403 and no failure-cause disclosure; rotation succeeds within the bounded refresh; refresh failures fail closed (FR-AUTH-001, FR-AUTH-018). | R3 |
| AUTHZ-002 | Present valid, expired, revoked, wrong-tenant, and forged API keys against the salted-hash store; scan memory-reachable sinks for plaintext. | Valid keys yield principals with tenant/scopes/expiry; all others reject uniformly; plaintext exists only in the presentation instant and never in any sink (FR-AUTH-002, NFR-SEC-004). | R3 |
| AUTHZ-003 | Drive the custom authorizer hook: versioned request/response round-trip, denial, malformed response, response timeout, and TTL expiry of a granted decision. | Timeout and malformed responses fail closed with typed errors; the versioned contract is validated both directions; TTL expiry forces re-decision (FR-AUTH-003). | R3 |
| AUTHZ-004 | Configure `auth.mode: none` with and without `development_acknowledged: true`; configure two modes on one listener. | Unacknowledged `none` refuses startup; acknowledged `none` logs the startup warning; at most one mode authenticates a connection (FR-AUTH-004). | R3 |
| AUTHZ-005 | Subscribe with a principal denied by the subscribe-time rule for the field/arguments. | Denial occurs at `SubscriptionAuthorizer.AuthorizeSubscribe` before any registry or index registration — asserted by instrumented registry/index showing zero writes (FR-AUTH-006, NFR-SEC-002). | R3 |
| AUTHZ-006 | Publish envelopes matching a live subscription whose principal the synthetic authorizer now denies. | `AuthorizePublish` runs on the subscriber's node against current grant state and the concrete payload before enqueue; the denied delivery never enqueues; no configuration disables the check for revocable modes (FR-AUTH-010, NFR-SEC-002). | R3 |
| AUTHZ-007 | Revoke a principal mid-subscription, then immediately publish matching events in the same scripted tick. | Publish-time checks fail closed from the application instant; affected subscriptions get `error` (`GRANT_REVOKED`); fully revoked principals close 4403; zero post-application deliveries — the named adversarial no-post-revocation-delivery test (FR-AUTH-013, ADR-0008). | R3 |
| AUTHZ-008 | Advance the fake clock to expiry minus the warning window, then to expiry, for a client that ignores the warning. | Warning `ping` carries `{"conduit":{"expires_in_ms":<n>}}` at the window; at expiry deliveries stop, typed `TOKEN_EXPIRED` errors are sent per subscription, and the connection closes 4403 (FR-AUTH-012, ADR-0008). | R3 |
| AUTHZ-009 | Populate the publish-time decision cache, then revoke, expire, and policy-reload in every interleaving with concurrent publishes under `-race`. | Every epoch-advancing event invalidates the cache before the next delivery decision; no stale-cache delivery in any interleaving (FR-AUTH-011, ADR-0008 caching). | R3 |
| AUTHZ-010 | Register tenant-A and tenant-B subscriptions with identical fields and predicates; publish tenant-A envelopes; probe with an instrumented index. | Tenant-A envelopes are matched only against tenant-A shards; no cross-tenant lookup path executes — structural assertion, not a runtime-check assertion (FR-AUTH-017, NFR-SEC-006, ADR-0009). | R3 |
| AUTHZ-011 | Partition the control subject via the memory bus, pass the 10 s heartbeat timeout under `fail_closed`, publish for revocable-mode principals, then heal. | Degraded mode is entered visibly (health, logs, metric); deliveries for revocable-auth principals suspend; heal resumes cleanly (FR-AUTH-015, FR-AUTH-016). | R3 |
| AUTHZ-012 | Repeat AUTHZ-011 under `fail_open_bounded` with a scripted staleness ceiling; run past the ceiling. | Deliveries continue only until the ceiling, then suspend; the policy is explicit configuration, logged, with no silent default change (FR-AUTH-015, FR-AUTH-016). | R3 |
| AUTHZ-013 | On the 3-node fleet, inject revocations via `/admin/v1/revocations` under `W-REF` load and measure admin-acknowledgement-to-node-application latency on all nodes across ≥ 500 revocations. | p99 application latency ≤ 2 s fleet-wide; the measurement method follows BENCHMARK_PLAN; the distribution is stored as gate evidence (FR-AUTH-014, NFR-SEC-003). | R5 |
| AUTHZ-014 | Compare client-visible failure bytes across: unknown principal, bad signature, revoked grant, denied rule, nonexistent field with introspection disabled. | Failures are indistinguishable beyond their typed category; no rule names, no field-existence oracle (FR-AUTH-018, FR-GQL-010). | R3 |
| AUTHZ-015 | Evaluate `@auth` field rules across queries and mutations: allowed, denied mid-selection, denied non-null field, denied list element. | Denied fields yield spec-shaped errors at the exact path with normal null propagation; sibling fields execute (FR-AUTH-007). | R3 |
| AUTHZ-016 | Capture the audit/decision trace for every decision in rows AUTHZ-005 through AUTHZ-008 and every admin revocation. | Each decision produces a structured record (allow/deny, rule, subject, decision point) within the logging budget; no string-interpolated policy input appears (FR-AUTH-009, FR-ADMIN-008). | R3 |

### 10.4 INDEX — predicate index matrix

Harness: class a; the differential oracle (§9.1) is the ground truth;
INDEX-010 additionally has a benchmark counterpart owned by BENCHMARK_PLAN.
Fixtures: seeded property generators over the full predicate grammar and
envelope attribute space; churn scripts. Runtime budget: class A, with the
property suite capped at 60 s per run (seed logged; the nightly run extends
iterations 20×). Placement: gates CI on every PR (`pr / index-race`);
extended-iteration property runs nightly. Flake policy: deterministic — zero
tolerance; a property failure ships its seed and minimized case in the
failure artifact.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| INDEX-001 | Property test: generate subscription sets (1–10,000 entries) and envelope streams across the full grammar; compare index candidate sets against the linear-scan oracle. | Exact candidate-set equality on every generated case; the generator provably covers every operator and type combination (coverage assertion over the generator's own output) (FR-FILT-007, ADR-0006). | R4 |
| INDEX-002 | Exercise equality and membership hash sub-indexes: value collisions, `in` lists at sizes 1/50/100, entry sharing across lists, removal correctness. | Candidate sets match the oracle; removal leaves no phantom entries; `in` beyond 100 was rejected upstream (FR-FILT-002). | R4 |
| INDEX-003 | Exercise interval sub-indexes: open/closed endpoints, `between`, equal endpoints, timestamp attributes, endpoint churn under sorted-array binary search. | Ordered-comparison candidates match the oracle at every boundary value; endpoint churn keeps lookups correct (ADR-0006). | R4 |
| INDEX-004 | Register conjunctive entries with K = 1…12 predicates; publish envelopes satisfying 0…K predicates. | Only counter-reaches-K entries join the candidate set; partial matches never leak (ADR-0006 counting algorithm). | R4 |
| INDEX-005 | Subscribe with disjunctions normalizing to 1–8 conjunctive entries, and one normalizing to 9. | 8 entries register and match equivalently to the oracle; 9 rejects with a typed error naming the bound (FR-FILT-005). | R4 |
| INDEX-006 | Run concurrent subscribe/unsubscribe/publish churn (seeded schedule, thousands of operations) under `-race` with epoch-snapshot reads on the publish path. | No race reports; every publish matches against a consistent epoch snapshot; matching never blocks on subscribe churn — asserted by scripted-lock instrumentation (FR-FILT-008, ADR-0006 concurrency). | R4 |
| INDEX-007 | Register non-indexable (custom-matcher) subscriptions up to the residual ceiling (default 1,000 per field) and one past it. | Residual entries scan linearly and match the oracle; the ceiling rejects further non-indexable subscribes with a typed error; residual length is a published metric (FR-FILT-006, FR-FILT-009). | R4 |
| INDEX-008 | Snapshot an epoch, mutate the index concurrently, and match against the snapshot; verify shard isolation per (tenant, field). | Snapshot reads are immutable; shard mutation is invisible to in-flight matches; cross-shard state never mixes (FR-FILT-008, ADR-0009). | R4 |
| INDEX-009 | Scrape metrics during INDEX-006/007 runs: entry count, residual length, shard sizes, match latency histogram, candidate-set size histogram. | All five metric families exist with documented names/labels, update correctly, and stay within the §16 cardinality budget (FR-FILT-009, FR-OPS-009). | R4 |
| INDEX-010 | Load 100,000 entries and run the match-cost microbenchmark alongside the linear scan; assert the CI regression ceiling (§12) while BENCHMARK_PLAN owns the published number. | Index beats the linear scan at and above 10,000 entries; p99 match ≤ 1 ms at 100,000 entries on the reference machine per BENCHMARK_PLAN method (FR-FILT-010, NFR-PERF-002, NFR-SCALE-004). | R4 |
| INDEX-011 | Subscribe with predicates whose types mismatch the schema (string against numeric attribute, list against scalar). | Every mismatch is a subscribe-time typed error, never a registered never-match entry (FR-FILT-004). | R4 |
| INDEX-012 | Unsubscribe and connection-teardown storms during active publish; count index entries after quiescence. | Zero orphan entries after teardown; counts return to exactly the pre-storm baseline (FR-CONN-001 interaction, FR-FILT-008). | R4 |

### 10.5 FAN — cross-node fanout matrix

Harness: class a with the fault-injectable memory bus for FAN-001 through
FAN-010; FAN-011 and FAN-012 re-run on class c against real NATS; node-loss
and fleet rows re-run on class d (marked). Fixtures: envelope corpora
(current and unknown versions), multi-node in-process fleet fixture (three
node instances sharing one memory bus), NATS testcontainer. Runtime budget:
class A deterministic; class C for broker re-runs (within
`integration / integration-nats`); class D for fleet re-runs (nightly).
Placement: deterministic rows gate CI on every PR; broker rows gate CI in
`integration.yml`; fleet re-runs nightly, release-blocking at R5. Flake
policy: deterministic — zero tolerance; broker rows — container policy;
fleet rows — statistical bands, no unrecorded retries.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| FAN-001 | Publish envelopes with the current version, an unknown future version, and a corrupted version field through the bus consumer path. | Unknown versions are rejected, counted in `conduit_publish_envelopes_total{stage="rejected_version"}`, and never partially interpreted; current versions flow (FR-FAN-002, NFR-COMPAT-003). | R5 |
| FAN-002 | Run a mutation whose resolver fails after its publish mappings are configured; run one that succeeds with two mappings. | Envelopes emit only after resolver success, one per mapping (FR-GQL-003, FR-FAN-001); the failed mutation emits zero envelopes. | R5 |
| FAN-003 | Publish interleaved streams from two publishers on one field and one publisher on two fields, through bus delay and reorder faults, to one subscriber. | Per-publisher, per-field order is preserved to the outbound queue; no cross-publisher or cross-field order is asserted — the test proves exactly the promised contract and nothing stronger (FR-FAN-004). | R5 |
| FAN-004 | Inject duplicates via the memory-bus `Duplicate` fault and via scripted publisher retry, within and beyond the 60 s dedupe window (fake clock). | In-window duplicates per (tenant, field, publish ID) deliver once and count as deduplicated; beyond-window duplicates deliver (documented bound, not a bug) (FR-FAN-008). | R5 |
| FAN-005 | Kill one node instance of the three-node in-process fleet mid-stream; on class d, `kill -9` a real pod under `W-REF`. | Surviving nodes deliver throughout with no interruption beyond reconnect load; no state migration occurs; killed node's clients recover per RESUME rows (FR-FAN-005, ADR-0005). | R5 |
| FAN-006 | Partition one node from the bus; publish locally on the partitioned node and remotely on the others. | The partitioned node continues local-publish-to-local-subscriber delivery, marks itself degraded in `/readyz` and metrics, and applies FR-AUTH-015 staleness policy (FR-FAN-006). | R5 |
| FAN-007 | Heal the partition from FAN-006; observe post-heal consumption. | The node resumes bus consumption with no replay of partition-era envelopes; the gap is honest — resume/gap rules apply to affected clients (FR-FAN-006, ADR-0007). | R5 |
| FAN-008 | Induce bus backlog via a scripted slow bus consumer; on class c, induce a real NATS slow-consumer condition against bounded pending limits. | Envelopes drop with an explicit counter, a health signal, and a log record; memory stays bounded; no unbounded buffering anywhere (FR-FAN-007, ADR-0004). | R5 |
| FAN-009 | Configure a node serving tenants A and B in a fleet whose bus also carries tenant C subjects; inspect subscriptions and attempt a crafted cross-tenant subject publish. | The node subscribes only to `conduit.A.…` and `conduit.B.…`; tenant-C envelopes never reach its matcher; subject scoping is verified against the broker's subscription list on class c (FR-FAN-009, ADR-0009). | R5 |
| FAN-010 | Inject envelopes via `/admin/v1/publish` — valid, schema-invalid, wrong-tenant, and rate-limited cases. | Admin publishes traverse the identical validation, matching, and authorization path as mutation publishes — asserted by shared-path instrumentation; audit records are produced (FR-FAN-010, FR-ADMIN-008). | R5 |
| FAN-011 | Drive per-tenant publish token buckets to exhaustion via mutations and admin publishes. | Exceeding the limit rejects the mutation's publish step with a typed error naming the limit class; the bucket refills per configuration (FR-FAN-011, PRODUCT_REQUIREMENTS §8). | R5 |
| FAN-012 | Scrape fanout metrics during FAN-003/004/008: published/received/matched/deduplicated/dropped counters, per-stage latency, bus connection state. | Every FR-FAN-012 metric exists, is accurate against scripted expectations, and respects the §16 budget. | R5 |
| FAN-013 | Restart the NATS container mid-stream (class c) with active publishers and subscribers. | The bus adapter reconnects with bounded backoff; disconnection is visible in `conduit_bus_connection_state` and health; delivery resumes; the outage window is honest (drops counted, gaps announced) (FR-FAN-006, FR-FAN-007, ADR-0004). | R5 |
| FAN-014 | Compare local-subscriber and remote-subscriber delivery for the same envelope on the in-process fleet. | Local and remote subscribers receive equivalent treatment — same authorization path, same envelope contents, same extension metadata (FR-FAN-001, FR-FAN-003). | R5 |

### 10.6 BP — backpressure and queue-bound matrix

Harness: class a with the stalled-consumer fixture (§9.4) and fake clock;
BP-008 re-runs on class b with real sockets and OS-level socket buffers.
Fixtures: `@backpressure`-annotated SDL corpus, burst scripts, stalled and
trickle consumer modes. Runtime budget: class A; BP-008 within class B.
Placement: gates CI on every PR. Flake policy: deterministic — zero
tolerance.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| BP-001 | Fill a connection's outbound queue to the 256-message bound against a stalled consumer with `drop_oldest`; continue publishing. | The oldest queued `Next` for that subscription evicts per enqueue-over-bound; newest events are retained; drops are counted per subscription (FR-CONN-007, FR-CONN-008, FR-CONN-009). | R6 |
| BP-002 | Repeat BP-001 hitting the 1 MiB byte bound before the message bound with large payloads. | Whichever bound binds first triggers the policy; accounting is byte-exact; no queue exceeds either bound at any instant (FR-CONN-007). | R6 |
| BP-003 | Fill the queue under `coalesce_by_key` with events carrying repeating keys, then drain. | Queued events with the same coalesce key are replaced in place preserving queue position; distinct keys accumulate up to the bound then evict per policy; final drain delivers the newest event per key (FR-CONN-008). | R6 |
| BP-004 | Send coalesce-edge events: missing key, malformed key, key collision across subscriptions, key present but null. | Documented fallback per UNIT-013 applies; cross-subscription keys never coalesce together; no panic, no silent drop without a counter (FR-CONN-008). | R6 |
| BP-005 | Fill the queue under `disconnect`. | The connection closes 4704 with the documented reason; other connections are unaffected; the close is counted by policy label (FR-CONN-008). | R6 |
| BP-006 | Stall a consumer while server keepalive pings, `connection_ack`, and close frames are due. | Control frames (ping/pong/ack/close) bypass the data queue and are never dropped or displaced by data pressure (FR-CONN-007). | R6 |
| BP-007 | After policy-caused drops, let the consumer resume reading. | The next delivered message for that subscription carries the `conduit.dropped` extension notice with the drop count; metrics agree with the notice (FR-CONN-009). | R6 |
| BP-008 | On real sockets, stall 500 concurrent consumers during a `W-BURST` script; capture heap profiles before, during, and after. | Per-connection memory stays within the budget envelope; total heap growth is bounded by (connections × queue byte bound) plus fixed overhead, asserted against the profile; recovery returns to baseline (NFR-SCALE-002 boundedness, FR-CONN-007). | R6 |
| BP-009 | Configure slow-consumer detection thresholds (queue depth, oldest-message age) and drive queues toward them under the fake clock. | The structured slow-consumer event emits before the policy fires, with connection/subscription identifiers and no payload bytes; thresholds are configurable and honored exactly (FR-CONN-012). | R6 |
| BP-010 | Override policies per field via `@backpressure(policy:, queue:, coalesceKey:)` with valid and invalid arguments. | Valid overrides bind per subscription field; invalid arguments fail SDL validation at startup naming the directive location (FR-CONN-008, FR-GQL-001). | R6 |
| BP-011 | Run mixed policies on one connection (three subscriptions, one per policy) under a shared queue-full condition. | Each subscription's own policy applies to its events; `disconnect` on one subscription closes the connection per its documented semantics while counters attribute drops correctly beforehand (FR-CONN-007, FR-CONN-008, FR-CONN-011). | R6 |

### 10.7 RESUME — reconnect and resume matrix

Harness: class a (fake clock, memory bus, scripted clients, multi-node
in-process fleet for cross-node rows); RESUME-012 re-runs on class c with
the reference client driver. Fixtures: token corpora (valid, tampered,
expired, cross-field, cross-tenant, oversized), replay-buffer fill scripts,
key-rotation fixtures (§6.4). Runtime budget: class A; class C re-run within
the conformance job. Placement: gates CI on every PR. Flake policy:
deterministic — zero tolerance; conformance re-run — container policy.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| RESUME-001 | Deliver events, capture the resume position from each `Next`'s `extensions.conduit`, reconnect, and present the token. | Positions are per (tenant, field) monotonic within the node epoch; the token round-trips through the codec and resumes from the exact position (FR-RESUME-001, FR-RESUME-002). | R7 |
| RESUME-002 | Present forged tokens: flipped signature bits, re-signed with a wrong key, truncated, and bit-flipped payload with valid-looking structure. | Every forgery is rejected with a typed error, the subscription proceeds fresh with a `resume_rejected` notice, and the forgery attempt is logged (FR-RESUME-007, NFR-SEC-007). | R7 |
| RESUME-003 | Present tokens aged beyond the configured maximum and tokens signed by a removed key. | Both reject as expired classes with `resume_rejected`; fresh subscription proceeds; no replay occurs (FR-RESUME-007). | R7 |
| RESUME-004 | Present a valid token for field X on a subscribe to field Y, and a tenant-A token on a tenant-B principal. | Both reject with typed errors; the cross-tenant attempt additionally produces a security log record; no cross-field or cross-tenant replay path exists (FR-RESUME-007, FR-AUTH-017). | R7 |
| RESUME-005 | Resume while live publishing continues; script the splice point exactly at buffer-tail versus incoming-live overlap. | Replay then live delivery with no duplicate and no gap at the splice; the cutover envelope is deterministic under the scripted schedule (FR-RESUME-004, FR-RESUME-006). | R7 |
| RESUME-006 | Fill the ring buffer past the horizon (count and byte bounds), then resume with a token now behind the horizon. | The server sends a `resume_gap` notice stating the covered range before live delivery; it never fabricates completeness (FR-RESUME-005, ADR-0007). | R7 |
| RESUME-007 | Resume against a node with a different epoch (restarted node fixture) and against a fresh node lacking the buffer (multi-node fixture). | Both produce `resume_gap` with the honest covered range (possibly empty), then live delivery (FR-RESUME-005, ADR-0005). | R7 |
| RESUME-008 | Revoke a principal's access to a subset of buffered events between disconnect and resume; resume. | Replay traverses the full publish-time authorization and filter path; revoked-era events are withheld exactly as live delivery would withhold them (FR-RESUME-004, FR-AUTH-010). | R7 |
| RESUME-009 | Drive the ring buffer to its 4,096-envelope and 16 MiB bounds per (tenant, field) with oversized and numerous envelopes; scrape the horizon metric. | Whichever bound binds first evicts oldest; memory never exceeds the byte bound; `conduit_replay_buffer_horizon_seconds` reports accurately (FR-RESUME-003). | R7 |
| RESUME-010 | Replay interleaved multi-publisher buffered streams and merge with live events at cutover. | Per-publisher order is preserved through replay and across the merge; the merge is deterministic at the cutover envelope (FR-RESUME-006). | R7 |
| RESUME-011 | Rotate HMAC keys per §6.4 mid-session; resume with tokens signed by the retired-but-listed key, then by the removed key. | Overlap-period tokens verify; removed-key tokens reject as RESUME-003; the active-`kid` log line appears on reload (FR-RESUME-002, NFR-SEC-007). | R7 |
| RESUME-012 | Run the reference client with the Conduit resume driver through disconnect/resume; run the unmodified reference client (no resume extension) through the same disconnect. | The resume-aware driver resumes per contract; the unmodified client reconnects fresh and remains fully functional, never harmed by resume machinery (NFR-COMPAT-001, PRODUCT_REQUIREMENTS §5.4). | R7 |

### 10.8 CONN — connection lifecycle matrix

Harness: class a for timer/quota logic; class b for socket-real rows
(CONN-004, CONN-007, CONN-008, CONN-009); drain re-runs on class d at R8.
Fixtures: timing-wheel scripts, quota configurations, TLS certificate pairs
(valid, expiring, mismatched), proxy-header corpora. Runtime budget: class A
and class B respectively. Placement: gates CI on every PR. Flake policy:
deterministic — zero tolerance.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| CONN-001 | Hold a connection with no client traffic (pongs suppressed) across the 5 min idle window (fake clock); repeat with pongs flowing. | Silent connection closes 4702 at the window; pong traffic counts as activity and prevents closure (FR-CONN-002). | R6 |
| CONN-002 | Create a cohort of 1,000 connections in one scripted instant; advance to the 12 h lifetime. | Each receives a warning ping then closes 4701; close times spread across the ±10% jitter band, not simultaneously — distribution asserted (FR-CONN-003). | R6 |
| CONN-003 | Exceed per-principal and per-tenant connection quotas at `connection_init`; exceed per-connection (100) and per-principal subscription quotas at `Subscribe`. | Excess connections close 4703 with a typed reason; excess subscriptions get a typed error, not a close, and the connection stays usable (FR-CONN-004, FR-CONN-005). | R6 |
| CONN-004 | Flood inbound messages beyond 50 msg/s burst 100 on a real socket via the hostile client. | A warning precedes enforcement; sustained abuse closes 4400-class with a typed reason; compliant connections on the same node are unaffected (FR-CONN-006, FR-SUB-012). | R6 |
| CONN-005 | Script the fd-budget tracker toward its threshold with synthetic accept load. | New upgrades are load-shed with HTTP 503 before fd exhaustion; threshold and current usage appear in health output; established connections are untouched (FR-CONN-014). | R6 |
| CONN-006 | Trigger drain via signal and via `/admin/v1/drain` (including dry-run) with 5,000 scripted connections and a 60 s window (fake clock). | Accepts stop, readiness fails, 4700 closes pace evenly across the window with retry-after hints, in-flight operations get bounded grace, exit occurs when empty or at deadline; dry-run mutates nothing (FR-CONN-010, FR-ADMIN-003, FR-RESUME-009 hint). | R8 |
| CONN-007 | Reload TLS certificates on the client listener via SIGHUP with established connections active; present the new certificate to fresh connections. | New handshakes use the new material; established connections continue undisturbed; a bad replacement pair is rejected leaving the old material serving (FR-CONN-013). | R6 |
| CONN-008 | Run `trusted_proxy` mode with allowlisted and non-allowlisted peer addresses and spoofed forwarding headers; run plaintext without acknowledgment. | Headers are honored only from allowlisted proxies; spoofed headers from other peers are ignored and logged; unacknowledged plaintext refuses to start (FR-CONN-013, NFR-SEC-005). | R6 |
| CONN-009 | Tear down connections mid-delivery (client RST, server close, drain) under `-race` with concurrent publishes matching their entries. | Registration and teardown are atomic with respect to matching: no delivery to a torn-down connection, no orphan entries after close (FR-CONN-001). | R2 |
| CONN-010 | Run the slowloris suite: byte-at-a-time handshakes, stalled TLS handshakes, half-open sockets at scale. | Handshake and read deadlines bound every phase; node memory and fd usage stay bounded; the hostile-client suite passes in one process (NFR-SEC-008, FR-SUB-012). | R6 |

### 10.9 CHAOS — fault and operations rehearsal matrix

Harness: class d (kind 3-node fleet, NATS deployment, `conduit-loadgen`)
except CHAOS-006 and CHAOS-007 which run on class c with scripted outage
injection. Fixtures: `W-REF` and `W-STORM` workloads, fault injectors (§8.4d),
JWKS stub with outage mode, PostgreSQL testcontainer with kill switch.
Runtime budget: class D — the full chaos set ≤ 4 h nightly; each scenario
individually ≤ 30 min. Placement: nightly (`nightly / chaos-full`);
release-blocking for the gates named per row. Flake policy: statistical
acceptance bands as stated per row; no retries without a recorded cause
attached to the run artifact; every run stores its full metric capture.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| CHAOS-001 | Execute the documented Kubernetes rolling deploy across the fleet under `W-REF`. | Each node drains per FR-CONN-010; fleet-wide delivery continues; client-visible loss stays within the drain-window contract; mixed-version rules hold throughout (FR-OPS-006, NFR-COMPAT-005). | R10 |
| CHAOS-002 | Drain one node while `W-STORM` replays its clients against the remaining two. | Paced 4700 closes with retry-after hints; accept-rate pacing bounds the herd; remaining nodes stay within latency bands for established connections (FR-CONN-010, FR-RESUME-009, NFR-SCALE-006). | R8 |
| CHAOS-003 | `kill -9` one node mid-publish-burst under `W-BURST`. | Surviving nodes deliver throughout; reconnecting clients resume with honest gaps; no surviving-node error-rate excursion beyond the band (FR-FAN-005, FR-RESUME-005, ADR-0005). | R5 |
| CHAOS-004 | Restart the NATS deployment under load; separately, partition one node from NATS for 120 s then heal. | Degraded mode enters and exits visibly; local delivery continues on the partitioned node; no replay on heal; drops counted (FR-FAN-006, FR-FAN-007, FR-AUTH-015). | R5 |
| CHAOS-005 | Induce a NATS slow-consumer condition on one node by throttling its bus consumption under fleet publish load. | The node drops with counters and health signals rather than buffering unboundedly; RSS stays within budget; other nodes unaffected (FR-FAN-007, ADR-0004). | R5 |
| CHAOS-006 | Take the JWKS endpoint down, then return it serving rotated keys, while connections authenticate and refresh across the outage. | Cached-JWKS validation continues within bounds; new-connection failures fail closed with uniform errors; recovery picks up rotated keys within the bounded refresh; alert fires (FR-AUTH-001, ALERT-JWKS). | R3 |
| CHAOS-007 | Kill PostgreSQL under query/mutation load with configured timeouts and pooling; restore it. | Typed timeout/error mapping per FR-GQL-004; deadlines cancel in-flight calls (FR-GQL-014); no connection-pool leak after recovery; publishes for failed mutations never emit (FR-GQL-003). | R1 |
| CHAOS-008 | Reload SDL/bindings via SIGHUP and admin trigger under load: a valid new set, an invalid set, and a set removing a subscribed field. | Valid reload cuts over atomically with a new config hash; invalid reload leaves the old schema serving; removed-field subscriptions complete with a typed error (FR-OPS-003, §7.3). | R8 |
| CHAOS-009 | Inject revocations continuously during CHAOS-004's partition and heal phases. | Non-partitioned nodes meet the propagation SLO; the partitioned node applies its degraded policy exactly; post-heal convergence closes revoked principals with no post-application delivery (FR-AUTH-013, FR-AUTH-014, FR-AUTH-015). | R5 |

### 10.10 SOAK/LOAD — endurance and scale matrix

Harness: class d, on the BENCHMARK_PLAN-designated environment for any row
whose number is published; kind fleet acceptable for leak-detection rows.
Fixtures: `W-IDLE`, `W-REF`, `W-CHURN`, `W-STORM` (§9.6). Measurement method
for every row is owned by BENCHMARK_PLAN (capture discipline,
coordinated-omission-safe latency, GC evidence per NFR-PERF-006); this
matrix defines only pass/fail mechanics. Runtime budget: class D — nightly
accelerated variants ≤ 6 h combined; full R9 runs are scheduled
gate-evidence executions. Placement: nightly accelerated
(`nightly / soak-accelerated`); full-length runs at gate evidence time.
Flake policy: statistical acceptance bands; no retries without recorded
cause; every run's raw capture is retained as an artifact.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| SOAK-001 | Hold 50,000 authenticated `W-IDLE` connections (keepalive active, one subscription each) on one benchmarked node for ≥ 30 min; method per BENCHMARK_PLAN §environment. | Zero unexpected closes; RSS delta per 10,000 connections ≤ 64 KiB/connection; GC evidence captured (NFR-SCALE-001, NFR-SCALE-002, NFR-PERF-006). | R9 |
| SOAK-002 | Run `W-REF` at 5,000 envelopes/s on one node for ≥ 30 min; method per BENCHMARK_PLAN §workloads. | Publish-to-delivery-enqueue p50 ≤ 10 ms, p95 ≤ 50 ms, p99 ≤ 150 ms; memory ≤ 100 KiB/connection p95; drop counters explainable by scripted slow consumers only (NFR-PERF-001, NFR-SCALE-002, NFR-SCALE-005). | R9 |
| SOAK-003 | Run `W-CHURN` at 500 accepts/s with proportional subscribe churn as an 8 h soak, executed nightly in accelerated form (2 h at elevated churn) and full-length at R9 evidence time. | Established-connection latency targets hold throughout; fd, goroutine, and heap counts return to baseline bands after each churn wave (NFR-SCALE-006). | R9 |
| SOAK-004 | Run the `W-STORM` expiry-storm profile: a large token cohort expires within one warning window, forcing warned reconnect-with-resume at scale. | Warning pings emit on schedule; the reconnect surge stays within accept pacing; post-storm delivery latency recovers to band within the documented window (FR-AUTH-012, FR-RESUME-009, ADR-0008 consequences). | R9 |
| SOAK-005 | Drive `W-REF` to 120% of the published targets (connections and publish rate) and hold. | Degradation is the documented kind: load-shed 503s, backpressure drops with counters, latency growth — never OOM, crash, or silent loss; the degradation curve is recorded for the capacity model (FR-OPS-010, FR-CONN-014, product principle §3.5). | R9 |
| SOAK-006 | Memory-leak detection method: across SOAK-001 through SOAK-003, capture heap profiles at fixed epochs, compute the post-warmup RSS regression slope, and diff profile object counts by type. | Post-warmup slope statistically indistinguishable from zero over the soak window; no monotonic object-count growth by type in profile diffs; any positive slope is a release-blocking finding (NFR-SCALE-002, product principle §3.4). | R9 |
| LOAD-001 | Three-node fleet `W-REF` throughput comparison against the single-node result; method and statistics per BENCHMARK_PLAN §fleet. | Fleet delivery throughput ≥ 2.5× single-node with the bus-overhead loss published, not hidden; bus added latency p95 ≤ 5 ms same-AZ (NFR-SCALE-003, NFR-PERF-003). | R9 |

### 10.11 PKG — packaging, artifact, and install matrix

Harness: CI runners plus clean-container fixtures (empty distroless and
scratch containers, fresh kind cluster); no Conduit test harness code — the
suite exercises released artifacts exactly as an operator would. Fixtures:
the release workflow's own outputs, `SHA256SUMS`, SBOM files, signed
attestations, reference Kubernetes manifests. Runtime budget: class C — the
packaging suite ≤ 25 min excluding the kind install test (≤ 15 min more,
release workflow only). Placement: `release.yml` on tags and release
candidates; a fast subset (PKG-003, PKG-005) runs nightly. Flake policy:
container policy — one retry with mandatory triage issue; a
reproducibility mismatch (PKG-001) is never retried, it is a finding.

| ID | Mechanics | Pass criteria | Earliest gate |
| --- | --- | --- | --- |
| PKG-001 | Build the release binary twice from the same SHA on independent runners with the pinned toolchain, vendored modules, `-trimpath`, and stamped metadata; byte-compare. | Bit-identical binaries per platform; any difference fails the release and opens an investigation (FR-OPS-012). | R10 |
| PKG-002 | Generate the SBOM with the pinned syft for each binary and image; validate format and completeness against `go.mod`. | SBOM exists per artifact, parses as SPDX, and lists every vendored module at its pinned version (FR-OPS-012, NFR-SEC-010). | R10 |
| PKG-003 | Inspect the OCI image: base, user, filesystem contents, and platform variants. | Distroless base, `USER nonroot`, only the binary plus licenses and CA bundle present, linux/amd64 and linux/arm64 variants published under one multi-arch manifest (FR-OPS-001, ADR-0011). | R10 |
| PKG-004 | Run the binary and image on clean containers: distroless (image), and the static binary copied into an empty scratch container and a stock Debian container. | `conduit version` and `conduit validate` run with no missing-library or writable-filesystem requirement; the binary is fully static (FR-OPS-001). | R10 |
| PKG-005 | Check version stamping: `conduit version` output, image labels, and the `/admin/v1/config` version fields against the tag and SHA. | Version, commit, and build date are stamped via `-ldflags -X`, consistent across binary, image, and admin output (FR-OPS-012). | R10 |
| PKG-006 | Verify signatures and provenance: `cosign verify-blob` on checksums, `cosign verify` on the image, and SLSA attestation validation against the workflow identity. | All signatures verify against the expected keyless identity; the attestation names the exact workflow, SHA, and builder (FR-OPS-012). | R10 |
| PKG-007 | Execute the documented uninstall and purge procedures (§14.6) after a full install-and-serve cycle on a host and on the kind cluster; sweep for residue. | Nothing durable remains beyond operator-captured logs; bus subjects and credentials are decommissioned per checklist; the residue sweep finds zero Conduit-owned files, subjects, or credentials (FR-OPS-011). | R10 |
| PKG-008 | Run `conduit doctor` against a deliberately broken environment matrix: low fd limits, skewed clock, unreachable bus, expired TLS material, world-readable key file. | Every defect is reported with the observed value and the actionable fix; nothing is mutated; healthy environments pass clean (FR-OPS-013). | R10 |
| PKG-009 | Cross-compile all Tier 1 artifacts from a Linux amd64 runner and a macOS arm64 developer machine; compare against release artifacts. | Cross-compilation from any supported dev machine reproduces the Tier 1 binaries bit-identically (ADR-0011 consequences, FR-OPS-012). | R10 |
| PKG-010 | Apply the reference Kubernetes manifests to kind 1.29 and 1.31; run first-run validation, a reference-client subscription, and a `kubectl rollout restart`. | Manifests apply cleanly on both versions; the flow of §14.3 completes; the rollout respects preStop/readiness/drain integration within the drain-window contract (FR-OPS-004, FR-OPS-006). | R10 |

## 11. Continuous Integration

### 11.1 Workflow inventory

| Workflow | Trigger | Jobs (exact required-check names where applicable) | Budget |
| --- | --- | --- | --- |
| `pr.yml` | Every PR and push to `main` | `lint`, `vet`, `arch-check`, `unit-race`, `proto-race`, `authz-race`, `index-race`, `docs-status-lint`, `metrics-contract`, `deps-audit`, `trace-check` — all with `-race` on for test jobs | ≤ 15 min wall clock for the whole workflow |
| `integration.yml` | Every PR and every push to `main`; required job contexts always report, while later gates may conditionally skip expensive job steps for PRs outside transport, bus, data-source, or auth paths | `conformance-node` (reference-client suite, class c), `integration-nats` (broker suite), `integration-postgres` (relational adapter, PG 14 and 16), `socket-hostile` (class b hostile suite) | ≤ 25 min wall clock |
| `nightly.yml` | Scheduled daily; manual dispatch | `fuzz` (all frame/document/token parsers, 30 min per target, corpus persisted), `soak-accelerated` (SOAK-003 accelerated form plus SOAK-006 method), `chaos-full` (all CHAOS rows), `nats-matrix` (broker range endpoints per §2.2), `bench-regression` (§12.3 benchstat comparison), `index-property-extended` (INDEX-001 at 20× iterations) | ≤ 8 h; each job individually bounded |
| `release.yml` | Tags `v*` and release-candidate dispatch | `package` (build matrix, PKG-001–PKG-005, PKG-009), `provenance` (PKG-006 signing and attestation), `cross-version-fixtures` (NFR-COMPAT-005 fixture suite, §14.5), `image-scan` (vulnerability scan of the release image), `kind-install` (PKG-010) | ≤ 60 min |

Platform coverage: `pr.yml` runs on Linux amd64 and macOS arm64; Linux
arm64 runs the full `pr.yml` matrix on `main` and release branches and a
representative subset on PRs. Tier 1 jobs are release-blocking; macOS jobs
block for correctness suites only (ADR-0011).

### 11.2 Required-check mapping to gates

The branch-protection contexts in §3.2 are the permanent PR floor. Gate
evidence additionally requires named workflow runs on the evidence SHA:

| Gate | Blocking workflows beyond the PR floor |
| --- | --- |
| R0 | `pr.yml` complete on the baseline SHA |
| R1 | `integration.yml` (`integration-postgres`) |
| R2 | `integration.yml` (`conformance-node`, `socket-hostile`) |
| R3–R4 | `pr.yml` families (`authz-race`, `index-race`) plus `nightly / index-property-extended` green once on the evidence SHA |
| R5 | `integration.yml` (`integration-nats`), `nightly / chaos-full` R5 rows, `nightly / nats-matrix` |
| R6 | `pr.yml` BP/CONN families, `nightly / fuzz` green on the evidence SHA |
| R7 | RESUME family in `pr.yml` plus `conformance-node` resume rows |
| R8 | CHAOS-002/008, drain rows, metrics-contract, runbook lint (§17.2) |
| R9 | Scheduled SOAK/LOAD evidence runs with stored captures |
| R10 | `release.yml` complete, including `cross-version-fixtures` and `kind-install` |

### 11.3 Artifact retention

- PR workflow logs and failure artifacts: 30 days.
- Nightly chaos/soak metric captures and fuzz corpora: 90 days; fuzz
  corpora additionally checked into `testdata/fuzz/` when minimized.
- Gate evidence runs (the runs cited in a gate's closure record): retained
  permanently — the workflow run is pinned and its artifact bundle is
  copied into `reports/gates/<gate>/<sha>/` in-repo (§19.2).
- Release artifacts, SBOMs, signatures, attestations: permanent, attached
  to the GitHub release.

Failure artifacts are allowlisted: seeds, fixture IDs, hashes, counts,
state-machine states, redacted logs, and metric captures. Payload bytes,
credentials, and canary values never enter an artifact (NFR-SEC-004); the
canary scan (§9.5) includes uploaded artifacts.

### 11.4 Nightly ownership

Each nightly job has a named owning subsystem area. A nightly failure
auto-opens an issue labeled `nightly-red` against the owning area with the
run link; three consecutive reds freeze merges to that area per §3.4.

### 11.5 Flake quarantine process

1. A container-suite test that passes only on retry twice consecutively is
   quarantined automatically: moved to a quarantine list that still runs it
   but does not block merges.
2. Quarantine requires a triage issue (`flake-triage`) with owner and a
   14-day SLA to root cause; the issue links every occurrence.
3. A quarantined test blocks the gate that owns its family: no gate closes
   with a quarantined row in its evidence set.
4. Deterministic-suite tests are never quarantined — a deterministic flake
   is a bug that blocks its gate immediately (§8.3).
5. Load/chaos rows are never quarantined; out-of-band results are findings
   recorded against the run.

## 12. Performance and Resource Budgets

### 12.1 Two-column discipline

Every performance requirement carries two numbers: the published SLO target
(what the claims ladder may state, measured by BENCHMARK_PLAN on its
designated environment) and the CI regression ceiling (a looser bound
checked continuously on CI hardware so regressions surface before the next
full benchmark). A CI-ceiling breach is a merge-blocking regression; an
SLO-target breach on the benchmark environment is a gate-blocking finding.

| Requirement | Published SLO target (BENCHMARK_PLAN method) | CI regression ceiling (nightly `bench-regression`) |
| --- | --- | --- |
| NFR-PERF-001 delivery latency | p50 ≤ 10 ms, p95 ≤ 50 ms, p99 ≤ 150 ms on `W-REF` | Microbenchmark of the match→authorize→enqueue path: no >5% regression versus the stored baseline (benchstat) |
| NFR-PERF-002 index match | p99 ≤ 1 ms at 100,000 entries | Match microbenchmark at 10k and 100k entries: no >5% regression; index must beat scan at ≥ 10,000 entries (FR-FILT-010) |
| NFR-PERF-003 bus added latency | p95 ≤ 5 ms same-AZ on the reference fleet | Memory-bus pipeline overhead benchmark: no >5% regression (broker latency is measured only at R9) |
| NFR-PERF-004 gateway overhead | p95 ≤ 5 ms over data-source latency at reference query load | Executor overhead benchmark with stub source: no >5% regression |
| NFR-PERF-005 hot-path allocations | Zero heap allocations per delivery in steady state | `testing.AllocsPerRun` = 0 on the delivery path — an exact gate, not a band; any nonzero result fails PR CI (`pr / unit-race` hosts the test) |
| NFR-SCALE-002 memory per connection | ≤ 64 KiB idle, ≤ 100 KiB p95 loaded (RSS delta per 10k connections) | Per-connection struct-size and pooled-buffer accounting test: computed footprint budget ≤ 48 KiB, so measured RSS headroom survives; stack-growth regression test on read/write loops (ADR-0001) |
| NFR-SCALE-005 publish throughput | 5,000 envelopes/s/node sustained | Pipeline throughput microbenchmark: no >5% regression |
| NFR-SCALE-006 accept rate | 500 accepts/s sustained | Handshake microbenchmark: no >5% regression |

### 12.2 GC evidence

Every published latency number attaches its GC evidence: `GODEBUG=gctrace=1`
capture, `GOGC` and `GOMEMLIMIT` values, and the Go patch version
(NFR-PERF-006, ADR-0001). The nightly `bench-regression` job records the
same for trend visibility even though nightly numbers are never published.

### 12.3 Microbenchmark gating rules

- Benchmarks live beside their packages as `Benchmark*` functions; the
  nightly job runs each with `-count=10` and compares against the stored
  baseline in `reports/bench/baseline/` using the pinned benchstat.
- The noise band is ±5%: a statistically significant regression beyond 5%
  fails the job and opens a `perf-regression` issue; an improvement beyond
  5% updates the baseline via a reviewed PR (never automatically).
- Baselines are keyed by runner class; results from different runner classes
  are never compared.

### 12.4 Allocation regression tests on the delivery path

The delivery path (index match with pooled counter arrays → publish-time
authorization with epoch cache → enqueue) carries `AllocsPerRun` tests at
each stage and end to end, running in PR CI. The counter-array pool
(ADR-0006 consequences) and the outbound-queue enqueue are individually
asserted at zero steady-state allocations (NFR-PERF-005). New allocations on
this path require an explicit exemption reviewed as a contract change, and
none exist at plan time.

## 13. Packaging and Artifact Construction

### 13.1 Build matrix and flags

| Artifact | Platforms | Notes |
| --- | --- | --- |
| Static binary `conduit` | linux/amd64, linux/arm64 | Tier 1 release artifacts |
| Static binary `conduit` | darwin/arm64 | Built and smoke-tested in CI; published labeled "development use only — Tier 2, no production claims" (ADR-0011) |
| OCI image | linux/amd64 + linux/arm64 multi-arch manifest | Distroless static base, `USER nonroot` (FR-OPS-001) |

Build invocation (identical in CI and in PKG-009 local reproduction):

```sh
CGO_ENABLED=0 go build -mod=vendor -trimpath \
  -ldflags "-s -w \
    -X main.version=${VERSION} \
    -X main.commit=${COMMIT_SHA} \
    -X main.buildDate=${BUILD_DATE_UTC}" \
  -o dist/conduit-${GOOS}-${GOARCH} ./cmd/conduit
```

`BUILD_DATE_UTC` is derived from the commit timestamp, not the wall clock,
so builds are reproducible (FR-OPS-012, PKG-001).

### 13.2 Image, SBOM, signing, provenance, checksums

- Image build: `ko` (preferred, reproducible, no Dockerfile) or
  `docker buildx` with a pinned distroless-static digest; the choice is
  recorded in the release workflow and PKG-003 verifies the result either
  way.
- SBOM: `./bin/syft dist/conduit-linux-amd64 -o spdx-json` per binary and
  per image (PKG-002).
- Checksums: `sha256sum dist/* > dist/SHA256SUMS`.
- Signing: `./bin/cosign sign-blob --yes dist/SHA256SUMS` and
  `./bin/cosign sign --yes <image-digest>` using keyless (OIDC workflow
  identity); PKG-006 verifies against the expected identity.
- Provenance: SLSA-style attestation generated by the release workflow and
  attached via `cosign attest`; it names workflow, SHA, builder, and
  material digests (FR-OPS-012).

### 13.3 Release notes rules

Release notes are generated from conventional-commit history (§3.3) grouped
by type, then hand-edited under two hard rules: every behavior claim names
the gate that owns it, and every performance number links the BENCHMARK_PLAN
claims-ladder entry that produced it. The docs-status lint runs over release
notes like any other document (NFR-MAINT-004); the claims-ladder audit
(§20) covers them before 1.0.

## 14. Installation, First Run, Upgrade, Rollback, Uninstall, Purge

### 14.1 Binary installation and first run

```sh
curl -fsSLO https://github.com/Zachshotamartin/conduit/releases/download/${VERSION}/conduit-linux-amd64
curl -fsSLO https://github.com/Zachshotamartin/conduit/releases/download/${VERSION}/SHA256SUMS
curl -fsSLO https://github.com/Zachshotamartin/conduit/releases/download/${VERSION}/SHA256SUMS.sig
cosign verify-blob --signature SHA256SUMS.sig \
  --certificate-identity-regexp 'github.com/Zachshotamartin/conduit' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
install -m 0755 conduit-linux-amd64 /usr/local/bin/conduit
mkdir -p /etc/conduit
conduit validate --config /etc/conduit/conduit.yaml
conduit doctor --config /etc/conduit/conduit.yaml
conduit serve --config /etc/conduit/conduit.yaml
```

The first-run flow (PRODUCT_REQUIREMENTS §6.1) — minimal config, one SDL
file, one data source, `auth.mode: none` with
`development_acknowledged: true` — must complete a query, a mutation, and a
filtered subscription from the reference client within fifteen minutes, and
every error on the path names the file, key, or SDL location that caused
it. The R10 evidence includes a scripted execution of exactly this flow.

### 14.2 Container installation

```sh
cosign verify ghcr.io/zachshotamartin/conduit:${VERSION} \
  --certificate-identity-regexp 'github.com/Zachshotamartin/conduit' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
docker run --rm \
  -v /etc/conduit:/etc/conduit:ro \
  -p 8443:8443 -p 9443:9443 \
  ghcr.io/zachshotamartin/conduit:${VERSION} \
  serve --config /etc/conduit/conduit.yaml
```

The image runs as `nonroot` with a read-only root filesystem; nothing in
normal operation writes to disk (§6.1).

### 14.3 Kubernetes installation

The repository ships reference manifests under `deploy/kubernetes/`
(FR-OPS-004): Deployment, Service, PodDisruptionBudget, and HPA guidance,
applied with:

```sh
kubectl apply -k deploy/kubernetes/
```

Manifest-encoded lifecycle integration (FR-OPS-006):

- `readinessProbe` on `/readyz` (fails during drain and configured degraded
  conditions, FR-ADMIN-005); `livenessProbe` on `/healthz`.
- `preStop` triggers drain; `terminationGracePeriodSeconds` = drain window
  (60 s default) + 15 s completion grace, so Kubernetes never SIGKILLs a
  node still pacing 4700 closes.
- PodDisruptionBudget holds `maxUnavailable: 1` for a 3-node fleet so
  voluntary disruptions serialize behind drains.

### 14.4 Load-balancer requirements for long-lived WebSockets

These requirements ship with the manifests and are asserted in PKG-010's
ingress configuration:

- **Idle-timeout floor**: the LB idle timeout must be ≥ keepalive interval
  (25 s) plus slack; the shipped guidance sets ≥ 75 s. An LB timeout below
  the keepalive interval converts healthy connections into spurious
  disconnect load.
- **Draining interaction**: LB deregistration delay must be ≥ the Conduit
  drain window so paced 4700 closes reach clients before the LB severs the
  path; readiness-based deregistration (via `/readyz`) is the shipped
  pattern.
- **WebSocket-aware health checks**: health checks target the admin
  listener's `/readyz`, never the client listener; checks that open client
  WebSockets pollute connection metrics and quota accounting.
- **Trusted-proxy headers**: when TLS terminates at the LB, Conduit runs in
  `trusted_proxy` mode with the LB address range in the mandatory proxy
  allowlist; forwarded-for and forwarded-proto headers are honored from
  allowlisted peers only (FR-CONN-013, CONN-008 row).

### 14.5 Rolling upgrade and mixed-version rules

Procedure (rehearsed by CHAOS-001):

1. Confirm the target release's `cross-version-fixtures` job is green: bus
   envelopes, control messages, and resume tokens written by N are readable
   by N+1 and vice versa (FR-OPS-005, NFR-COMPAT-005). Cross-version
   fixtures are release-blocking from the first tagged release.
2. `kubectl set image deployment/conduit conduit=ghcr.io/zachshotamartin/conduit:${NEW_VERSION}`
   then `kubectl rollout status deployment/conduit`.
3. Each pod drains per FR-CONN-010; clients reconnect with resume tokens to
   remaining capacity; no client sees a protocol behavior change
   mid-connection (PRODUCT_REQUIREMENTS §6.7).
4. Only N and N+1 may coexist; skipping versions requires stepping through
   each intermediate minor. The upgrade window ends when all pods report
   the new version in `/admin/v1/config`.

### 14.6 Rollback, uninstall, purge

**Rollback** uses the same drain path in reverse:
`kubectl rollout undo deployment/conduit`. Because the mixed-version window
is symmetric (N+1-written contracts are readable by N within the window,
FR-OPS-005), rollback during or after an upgrade is safe for all versioned
contracts; resume tokens issued by N+1 verify on N. Rollback across a
contract version bump (a major envelope or token version change) is not
supported and the release notes for such a release must say so before the
claims-ladder audit passes.

**Uninstall** (host): stop the service, then
`rm /usr/local/bin/conduit`. **Uninstall** (Kubernetes):
`kubectl delete -k deploy/kubernetes/`.

**Purge checklist** (FR-OPS-011, verified by PKG-007):

1. Remove configuration and key material: `/etc/conduit/` including
   `conduit.yaml`, `tls/`, `keys/resume-hmac.yaml`, `bus.creds`.
2. Decommission bus subjects: remove the fleet's NATS accounts/permissions
   for `conduit.<tenant>.pub.*` and `conduit.<tenant>.ctl.*`; no Conduit
   data is stored in NATS (core NATS retains nothing, ADR-0004).
3. Revoke credentials: NATS credentials, admin bearer tokens or mTLS client
   certificates, data-source credentials issued for Conduit, and the JWKS
   client registration if one was created.
4. Confirm the no-durable-state design: nothing else exists to remove
   beyond logs already in the operator's log pipeline (§6.1).

## 15. Diagnostics and Support Bundles

### 15.1 `/admin/v1/diagnostics` inventory (FR-ADMIN-007)

The bundle contains exactly: the effective-config hash and redacted
configuration; binary version, commit, build date; runtime stats (GC,
goroutine count, heap, fd usage); the last 1,000 structured events from the
bounded in-memory event ring; current gauge snapshot of the §16 catalogue;
goroutine and heap profiles; and the bundle's own inventory manifest. It
never contains payload bytes, credential bytes, token contents, or key
material — the canary suite (§9.5) runs against generated bundles, and the
inventory manifest is a contract: an undeclared file in the bundle fails the
diagnostics test.

### 15.2 Log conventions and rate limits

- JSON via `log/slog` with stable keys: `tenant`, `conn_id`, `sub_id`,
  `close_code`, `error_code` as applicable (ADR-0010).
- No payload bytes, credentials, or token contents at any level
  (NFR-SEC-004); reason strings echo no client bytes (FR-SUB-008).
- Per-connection log volume is rate-limited so a hostile client cannot
  amplify logging into disk exhaustion (NFR-SEC-009); suppressed-record
  counts are themselves logged and counted.
- Unknown-error paths log at `ERROR` with a typed `UNKNOWN` category,
  counted and alarmed (NFR-MAINT-003).

### 15.3 Profile capture policy

Goroutine and heap profiles are available on demand through the diagnostics
bundle and continuously via the admin listener's pprof endpoints (admin
authentication required, never on the client listener, FR-ADMIN-001).
Operators capture at most one CPU profile at a time; the runbook's
memory-incident entry (§17) names the exact capture commands. Profiles
contain symbol names only — the arch-check keeps secret material out of
identifier names as a defense in depth.

## 16. Observability Contract

### 16.1 Metric catalogue

This table is the normative catalogue (FR-ADMIN-004, FR-OPS-009). Names use
the `conduit_` prefix; types are Prometheus types; the cardinality bound is
the maximum label-combination count per node.

| Name | Type | Labels | Cardinality bound | Owning subsystem |
| --- | --- | --- | --- | --- |
| `conduit_connections_active` | gauge | tenant | 51 | transport |
| `conduit_connections_accepted_total` | counter | tenant | 51 | transport |
| `conduit_connections_closed_total` | counter | tenant, close_code | 51 × 13 | transport |
| `conduit_connection_duration_seconds` | histogram | tenant | 51 | transport |
| `conduit_handshake_rejections_total` | counter | reason | 8 | transport |
| `conduit_inbound_messages_total` | counter | tenant, type | 51 × 8 | transport |
| `conduit_subscriptions_active` | gauge | tenant, field | 51 × 100 | registry |
| `conduit_subscribe_total` | counter | tenant, outcome | 51 × 6 | registry |
| `conduit_index_entries` | gauge | tenant, field | 51 × 100 | index |
| `conduit_index_residual_entries` | gauge | tenant, field | 51 × 100 | index |
| `conduit_index_match_seconds` | histogram | (none) | 1 | index |
| `conduit_index_candidate_set_size` | histogram | (none) | 1 | index |
| `conduit_publish_envelopes_total` | counter | tenant, stage | 51 × 6 | fanout |
| `conduit_fanout_stage_seconds` | histogram | stage | 4 | fanout |
| `conduit_publish_rate_limited_total` | counter | tenant | 51 | fanout |
| `conduit_bus_connection_state` | gauge | (none) | 1 | bus |
| `conduit_bus_reconnects_total` | counter | (none) | 1 | bus |
| `conduit_bus_dropped_envelopes_total` | counter | reason | 4 | bus |
| `conduit_outbound_queue_depth` | histogram | (none) | 1 | delivery |
| `conduit_backpressure_drops_total` | counter | tenant, field, policy | 51 × 100 × 3 | delivery |
| `conduit_slow_consumer_events_total` | counter | tenant | 51 | delivery |
| `conduit_delivery_enqueue_seconds` | histogram | (none) | 1 | delivery |
| `conduit_replay_buffer_horizon_seconds` | gauge | tenant, field | 51 × 100 | resume |
| `conduit_resume_attempts_total` | counter | outcome | 4 | resume |
| `conduit_resume_replayed_envelopes_total` | counter | tenant | 51 | resume |
| `conduit_authz_decisions_total` | counter | point, decision | 2 × 2 | authz |
| `conduit_authz_decision_seconds` | histogram | point | 2 | authz |
| `conduit_revocations_applied_total` | counter | kind | 4 | authz |
| `conduit_revocation_apply_latency_seconds` | histogram | (none) | 1 | authz |
| `conduit_revocation_set_size` | gauge | (none) | 1 | authz |
| `conduit_jwks_refresh_total` | counter | outcome | 3 | authz |
| `conduit_degraded_mode` | gauge | reason | 4 | health |
| `conduit_fd_usage` / `conduit_fd_limit` | gauge | (none) | 1 each | health |
| `conduit_drain_state` | gauge | (none) | 1 | lifecycle |
| `conduit_config_reloads_total` | counter | outcome | 2 | config |
| Go runtime collector (`go_goroutines`, `go_gc_duration_seconds`, `go_memstats_*`) | mixed | (none) | standard set | runtime |

### 16.2 Named cardinality budget

- **Tenant label cap**: at most 50 labeled tenants per node; the 51st and
  beyond aggregate into the `other` bucket (ADR-0009 consequences). The cap
  is configuration with a documented default, and crossing it logs once.
- **Field label cap**: at most 100 labeled subscription fields; beyond,
  `other`.
- **Enumerated labels** (close_code, stage, policy, outcome, reason, point,
  kind, type) are closed sets defined by their owning contracts; adding a
  value is a reviewed contract change.
- **Total series ceiling**: 10,000 series per node. The metrics-contract
  test computes the worst-case series count from the catalogue and fails
  if the ceiling is exceeded; the runtime also samples actual series count
  and alarms at 80% of ceiling.

### 16.3 Trace sampling policy (ADR-0010)

Traces are off by default; when enabled, parent-based ratio sampling
(default 0.01) applies to HTTP operations, subscribe handling, and the
publish pipeline with the bus hop as a span link. There is never one span
per fanout delivery leg beyond the sample. Trace attributes carry the same
stable keys as logs and are canary-scanned (§9.5).

### 16.4 Metrics-contract CI test (FR-OPS-009)

The `pr / metrics-contract` job starts a node against a fixture workload,
scrapes `/metrics`, and fails on: any metric not in §16.1; any label not in
the catalogue row; any enumerated label value outside its closed set; any
documented metric absent after its subsystem exercised; and computed series
count beyond the ceiling. The catalogue table above is machine-readable
(generated from a checked-in YAML source that also renders this section),
so drift between doc and test is structurally impossible.

## 17. Incident Response and Release Revocation

### 17.1 Severity ladder

| Severity | Definition | Response |
| --- | --- | --- |
| SEV1 | Authorization bypass, cross-tenant leak, secret exposure, or fleet-wide delivery outage | Page immediately; stop the line (no releases); security process (§17.4) if security-related |
| SEV2 | SLO breach in production defaults (revocation lag, delivery p99, memory budget), single-node crash loop, degraded mode without recovery | Page; fix or mitigate before next release |
| SEV3 | Bounded degradation matching documented behavior (drop-rate spike within policy, residual ceiling reached) | Ticket with runbook remediation; review at triage |
| SEV4 | Cosmetic, docs, or tooling faults | Ticket |

### 17.2 Shipped alerts and runbook mapping (FR-OPS-008)

Every alert ships with the reference dashboards and maps to a rehearsed
runbook entry; an alert without a runbook entry fails the R8 gate, enforced
by a CI lint that joins the alert list against runbook anchors. The CHAOS
rows named below are the rehearsal evidence.

| Alert | Fires on | Runbook entry | Rehearsed by |
| --- | --- | --- | --- |
| `ConduitSlowConsumerSurge` | `conduit_slow_consumer_events_total` rate above band | RB-SLOW-CONSUMER | BP-009, CHAOS-005 |
| `ConduitResidualCeiling` | `conduit_index_residual_entries` at ceiling on any field | RB-RESIDUAL | INDEX-007 |
| `ConduitRevocationLag` | `conduit_revocation_apply_latency_seconds` p99 > 2 s | RB-REVOCATION-LAG | AUTHZ-013, CHAOS-009 |
| `ConduitBusDisconnect` | `conduit_bus_connection_state` = 0 beyond 10 s | RB-BUS-DISCONNECT | FAN-013, CHAOS-004 |
| `ConduitDegradedMode` | `conduit_degraded_mode` > 0 | RB-DEGRADED | AUTHZ-011/012, CHAOS-004 |
| `ConduitFdPressure` | `conduit_fd_usage` / `conduit_fd_limit` > 0.8 | RB-FD-PRESSURE | CONN-005 |
| `ConduitMemoryPerConnRegression` | RSS per active connection above budget band | RB-MEMORY | SOAK-006 method |
| `ConduitDeliveryP99Breach` | `conduit_delivery_enqueue_seconds` p99 above SLO band | RB-DELIVERY-LATENCY | SOAK-002 |
| `ConduitJwksFailures` | `conduit_jwks_refresh_total{outcome="error"}` rate above band | RB-JWKS | CHAOS-006 |
| `ConduitDropRateSpike` | `conduit_backpressure_drops_total` rate above band | RB-DROP-RATE | BP-001/007, CHAOS-003 |
| `ConduitReplayHorizonShort` | `conduit_replay_buffer_horizon_seconds` below the documented gap-window floor | RB-REPLAY-HORIZON | RESUME-009 |
| `ConduitUnknownErrors` | unknown-error counter nonzero | RB-UNKNOWN-ERROR | NFR-MAINT-003 test |

### 17.3 On-call diagnosis flows

Each runbook entry follows one shape: confirm the signal (exact PromQL
shipped with the dashboard); classify against documented behavior (is this
the bounded, honest degradation the contract states, or a violation?);
capture (diagnostics bundle, relevant profiles); remediate (entry-specific:
raise the residual ceiling versus fix the offending schema; scale out
versus drain a hot node; fail closed versus accept staleness per
FR-AUTH-016); verify recovery on the same signal. Degraded modes are
visible in `/readyz` and metrics before they are visible as client harm
(PRODUCT_REQUIREMENTS §8); a runbook entry that depends on client reports
to detect a condition fails review.

### 17.4 Security releases and revocation

- Reports arrive via the published security contact; acknowledgment within
  48 hours.
- Fixes for SEV1/SEV2 security issues develop in a private fork with a
  private CI mirror; the public repository sees the fix only at coordinated
  release.
- Advisory: GitHub Security Advisory with CVE requested for any
  exploitable issue in a released artifact; the advisory names affected
  version ranges and the fixed version.
- Release revocation: compromised or critically broken releases are marked
  deprecated on the release page, their images tagged `revoked` metadata,
  checksums removed from the latest pointer, and the advisory instructs
  operators to upgrade and — when key material may be affected — rotate
  resume HMAC keys (§6.4), admin credentials, and bus credentials.
- Every security incident lands a permanent regression test named with the
  advisory ID.

## 18. Release Gates R0–R10

Each subsection lists the operations evidence this plan adds; the ticket
list and per-gate evidence matrix live in BUILD_PLAN §X.9 of the owning gate
and are referenced, not duplicated. A gate closes only when its BUILD_PLAN
evidence checklist and the operations additions below are green on one SHA,
with the run pinned per §11.3. R0 is `in progress`; R1 through R10 remain
`planned`.

### 18.1 R0 — repository, toolchain, CI, architecture checks

Evidence commands and workflows:

```sh
gh auth status
gh api repos/Zachshotamartin/conduit/branches/main/protection
make bootstrap && make check && make test
```

plus a green `pr.yml` on the baseline SHA. Operations additions: branch
protection matches §3.2 exactly; the arch-check enforces NFR-MAINT-001
rules (transport-only WebSocket imports, ast-only gqlparser, platform code
confinement per ADR-0011); docs-status lint (NFR-MAINT-004) and dependency
budget check (NFR-MAINT-005, §5.1) pass; `trace-check` (§19.1) runs green
on the empty-but-wired matrix; UNIT-001 and UNIT-016 pass; determinism
rules (NFR-MAINT-006) are enforced by lint (no `time.Sleep` in
deterministic suites). NFR-SEC-010 evidence: `deps-audit` green.

### 18.2 R1 — queries and mutations against real data sources

Evidence: BUILD_PLAN §R1.9 checklist plus `integration-postgres` green;
UNIT-002 through UNIT-006, UNIT-017, CHAOS-007 (deterministic subset) pass.
Operations additions: `conduit validate` phases 1–6 behave per §7.1 on the
corpus; the admin listener skeleton serves `/metrics` and `/healthz` with
the metrics-contract test wired (ADR-0010 consequences); error redaction
canaries pass at every R1 sink (FR-GQL-012 advanced here, closed at R3).

### 18.3 R2 — protocol conformance against the unmodified reference client

Evidence: the full PROTO matrix (PROTO-001–014), UNIT-010, UNIT-012,
UNIT-015, CONN-009; `conformance-node` and `socket-hostile` green on the
evidence SHA; the fuzz targets for frame parsers seeded and green in
nightly (NFR-SEC-001, FR-SUB-012 advanced here, closed at R6). Operations
additions: the close-code table generation check (PROTO-012) proves
FR-SUB-010; the reference-client pin and range are documented in
PROTOCOL_CONFORMANCE and enforced by the fixture build (NFR-COMPAT-001,
NFR-COMPAT-002); FR-CONN-001 closes here per BUILD_PLAN §19.

### 18.4 R3 — authorization: subscribe, publish, revocation, expiry

Evidence: AUTHZ-001 through AUTHZ-012 and AUTHZ-014 through AUTHZ-016;
UNIT-014, UNIT-018; CHAOS-006. Operations additions: the named
no-post-revocation-delivery adversarial test (AUTHZ-007) is cited by name
in the gate record (NFR-SEC-002); cross-tenant probes (AUTHZ-010) close the
structural half of NFR-SEC-006; canary redaction closes NFR-SEC-004;
mutation testing runs on `internal/auth` with zero surviving
enforcement-branch mutants (NFR-MAINT-002); FR-GQL-010 and FR-GQL-012 close
here per BUILD_PLAN §19.

### 18.5 R4 — predicate index beats linear scan

Evidence: the full INDEX matrix; UNIT-007; `nightly /
index-property-extended` green on the evidence SHA. Operations additions:
the index-versus-scan benchmark result is stored under
`reports/bench/index/` with the BENCHMARK_PLAN §index method
(FR-FILT-010, NFR-PERF-002); the crossover point and scaling curve are the
published deliverable; mutation testing covers the predicate compiler.

### 18.6 R5 — cross-node fanout under node loss and partition

Evidence: the full FAN matrix including broker re-runs
(`integration-nats`, `nightly / nats-matrix`); CHAOS-003, CHAOS-004,
CHAOS-005, CHAOS-009; AUTHZ-013 (revocation SLO fleet measurement,
FR-AUTH-014, NFR-SEC-003). Operations additions: broker-suite rows verify
the assumed NATS guarantees rather than take them on faith (ADR-0004);
degraded-mode entry/exit is observable per §16 during every partition
scenario; the memory-bus-versus-NATS equivalence obligation is discharged
row by row — no FAN row closes on memory-bus evidence alone.

### 18.7 R6 — backpressure, quotas, bounded memory under slow consumers

Evidence: the full BP matrix; CONN-001 through CONN-005, CONN-007,
CONN-008, CONN-010; UNIT-011, UNIT-013; `nightly / fuzz` green
(NFR-SEC-008, closing FR-SUB-012). Operations additions: BP-008's heap
assertions are the bounded-memory adversarial evidence for the R6
adversarial-load suite (product principle §3.4); the allocation regression
tests (§12.4) land and gate PRs from here (NFR-PERF-005); log
rate-limiting canary evidence closes NFR-SEC-009.

### 18.8 R7 — reconnect and resume with the measured gap window

Evidence: the full RESUME matrix; the resume-token codec fuzz target green;
`conformance-node` resume rows (RESUME-012). Operations additions: the
measured gap window — buffer horizon in seconds at reference publish
rates — is produced per BENCHMARK_PLAN and stored under
`reports/bench/resume/` as the R7 benchmark deliverable cited by the public
API contract documentation (FR-RESUME-008); key-rotation procedure (§6.4)
is rehearsed by RESUME-011; NFR-SEC-007 and NFR-COMPAT-003 (all public
contracts versioned) close here per BUILD_PLAN §19.

### 18.9 R8 — metrics, tracing, admin, drain, runbook

Evidence: CONN-006 (drain), CHAOS-002, CHAOS-008; the metrics-contract test
against the complete catalogue (§16.4); the alert-to-runbook join lint
(§17.2); admin API tests for every `/admin/v1` endpoint including audit
records (FR-ADMIN-001 through FR-ADMIN-008). Operations additions: every
§17.2 alert has fired at least once in a rehearsal run with its runbook
entry followed (FR-OPS-008); FR-OPS-009 closes with the catalogue and
budget enforced; FR-CONN-010 closes here per BUILD_PLAN §19; the
diagnostics bundle inventory test (§15.1) passes with canaries.

### 18.10 R9 — measured 50k-connection target published

Evidence: SOAK-001 through SOAK-006 and LOAD-001 executed on the
BENCHMARK_PLAN-designated environment with full statistical treatment; the
published report lands under `reports/bench/r9/` with GC evidence
(NFR-PERF-006), environment disclosure, and claims-ladder classification.
Operations additions: the capacity model coefficients (FR-OPS-010) each
cite a benchmark row; FR-RESUME-009's storm measurement closes;
NFR-PERF-001/003/004 and all NFR-SCALE rows close; NFR-SEC-005 (all-legs
TLS measured configuration) closes per BUILD_PLAN §19. No number ships
outside the claims ladder.

### 18.11 R10 — packaging, deploy, upgrade, rollback, flagship = 1.0

Evidence: the full PKG matrix via `release.yml`; CHAOS-001;
`cross-version-fixtures` green (NFR-COMPAT-005); the scripted flagship
demonstration (PRODUCT_REQUIREMENTS §1.2, all six steps as repeatable
repository scenarios). Operations additions: first-run flow timed and
scripted (§14.1); LB requirements validated in the cluster ingress
(§14.4); uninstall/purge executed and swept (PKG-007, FR-OPS-011);
NFR-COMPAT-004/006 and the NFR-MAINT-002 final coverage floor close; the
release-candidate checklist (§20) is the closing artifact.

## 19. Requirement-to-Evidence Traceability Mechanics

### 19.1 Keeping BUILD_PLAN §19 honest

- Every test family row in §10 carries requirement IDs in its pass
  criteria; every automated test names its row ID in the test name
  (`TestPROTO004_InitTimeout`).
- The `pr / trace-check` job extracts all requirement IDs from
  PRODUCT_REQUIREMENTS §7 and §9, greps test names and row tables for
  coverage, and fails on: an ID owned by a gate at or below the highest
  `accepted` gate with zero owning tests; an ID cited by a test that does
  not exist in PRODUCT_REQUIREMENTS (invented IDs); and a §10 row ID with
  no matching test function once its earliest gate is `in progress`.
- The check reads the machine-readable mirror of BUILD_PLAN §19 so gate
  ownership disagreements surface as CI failures, not review comments.

### 19.2 Evidence storage

- Workflow artifacts: per §11.3 retention; gate-evidence runs pinned
  permanently.
- Benchmark reports: in-repo under `reports/bench/<area>/` with the raw
  capture, environment disclosure, and the generated summary; the claims
  ladder cites these paths.
- Gate closure records: `reports/gates/<gate>/<sha>/` containing the
  checklist, workflow run links, and copied artifact bundles; the gate's
  status flip to `accepted` in BUILD_PLAN lands in the same PR as the
  record.

## 20. Release-Candidate Readiness Checklist

The final pre-1.0 checklist, executed on the release-candidate SHA and
stored as the R10 closing artifact. Every line must be checked by a named
person with a link:

1. All gates R0–R10 green on the release SHA: pinned workflow runs for
   every §11.2 row.
2. `cross-version-fixtures` green against the previous release
   (NFR-COMPAT-005).
3. Docs status lint green: no forbidden phrases, every deliverable carrying
   exactly one of accepted/in progress/planned/deferred, statuses matching
   gate records (NFR-MAINT-004).
4. Claims-ladder audit of the root README and MARKETING_PLAN assets: every
   public claim maps to an accepted gate and a benchmark row where
   numeric; single-node/idle/memory-bus qualifiers present wherever
   required (§1.2, PRODUCT_REQUIREMENTS §10.2).
5. Security review sign-off: THREAT_MODEL residual-risk register reviewed
   against the shipped release; open vuln-triage issues at or above high
   severity: zero (NFR-SEC-010, §5.4).
6. Runbook rehearsal record: every §17.2 alert rehearsed with date, run
   link, and the operator who executed it (FR-OPS-008).
7. Flagship demonstration executed from the repository scripts end to end
   on the release artifacts (PRODUCT_REQUIREMENTS §1.2).
8. Flake quarantine list empty for all owning gates (§11.5).
9. Purge rehearsal on a production-shaped install (PKG-007).
10. Release notes reviewed under §13.3 rules.

## 21. Feature Exhaustiveness Audit

Every PRODUCT_REQUIREMENTS §5 API element and every §7/§9 requirement group
is owned by exactly one terminal gate (BUILD_PLAN §19 authoritative) and
appears in at least one matrix family; the trace-check (§19.1) enforces the
ID-level version of this table continuously.

| Surface element | Owning gate | Matrix families |
| --- | --- | --- |
| Client listener: `POST /graphql` queries/mutations (§5.1) | R1 | UNIT, CHAOS-007 |
| Client listener: WebSocket upgrade + subprotocol (§5.1) | R2 | PROTO, CONN |
| Operations over WebSocket (FR-GQL-015) | R2 | PROTO-008 |
| Admin listener: `/healthz`, `/readyz`, `/metrics` (§5.1) | R8 | CHAOS, FAN-012, metrics-contract |
| Admin: `/admin/v1/connections`, `/admin/v1/subscriptions` | R8 | CONN, AUTHZ-016 |
| Admin: `/admin/v1/drain` | R8 | CONN-006, CHAOS-002 |
| Admin: `/admin/v1/revocations` | R8 (surface), R5 (propagation SLO) | AUTHZ-007/013, CHAOS-009 |
| Admin: `/admin/v1/publish` | R5 | FAN-010 |
| Admin: `/admin/v1/config` | R8 | UNIT-001, PKG-005, §7.3 test |
| Admin: `/admin/v1/diagnostics` | R8 | §15.1 inventory test, UNIT-014 |
| Directive `@source` (§5.2) | R1 | UNIT-002 |
| Directive `@auth` (§5.2) | R3 | AUTHZ-015, UNIT-002 |
| Directive `@filterable` (§5.2) | R4 | INDEX, UNIT-002 |
| Directive `@backpressure` (§5.2) | R6 | BP-010 |
| Directive `@complexity` (§5.2) | R1 | UNIT-004 |
| CLI `conduit serve` (§5.3) | R1 | §7 phases, CHAOS-008 |
| CLI `conduit validate` (§5.3) | R1 | UNIT-001/002, §7.2 |
| CLI `conduit version` (§5.3) | R10 | PKG-005 |
| CLI `conduit doctor` (§5.3) | R10 | PKG-008 |
| Protocol extensions (`extensions.conduit`, `ping.payload.conduit`, resume request) (§5.4) | R7 | PROTO-009, RESUME, BP-007 |
| FR-GQL group (execution, adapters, limits) | R1 (13), R3 (010, 012) | UNIT, PROTO-008, CHAOS-007 |
| FR-SUB group (transport) | R2 | PROTO |
| FR-AUTH group | R3 (16), R5 (014, 015 fleet) | AUTHZ, CHAOS-006/009 |
| FR-FILT group | R4 | INDEX, UNIT-007 |
| FR-FAN group | R5 | FAN, CHAOS-003/004/005 |
| FR-CONN group | R6 (12), R2 (001), R8 (010) | CONN, BP, PROTO-013 |
| FR-RESUME group | R7 (8), R9 (009) | RESUME, SOAK-004 |
| FR-ADMIN group | R8 | AUTHZ-016, CONN-006, §15/§16 tests |
| FR-OPS group | R10 (11), R8 (008, 009) | PKG, CHAOS-001/008, §17.2 lint |
| NFR-PERF group | R4 (002), R6 (005), R9 (001, 003, 004, 006) | INDEX-010, §12.4, SOAK/LOAD |
| NFR-SCALE group | R9 | SOAK, LOAD |
| NFR-SEC group | per BUILD_PLAN §19 split | UNIT-003/008/014, AUTHZ, CONN-010, PROTO-010, canary suite |
| NFR-COMPAT group | R2 (001, 002), R7 (003), R10 (004, 005, 006) | PROTO-009, RESUME-012, PKG, cross-version fixtures |
| NFR-MAINT group | R0 (001, 004, 005, 006), R1 (003), R10 (002) | arch-check, docs lint, §5.1 budget check, mutation testing |

## Explicit Deferrals and Requirements Traced

Deferred from this plan, with the rule that nothing deferred may be used to
claim any gate complete (GLOSSARY status vocabulary):

- **deferred**: operational procedures for any second bus adapter; ADR-0004
  requires any future adapter to pass the full R5 fault matrix before being
  documented as supported, and no such adapter is planned for 1.0.
- **deferred**: Windows-native, FreeBSD, and 32-bit packaging, install, and
  test procedures (ADR-0011, PRODUCT_REQUIREMENTS §4.3); Windows users run
  the Linux container.
- **deferred**: durable-delivery operational tooling (cursor stores, replay
  from durable logs); ADR-0007 records the reopen trigger in
  OPEN_QUESTIONS.
- **deferred**: legacy `subscriptions-transport-ws` conformance machinery
  (ADR-0002).
- **deferred**: hard per-tenant performance-isolation verification in one
  process; compliance regimes requiring hard isolation deploy per-tenant
  fleets (ADR-0009), and this plan's tenancy evidence is namespace
  isolation only.
- **deferred**: SaaS/hosted control-plane operations of any kind
  (PRODUCT_REQUIREMENTS §4.3).

Requirements traced by this document: FR-GQL-001 through FR-GQL-015;
FR-SUB-001 through FR-SUB-012; FR-AUTH-001 through FR-AUTH-018; FR-FILT-001
through FR-FILT-010; FR-FAN-001 through FR-FAN-012; FR-CONN-001 through
FR-CONN-014; FR-RESUME-001 through FR-RESUME-009; FR-ADMIN-001 through
FR-ADMIN-008; FR-OPS-001 through FR-OPS-013; NFR-PERF-001 through
NFR-PERF-006; NFR-SCALE-001 through NFR-SCALE-006; NFR-SEC-001 through
NFR-SEC-010; NFR-COMPAT-001 through NFR-COMPAT-006; NFR-MAINT-001 through
NFR-MAINT-006. Gate ownership for every ID follows BUILD_PLAN §19; where
any statement here and that matrix disagree, the matrix controls. Gate R0
repository infrastructure in this document is `in progress`; every
later-gate deliverable remains `planned`.
