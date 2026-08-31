#!/bin/sh
set -eu

go_command="${GO:-go}"
gofmt_command="$(dirname "$go_command")/gofmt"
if [ ! -x "$gofmt_command" ]; then
  gofmt_command="$(command -v gofmt)"
fi

go_files="$(git ls-files -- '*.go' ':!:vendor/**')"
if [ -z "$go_files" ]; then
  exit 0
fi

unformatted="$($gofmt_command -l $go_files)"
if [ -n "$unformatted" ]; then
  printf '%s\n' 'gofmt is required for:' >&2
  printf '%s\n' "$unformatted" >&2
  exit 1
fi
