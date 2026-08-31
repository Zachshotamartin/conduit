#!/bin/sh
set -eu

go_command="${GO:-go}"
exec "$go_command" run -mod=vendor ./tools/determinismcheck -root .
