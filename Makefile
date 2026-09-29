.PHONY: all build test clean install check fmt fmt-check vet test-race

BINARY_NAME=rukia
BIN_DIR=bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X github.com/Chavao/rukia-player/internal/app.Version=$(VERSION)

export PKG_CONFIG_PATH := /usr/lib/x86_64-linux-gnu/pkgconfig:$(PKG_CONFIG_PATH)

all: check build

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/rukia

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/rukia

fmt:
	gofmt -s -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Unformatted files found:" && gofmt -l . && exit 1)

vet:
	go vet ./...

test:
	go test -v ./...

test-race:
	go test -race ./...

check: fmt-check vet test test-race

clean:
	rm -rf $(BIN_DIR)
