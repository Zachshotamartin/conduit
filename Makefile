SHELL := /bin/sh

GO ?= go
GO_TEST_FLAGS ?= -race -shuffle=on

.PHONY: check-gh build test

check-gh:
	./scripts/check-gh.sh

build:
	$(GO) build ./...

test:
	$(GO) test $(GO_TEST_FLAGS) ./...
