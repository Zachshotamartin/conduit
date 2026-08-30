SHELL := /bin/sh

override GOTOOLCHAIN := go1.26.7
export GOTOOLCHAIN

GO ?= go

bootstrap: check-go
	GO="$(GO)" ./scripts/bootstrap.sh

check-go:
	@actual_version="$$($(GO) version | awk '{print $$3}')"; \
	test "$$actual_version" = "$(GOTOOLCHAIN)" || { \
		printf 'Go toolchain must be $(GOTOOLCHAIN); got %s\n' "$$actual_version" >&2; \
		exit 1; \
	}

build: check-go
test: check-go
check: check-go
lint: check-go
staticcheck: check-go
vet: check-go
arch-check: check-go
docs-status-lint: check-go
metrics-contract: check-go
deps-audit: check-go
trace-check: check-go
ci-contract: check-go
unit-race: check-go
proto-race: check-go
authz-race: check-go
index-race: check-go
conformance: check-go
integration: check-go
bench-smoke: check-go
