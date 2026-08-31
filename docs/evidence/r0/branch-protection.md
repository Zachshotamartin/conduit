# R0 branch-protection evidence

Status: in progress.

The intended `main` protection policy is checked in at
`.github/branch-protection.json`. On 2026-08-30, applying that policy while the
repository was private returned GitHub HTTP 403 because the account tier did
not include private-repository branch protection. The repository owner then
authorized early public visibility in ADR-0014.

After publication, these commands succeeded:

```sh
gh api --method PUT repos/Zachshotamartin/conduit/branches/main/protection \
  --input .github/branch-protection.json
gh api repos/Zachshotamartin/conduit/branches/main/protection
```

Read-back confirmed strict required checks, one approving review,
stale-review dismissal, administrator enforcement, required linear history,
and disabled force pushes and deletion. The remaining R0 blockers are the
independent approval and a real schedule-triggered nightly run after the
workflow reaches the default branch.
