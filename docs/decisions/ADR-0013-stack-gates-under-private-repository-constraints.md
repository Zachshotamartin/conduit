# ADR-0013: Stack Gates Under Private-Repository Constraints

Document status: in progress.

- Decision state: accepted
- Date: 2026-08-30
- Related findings or requirements: NFR-MAINT-004, gates R0 through R10

## Context

The gate plan requires every gate branch to merge only after its predecessor is
accepted, protected-branch requirements are read back from GitHub, required
checks pass, and an independent reviewer approves the pull request. The
repository must also remain private until R10. GitHub's API has returned an
HTTP 403 for both branch-protection and repository-ruleset configuration with
the explanation that the private repository must either be upgraded to a paid
plan or made public. GitHub also evaluates scheduled workflows only from the
default branch, so the R0 nightly workflow cannot produce schedule-triggered
evidence while it exists only on the unmerged R0 branch.

R0's local validation and pull-request workflows are green, but protection
readback, a schedule-triggered nightly run, and independent approval remain
unfulfilled. Treating R0 as accepted would therefore be false. Stopping all
engineering until R10 would make R10 unreachable, while making the repository
public now would violate the product plan's publication boundary.

## Decision

Until the authorized R10 publication step, Conduit may perform provisional
integration work on a strictly ordered stack of gate branches. Each
`gate/rN` branch is based on `gate/r(N-1)`, and its pull request targets that
immediate predecessor. The stack is development evidence only; it does not
change any gate's lifecycle status or earn any release claim.

The following controls are binding:

- R0 remains `in progress`. A provisionally active successor gate is also
  marked `in progress` so dependency and status audits describe the code that
  exists, while every untouched successor remains `planned`. No gate becomes
  `accepted` until its normative acceptance conditions actually pass.
- No stacked gate pull request may be merged or represented as accepted while
  a predecessor is unaccepted.
- Every branch reruns all inherited required checks in addition to its own
  gate-specific checks.
- Each pull request records the exact unresolved predecessor conditions and
  labels its results as provisional.
- The normal ticket order is preserved. Optional R4/R5 parallelism is not used
  while operating under this exception.
- Failures remain visible, and evidence records distinguish local, manual,
  pull-request, push, and schedule triggers.

At the R10 publication step, the repository is made public, branch protection
or an equivalent ruleset is configured and read back, and the nightly schedule
is observed from the default branch. The stacked pull requests are then
reviewed and integrated from R0 upward, one gate at a time. Each gate must be
rebased or retargeted as needed, independently approved, rerun against its
actual base, and accepted before its successor can merge. Any check invalidated
by the transition is rerun rather than inherited from the provisional stack.

## Evidence Boundary

The GitHub API responses and current workflow-run URLs belong in the R0
evidence bundle. Provisional work may cite those artifacts only as evidence of
what ran; it may not cite them as evidence that branch protection, scheduled
execution, review, acceptance, or release readiness exists. If publication or
independent review never occurs, the stacked branches remain unaccepted and no
public claim is promoted.

## Alternatives Considered

- **Stop after R0 until hosting policy changes**: rejected because the required
  publication step is itself in R10 and cannot be reached without performing
  the intervening implementation work.
- **Publish before R10**: rejected because it violates the explicit private
  development boundary and exposes incomplete security-sensitive code.
- **Purchase a paid GitHub plan automatically**: rejected because spending and
  account-plan changes require authority outside the repository implementation
  task.
- **Use the pull-request author or an automation identity as the reviewer**:
  rejected because GitHub does not count self-approval as independent review
  and manufacturing review evidence would defeat the gate.
- **Record protection in documentation without API readback**: rejected
  because prose is not executable enforcement evidence.

## Consequences

Development can proceed without falsifying acceptance, but the pull-request
stack grows and every affected check must run again during final integration.
GitHub history will clearly separate provisional implementation from accepted
gates. Publication, protection configuration, scheduled execution, and review
become an explicit R10 unwind sequence. The only required human action is an
approval from a GitHub account other than the pull-request author after each
gate is otherwise ready; the same eligible reviewer may approve the sequence.
