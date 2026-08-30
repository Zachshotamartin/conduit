# Codex Ultra — One-Shot Implementation Prompt for Conduit

Copy everything below the line into Codex Ultra, launched from
`/Users/zacharymartin/Desktop/portfolio_projects/Conduit`.

---

You are implementing Conduit end to end in one autonomous run: a self-hosted,
subscription-first GraphQL gateway, fully specified by the normative
documentation set already present in this directory. You do not design; you
execute the plan exactly as written. Where you must deviate, you record the
deviation the way the plan requires (a new ADR), never silently.

## 0. Ground rules (read before anything else)

1. Read, in this order, before writing any code:
   - `docs/README.md` (conflict-and-status precedence rules — they bind you)
   - `docs/PRODUCT_REQUIREMENTS.md` (all 145 requirement IDs)
   - `docs/BUILD_PLAN.md` in full — it is the execution script: gates R0–R10,
     tickets `Rn.mm` in order, each with a definition of done
   - `docs/ARCHITECTURE.md`, `docs/PROTOCOL_CONFORMANCE.md`,
     `docs/AUTHORIZATION_MODEL.md` (interfaces, state tables, wire shapes —
     use the exact typed signatures and close codes they define)
   - `docs/OPERATIONS_TEST_PLAN.md` (test families, harness classes, CI
     workflow inventory, flake policy)
   - `docs/BENCHMARK_PLAN.md`, `docs/THREAT_MODEL.md`,
     `docs/MARKETING_PLAN.md`, `docs/OPEN_QUESTIONS.md`, all 11 ADRs
2. The status vocabulary is law: `accepted` / `in progress` / `planned` /
   `deferred`. Nothing becomes `accepted` without its named automated gate
   green. A stub, type, or happy-path test is never completion.
3. Test-first is non-negotiable and must be visible in the commit sequence:
   for every parser, state transition, authorization decision, predicate
   compilation, and error category, the failing deterministic test is
   committed before or with the implementation (BUILD_PLAN §4.1 merge order).
4. No unearned claims, anywhere, ever: README, docs, PR bodies, commit
   messages. Single-node ≠ fleet; idle ≠ loaded; memory-bus ≠ NATS;
   microbenchmark ≠ product claim (BENCHMARK_PLAN claims ladder).
5. Do not touch `OPEN_QUESTIONS.md` items — every one has a fail-closed
   default that stands. Do not implement deferred features "while you're
   there."
6. Determinism: injected clocks only (no `time.Now()` outside
   `internal/clock`), seeded randomness with logged seeds, race detector on,
   no wall-clock sleeps for correctness.

## 1. Git, GitHub, and merge authority

You have full permission to commit, push, open PRs, and merge them once
green. Exercise it exactly like this:

1. First command: `gh auth status` — confirm account `Zachshotamartin` with
   `repo` and `workflow` scopes. Abort with instructions if not.
2. Execute BUILD_PLAN R0.02: `gh repo create Zachshotamartin/conduit
   --private`, push this documentation set as the first commit on `main`,
   then apply branch protection via `gh api` and verify by read-back.
3. Work in one branch per gate: `gate/r0`, `gate/r1`, … Commit per ticket
   (`Rn.mm`) or per coherent ticket group, conventional-commit format
   (`feat:`, `fix:`, `test:`, `docs:`, `ci:`, `chore:`), message body naming
   the ticket ID. No AI attribution lines in commits or PRs.
4. Open one PR per gate titled `Gate Rn: <capability unlocked>`. The PR body
   must contain: the ticket list with DoD confirmation, the §X.6 evidence
   matrix status, the §X.9 acceptance checklist with every box checked or
   explicitly marked blocked, and the claims-register update.
5. Merge the PR yourself when and only when all required checks are green
   and the §X.9 checklist is fully satisfied (or carries honest blocked
   markers per §3 below). Then start the next gate branch from `main`.
   Respect the one sanctioned overlap: R4 and R5 may proceed in parallel
   after R3 merges.
6. Never force-push `main`, never delete branches with unmerged evidence,
   never weaken branch protection to get a merge through. If CI is red, fix
   the code or the test — in that order of suspicion.

## 2. Execution order

