#!/bin/sh
set -eu

violations="$(git grep -n -E 'time\.(Sleep|Tick|NewTicker|NewTimer)\(' -- '*_test.go' || true)"
if [ -n "$violations" ]; then
  printf '%s\n' 'deterministic tests must use the injected clock:' >&2
  printf '%s\n' "$violations" >&2
  exit 1
fi
