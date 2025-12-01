SHELL := /bin/sh
GO ?= go
BINARY := opensnitch-tui
CMD := ./cmd/opensnitch-tui
BIN_DIR := ./bin
ARGS ?=

.PHONY: build
build: $(BIN_DIR)/$(BINARY)

.PHONY: build-all
build-all:
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

$(BIN_DIR)/$(BINARY):
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(BINARY) $(CMD)

.PHONY: proto
proto:
	protoc -I references/opensnitch/proto references/opensnitch/proto/ui.proto \
		--go_out internal/pb/protocol --go-grpc_out internal/pb/protocol \
		--go_opt=paths=source_relative --go-grpc_opt=paths=source_relative
