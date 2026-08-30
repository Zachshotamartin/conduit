#!/bin/sh
set -eu

expected_account="${CONDUIT_GITHUB_ACCOUNT:-Zachshotamartin}"

if ! auth_status="$(gh auth status --hostname github.com --active 2>&1)"; then
  printf '%s\n' "$auth_status" >&2
  printf '%s\n' "GitHub authentication failed; run: gh auth login --scopes repo,workflow" >&2
  exit 1
fi
printf '%s\n' "$auth_status"

active_account="$(gh api user --jq .login)"
if [ "$active_account" != "$expected_account" ]; then
  printf 'expected GitHub account %s, got %s\n' "$expected_account" "$active_account" >&2
  exit 1
fi

case "$auth_status" in
  *"'repo'"*) ;;
  *)
    printf '%s\n' "GitHub token is missing required scope: repo" >&2
    exit 1
    ;;
esac

case "$auth_status" in
  *"'workflow'"*) ;;
  *)
    printf '%s\n' "GitHub token is missing required scope: workflow" >&2
    exit 1
    ;;
esac

printf 'GitHub prerequisite verified for %s with repo and workflow scopes.\n' "$active_account"
