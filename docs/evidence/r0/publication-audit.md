# R0 early-publication audit evidence

Document status: in progress.

- Date: 2026-08-30
- Repository: <https://github.com/Zachshotamartin/conduit>
- Authorization and deviation record:
  [ADR-0014](../../decisions/ADR-0014-authorize-early-public-repository.md)

## Visibility and protection

GitHub CLI read-back reports `visibility: PUBLIC` and `isPrivate: false`.
The checked-in `.github/branch-protection.json` policy was then applied to
`main` and read back successfully. The API response confirms:

- strict required status checks with all fifteen configured contexts;
- one approving review with stale-review dismissal;
- administrator enforcement and required linear history; and
- force pushes and branch deletion disabled.

This is repository-control evidence only. It does not accept R0 or any product
gate.

## Full-history secret scan

Gitleaks v8.30.1 scanned all repository history reachable at audit time with:

```sh
gitleaks git --redact --no-banner \
  --report-format json --report-path /tmp/conduit-gitleaks-report.json .
```

The first scan found one generic-key heuristic match: the literal
`b64u.opaque.signed` resume-token example in
`docs/PROTOCOL_CONFORMANCE.md`. It is a deliberately opaque protocol example,
not a credential. `.gitleaks.toml` contains a path-and-literal-specific
allowlist for only that example. The repeat scan examined approximately
1.81 MB across 66 commits and reported zero leaks. The JSON report contained
zero findings.

## Licenses and claims

The repository dependency audit reported `depsaudit: no findings`; every
vendored dependency had its pinned review, allowed license, and expected source
digest. The documentation-status and claims-ladder linters also passed.

There is no root project license file, and GitHub reports `licenseInfo: null`.
That is recorded as the current legal posture rather than silently selecting a
license: public visibility grants no open-source license, and this audit makes
no open-source claim. A distributable R10 release still requires an explicit
owner-selected project license or an equally explicit all-rights-reserved
distribution decision.

## Remaining conditions

R0 remains `in progress` until PR #1 has an independent approval, all protected
checks rerun green on its final head, it merges, and the `nightly` workflow
passes from a real `schedule` event on the default branch. No later gate may
merge first.
