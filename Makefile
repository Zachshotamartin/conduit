SHELL := /bin/sh

override GOTOOLCHAIN := go1.26.7
export GOTOOLCHAIN

GO ?= go
GO_TEST_FLAGS ?= -race -shuffle=on

.PHONY: bootstrap check-gh check-go build test check lint staticcheck vet arch-check \
	docs-status-lint metrics-contract deps-audit trace-check ci-contract \
	unit-race proto-race authz-race index-race conformance integration bench-smoke

bootstrap: check-go
	GO="$(GO)" ./scripts/bootstrap.sh

check-gh:
	./scripts/check-gh.sh

check-go:
	@actual_version="$$($(GO) version | awk '{print $$3}')"; \
	test "$$actual_version" = "$(GOTOOLCHAIN)" || { \
		printf 'Go toolchain must be $(GOTOOLCHAIN); got %s\n' "$$actual_version" >&2; \
		exit 1; \
	}

build: check-go
	$(GO) build -mod=vendor ./...

test: check-go
	$(GO) test $(GO_TEST_FLAGS) ./...

check: check-go
	$(MAKE) vet
	$(MAKE) staticcheck
	$(MAKE) arch-check
	$(MAKE) lint
	$(MAKE) docs-status-lint
	$(MAKE) metrics-contract
	$(MAKE) deps-audit
	$(MAKE) trace-check
	$(MAKE) ci-contract

lint: check-go
	GO="$(GO)" ./scripts/check-format.sh
	./scripts/check-determinism.sh
	./bin/golangci-lint run --timeout=5m ./...

staticcheck: check-go
	./bin/staticcheck ./...

vet: check-go
	$(GO) vet ./...

arch-check: check-go
	$(GO) run ./tools/archcheck -root .

docs-status-lint: check-go
	$(GO) run ./tools/docsstatus docs
	$(GO) run ./tools/claimslint .

metrics-contract: check-go
	$(GO) test -race ./internal/observability

deps-audit: check-go
	$(GO) run ./tools/depsaudit -root .

trace-check: check-go
	$(GO) run ./tools/tracecheck -root .

ci-contract: check-go
	$(GO) run ./tools/cicontract -root .

unit-race: check-go
	$(GO) test $(GO_TEST_FLAGS) ./...

proto-race: check-go
	$(GO) test $(GO_TEST_FLAGS) ./internal/protocol ./test/conformance

authz-race: check-go
	$(GO) test $(GO_TEST_FLAGS) ./internal/auth/...

index-race: check-go
	$(GO) test $(GO_TEST_FLAGS) ./internal/filter/...

conformance: check-go
	$(GO) test $(GO_TEST_FLAGS) ./internal/protocol ./test/conformance ./test/hostile

integration: check-go
	$(GO) test $(GO_TEST_FLAGS) ./internal/bus/... ./internal/datasource/... ./test/fault

bench-smoke: check-go
	$(GO) test -run '^$$' -bench '.' -benchtime=100ms ./...
