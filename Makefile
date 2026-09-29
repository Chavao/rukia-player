.PHONY: all build test clean

BINARY_NAME=rukia
BIN_DIR=bin

all: test build

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/rukia

test:
	go test -v ./...

clean:
	rm -rf $(BIN_DIR)
