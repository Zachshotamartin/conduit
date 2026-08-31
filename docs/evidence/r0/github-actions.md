# R0 GitHub Actions evidence

Document status: in progress.

- Date: 2026-08-30
- Pull request: [Gate R0 PR #1](https://github.com/Zachshotamartin/conduit/pull/1)
- Green recovery SHA: `e05a5043fe167d91900fce191ca966a26bc4ea30`

## Green remote matrix

The recovery SHA completed all 17 pull-request checks successfully:

- [`pr` workflow run 33340621359](https://github.com/Zachshotamartin/conduit/actions/runs/33340621359):
  all 11 protected-context jobs plus both platform-correctness jobs passed;
- [`integration` workflow run 33340621342](https://github.com/Zachshotamartin/conduit/actions/runs/33340621342):
  all four R0 scaffold integration jobs passed;
- [clean macOS 14 correctness job](https://github.com/Zachshotamartin/conduit/actions/runs/33340621359/job/99335494079):
  `make bootstrap`, `make check`, and `make test` passed in 4m31s; and
- [clean Linux arm64 correctness job](https://github.com/Zachshotamartin/conduit/actions/runs/33340621359/job/99335494084):
  the same exact chain passed in 6m24s.

The integration workflow currently exercises R0 scaffold packages. Its green
state is workflow-wiring evidence, not product or external-service integration
evidence.

## Deliberate negative proof and recovery

Commit `cf357dc68a63673c3abc5c9727cc259cf65d480c` temporarily added a
clearly labeled documentation file containing the forbidden placeholder
marker. The resulting [`pr` workflow run 33340571002](https://github.com/Zachshotamartin/conduit/actions/runs/33340571002)
completed with `failure`. Its
[`docs-status-lint` job](https://github.com/Zachshotamartin/conduit/actions/runs/33340571002/job/99335354906)
failed in the expected step and named the exact fixture path and forbidden
marker.

Commit `e05a5043fe167d91900fce191ca966a26bc4ea30` removed that fixture. The
17-check matrix above then returned fully green. The deliberate fixture does
not exist in the current tree.

## GitHub-hosted status

The repository owner authorized early publication in ADR-0014. The checked-in
branch-protection policy has now been applied and verified by API read-back;
the former account-tier blocker is resolved. Exact evidence is recorded in
[branch-protection.md](branch-protection.md) and
[publication-audit.md](publication-audit.md).

R0 remains `in progress` because two acceptance conditions are still
unsatisfied:

1. PR #1 requires an approval from an eligible GitHub account other than its
   author, followed by a green rerun on the final protected head.
2. `nightly.yml` has not completed a scheduled run. A CLI dispatch attempt on
   `gate/r0` returned HTTP 404 because GitHub requires the workflow to exist on
   the default branch before it can be manually dispatched. Scheduled
   workflows are likewise evaluated from the default branch. ADR-0014 records
   the two-phase finalization: merge only after review and protected checks,
   then record R0 acceptance after the first real schedule-triggered run.

No R0 claim is earned, the PR remains a draft, and R1 must not start while
these blockers remain except for the provisional ordered work authorized by
ADR-0013. No later gate may merge first.
