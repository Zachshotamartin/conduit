# ADR-0014: Authorize Early Public Repository

Document status: accepted.

- Decision state: accepted
- Date: 2026-08-30
- Related findings or requirements: gate R0, gate R10, NFR-MAINT-004,
  NFR-SEC-010

## Context

ADR-0013 preserved a private-development boundary because the original build
plan scheduled publication for R10. On 2026-08-30, the repository owner
explicitly authorized making `Zachshotamartin/conduit` public immediately and
confirmed that public visibility is acceptable before the portfolio is
published. GitHub's free-plan restriction had prevented branch protection and
scheduled-workflow evidence while the repository was private.

Changing visibility early reverses only the publication-timing decision. It
does not make unfinished product claims true, satisfy a gate, grant a software
license, or remove the required independent review and automated evidence.

## Decision

The repository may be public before R10 under the owner's explicit
authorization. This ADR supersedes ADR-0013 only where ADR-0013 requires the
repository to remain private until R10; its honesty rules, ordered gate stack,
and prohibition on unearned acceptance remain binding until the stack is
unwound.

The following controls apply:

- Run a full-history secret scan immediately and check in the exact scanner
  allowlist. Every exception must be narrow, reviewable, and demonstrably
  non-secret.
- Run the existing dependency-license, documentation-status, and claims
  audits. Public visibility does not imply an open-source license; no license
  grant is claimed while the repository has no root license file.
- Apply the checked-in `main` protection policy and verify it by API read-back.
- Keep every gate `in progress` until its own acceptance conditions pass. No
  README, marketing, security, compatibility, performance, or release claim is
  promoted merely because the source is visible.
- The R0 nightly schedule can run only after its workflow reaches the default
  branch. R0 therefore remains in progress through independent review and
  merge; acceptance is recorded only after the first real schedule-triggered
  run passes. R1 may continue provisionally but cannot merge first.

## Alternatives Considered

- **Keep the repository private until R10:** rejected because the owner
  explicitly changed the publication requirement and accepted early
  visibility.
- **Make it public and mark R0 accepted immediately:** rejected because
  visibility is not evidence for required checks, review, or scheduled
  execution.
- **Add an open-source license automatically:** rejected because choosing a
  license grants legal rights and the owner authorized visibility, not a
  specific license.
- **Weaken protection to avoid review:** rejected because the checked-in policy
  and independent-review requirement remain mandatory.

## Consequences

Branch protection is now available on the current account tier, and the
repository can be inspected publicly. Incomplete security-sensitive code and
documentation are also visible earlier than originally planned, so status and
claims discipline remain especially important. A future owner-selected license
can be added explicitly; until then, visibility conveys no repository license
grant. The remaining non-automatable action is approval by an eligible GitHub
account other than the pull-request author.
