SHELL := /bin/sh
GO ?= go
BINARY := opensnitch-tui
CMD := ./cmd/opensnitch-tui
ARGS ?=

.PHONY: build
build:
	$(GO) build ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: run
run:
	$(GO) run $(CMD) $(ARGS)
