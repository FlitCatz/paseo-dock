.PHONY: build test vet clean

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/paseo-sbx ./cmd/paseo-sbx
	CGO_ENABLED=0 go build -trimpath -o bin/agent-wrapper ./cmd/agent-wrapper
	CGO_ENABLED=0 go build -trimpath -o bin/install-node ./cmd/install-node

test:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf bin