Follow BUILD_PLAN §23 (immediate execution order) into §5–§15 ticket by
ticket: R0 (repo, toolchain pin, `internal/clock`, `internal/errors`,
archcheck, docs/claims lints, CI workflows) → R1 (bounded intake, SDL +
directives, executor, complexity, Postgres/HTTP/function sources, publish
seam) → R2 (protocol state table, codecs, transport, registry, outbound
queue, memory bus, timing wheel, single-node fanout, conformance +
hostile suites, fuzzing) → R3 (principals, rule engine, OIDC/API-key/custom
modes, both enforcement points, decision cache, revocation, expiry,
degraded mode, tenancy, mutation testing ≥85%) → R4 ∥ R5 (predicate
compiler + counting index + differential oracle + W7 benchmark; NATS
adapter + dedupe + fault matrix + fleet fixture + revocation SLO) → R6
(backpressure policies, quotas, rate limits, fd budget, stalled-consumer
and DoS batteries, zero-alloc delivery closure) → R7 (replay buffers,
HMAC resume tokens, splice with 10k-interleaving proof, gap honesty, W8,
contract freeze) → R8 (admin API, metrics catalogue + cardinality
contract test, tracing, drain, reload, dashboards, alerts, runbook) → R9
(loadgen, workloads) → R10 (reproducible builds, SBOM/cosign/provenance,
Kubernetes, cross-version fixtures, flagship `examples/orderboard`,
uninstall/purge, launch assets, 1.0 checklist).

The exact interface names, close codes (4400/4401/4403/4406/4408/4409/4429,
4700–4704), defaults (init 3 s, keepalive 25 s, idle 5 min, lifetime 12 h
±10%, inbound 512 KiB, queue 256/1 MiB, replay 4,096/16 MiB, revocation
p99 ≤ 2 s, drain 60 s), and wire shapes are all specified — do not invent
variants.

## 3. Honesty protocol for environment limits

Some evidence physically cannot be produced on your machine. Do not fake it
and do not skip the code:

- **Runnable everywhere (must be green to merge)**: all deterministic
  suites, race-detector suites, property/differential suites, fuzz smoke,
  conformance against the containerized reference `graphql-ws` client,
  testcontainer suites (NATS, Postgres), kind-based fleet fixtures at CI
  scale, microbenchmarks (W7), allocation regression tests, packaging and
  reproducibility checks.
- **Requires reference hardware (Env-A/Env-B per BENCHMARK_PLAN §3)**: the
  R9 headline runs (50k connections, W1–W6, W8–W10 at target scale) and any
  number destined for the claims register at L2/L3. Implement
  `cmd/conduit-loadgen` completely, implement every workload, run each at
  the largest honest local scale (e.g. 5–10k connections), and file the
  results as **smoke evidence only** — clearly labeled, never promoted.
  Mark R9 (and the R9-dependent rows of R10) `in progress — blocked on
  reference hardware` in BUILD_PLAN §2 and the gate PR, with the exact
  commands a human must run on Env-A/B to finish acceptance.
- The claims register in `docs/MARKETING_PLAN.md` flips a row to `earned`
  only on real gate acceptance. If R9 is blocked, C9/C10 stay `unearned`
  and the README says so. A fabricated number is the one unrecoverable
  failure mode of this entire run.

## 4. Continuous obligations (every gate, no exceptions)

- Update BUILD_PLAN §2 (current baseline) and the root README status
  snapshot in the same PR that changes the truth.
- Update the claims register per gate (§X.8/§X.9 obligations).
- Keep the docs-status lint, claims lint, and archcheck green — they are
  required checks from R0 onward and you built them first for exactly this
  reason.
- Record any deviation from a spec decision as a new ADR
  (`docs/decisions/ADR-00NN-*.md`, template provided) in the same PR —
  never a silent edit to an earlier decision or an existing ADR.
- Every gate PR closes with explicit deferrals and requirements traced,
  mirroring §X.10/§X.11.

## 5. Definition of done for this run

You are done when: gates R0–R8 and R10's environment-independent tickets
are `accepted` with merged, green PRs; R9 is either `accepted` (if
reference hardware was available) or `in progress — blocked on reference
hardware` with the harness complete and the human runbook written; the
traceability matrix (BUILD_PLAN §19) status column matches reality for all
145 requirement IDs; `main` is green on every workflow; and the final
commit updates README + BUILD_PLAN §2 to the new honest claim. End your run
with a report: per-gate status, PR links, test/coverage/mutation numbers,
every blocked item with its unblock command, and any ADRs you added.

Begin with `gh auth status`, then read the nine documents, then execute
R0.01 onward. Do not ask for confirmation at any point; the plan has
already made every decision you need.
