.PHONY: build test test-helpers vet clean

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/paseo-sbx ./cmd/paseo-sbx
	CGO_ENABLED=0 go build -trimpath -o bin/agent-wrapper ./cmd/agent-wrapper
	CGO_ENABLED=0 go build -trimpath -o bin/install-runtime ./cmd/install-runtime

test:
	go test -race ./...
	$(MAKE) test-helpers

# testdata is excluded from ./...; this directory contains other main programs.
test-helpers:
	go test -race internal/tui/testdata/management-tui-live/category-verify.go internal/tui/testdata/management-tui-live/category-verify_test.go

vet:
	go vet ./...
	go vet internal/tui/testdata/management-tui-live/category-verify.go internal/tui/testdata/management-tui-live/category-verify_test.go

clean:
	rm -rf bin
