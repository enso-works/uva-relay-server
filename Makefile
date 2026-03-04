.PHONY: build test lint run clean

BINARY := uvame-relay
VERSION ?= dev

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/relay

test:
	go test -race ./...

lint:
	golangci-lint run

run:
	go run ./cmd/relay

clean:
	rm -f $(BINARY)
