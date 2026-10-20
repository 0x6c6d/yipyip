.PHONY: build check install

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

build:
	go build ./...
	go build -ldflags "$(LDFLAGS)" -o bin/yip ./cmd/yip

check:
	@files="$$(gofmt -l .)"; if [ -n "$$files" ]; then echo "gofmt needed for:"; echo "$$files"; exit 1; fi
	go vet ./...
	go test ./...
	go build ./...

install:
	@set -e; \
	dir="$$(go env GOBIN)"; \
	if [ -z "$$dir" ]; then dir="$$(go env GOPATH)/bin"; fi; \
	mkdir -p "$$dir"; \
	go build -ldflags "$(LDFLAGS)" -o "$$dir/yip" ./cmd/yip
