.PHONY: build test test-helpers vet clean

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/paseo-dock ./cmd/paseo-dock
	CGO_ENABLED=0 go build -trimpath -o bin/agent-wrapper ./cmd/agent-wrapper
	CGO_ENABLED=0 go build -trimpath -o bin/install-runtime ./cmd/install-runtime

test:
	go test -race ./...
	$(MAKE) test-helpers

# Go's ./... pattern excludes testdata; these tests use fake runtime boundaries.
# Select category-verify files explicitly: that directory has other main programs.
test-helpers:
	go test -race ./internal/operations/testdata/userns-acceptance
	go test -race internal/tui/testdata/management-tui-live/category-verify.go internal/tui/testdata/management-tui-live/category-verify_test.go

vet:
	go vet ./...
	go vet ./internal/operations/testdata/userns-acceptance
	go vet internal/tui/testdata/management-tui-live/category-verify.go internal/tui/testdata/management-tui-live/category-verify_test.go

clean:
	rm -rf bin
