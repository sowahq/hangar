.PHONY: build test test-race vet fmt run docker docker-run clean tidy install proto help

BINARY := hangar
PKG    := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
IMAGE   := hangar:$(VERSION)

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-15s %s\n",$$1,$$2}'

build: ## Build the binary
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) .

test: ## Run tests
	go test $(PKG)

test-race: ## Run tests with race detector
	go test -race $(PKG)

vet: ## Run go vet
	go vet $(PKG)

fmt: ## Format code
	go fmt $(PKG)

tidy: ## Tidy modules
	go mod tidy

run: build ## Build and run server
	./bin/$(BINARY) server -c config.toml

docker: ## Build docker image
	docker build -t $(IMAGE) .

docker-run: docker ## Run docker image with mounted data dir
	docker run --rm -p 8080:8080 -v $(PWD)/data:/data $(IMAGE)

install: ## Install binary into GOPATH/bin
	go install -trimpath -ldflags="$(LDFLAGS)" .

clean: ## Remove build artifacts
	rm -rf bin/ data/

proto: ## Regenerate dRPC stubs from .proto files
	@command -v protoc >/dev/null || { echo "protoc not found in PATH"; exit 1; }
	go install google.golang.org/protobuf/cmd/protoc-gen-go
	go install storj.io/drpc/cmd/protoc-gen-go-drpc
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-drpc_out=. --go-drpc_opt=paths=source_relative \
		internal/api/rpc/service.proto
