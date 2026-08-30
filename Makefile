SHELL := /bin/sh

GO ?= go
GO_TEST_FLAGS ?= -race -shuffle=on

.PHONY: bootstrap check-gh check-go build test check lint staticcheck vet arch-check \
	docs-status-lint metrics-contract deps-audit trace-check ci-contract \
	unit-race proto-race authz-race index-race conformance integration bench-smoke

bootstrap:
	GO="$(GO)" ./scripts/bootstrap.sh

check-gh:
	./scripts/check-gh.sh

check-go:
	@test "$$($(GO) version | awk '{print $$3}')" = "go1.23.12" || { \
		printf 'Go toolchain must be go1.23.12; got %s\n' "$$($(GO) version | awk '{print $$3}')" >&2; \
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

lint:
	GO="$(GO)" ./scripts/check-format.sh
	./scripts/check-determinism.sh
	./bin/golangci-lint run --timeout=5m ./...

staticcheck:
	./bin/staticcheck ./...

vet:
	$(GO) vet ./...

arch-check:
	$(GO) run ./tools/archcheck -root .

docs-status-lint:
	$(GO) run ./tools/docsstatus docs
	$(GO) run ./tools/claimslint .

metrics-contract:
	$(GO) test -race ./internal/observability

deps-audit:
	$(GO) run ./tools/depsaudit -root .

trace-check:
	$(GO) run ./tools/tracecheck -root .

ci-contract:
	$(GO) run ./tools/cicontract -root .

unit-race:
	$(GO) test $(GO_TEST_FLAGS) ./...

proto-race:
	$(GO) test $(GO_TEST_FLAGS) ./internal/protocol ./test/conformance

authz-race:
	$(GO) test $(GO_TEST_FLAGS) ./internal/auth/...

index-race:
	$(GO) test $(GO_TEST_FLAGS) ./internal/filter/...

conformance:
	$(GO) test $(GO_TEST_FLAGS) ./internal/protocol ./test/conformance ./test/hostile

integration:
	$(GO) test $(GO_TEST_FLAGS) ./internal/bus/... ./internal/datasource/... ./test/fault

bench-smoke:
	$(GO) test -run '^$$' -bench '.' -benchtime=100ms ./...
