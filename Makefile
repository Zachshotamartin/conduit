SHELL := /bin/sh

GO ?= go
GO_TEST_FLAGS ?= -race -shuffle=on

.PHONY: bootstrap check-gh build test

bootstrap:
	GO="$(GO)" ./scripts/bootstrap.sh

check-gh:
	./scripts/check-gh.sh

build:
	$(GO) build ./...

test:
	$(GO) test $(GO_TEST_FLAGS) ./...
