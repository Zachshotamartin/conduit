# R0 branch-protection evidence

Status: in progress.

The intended `main` protection policy is checked in at
`.github/branch-protection.json`. On 2026-08-30, applying that policy to
the private `Zachshotamartin/conduit` repository returned GitHub HTTP 403:
private-repository branch protection requires GitHub Pro for this account.

R0.02 and R0 acceptance remain blocked until the account is upgraded or the
repository reaches the R10 publication gate. The policy must not be weakened
to bypass this prerequisite.

After the prerequisite changes, apply and verify the policy with:

```sh
gh api --method PUT repos/Zachshotamartin/conduit/branches/main/protection \
  --input .github/branch-protection.json
gh api repos/Zachshotamartin/conduit/branches/main/protection
```
