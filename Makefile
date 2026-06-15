.DEFAULT_GOAL := help

BINARY_NAME=yttv-bridge
VERSION ?= 0.0.0
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X 'main.Version=$(VERSION)' \
	-X 'main.GitCommit=$(GIT_COMMIT)' \
	-X 'main.BuildDate=$(BUILD_DATE)'

IMAGE ?= ghcr.io/ygelfand/yttv-bridge
IMAGE_TAG ?= dev

##@ Development

.PHONY: build
build: ## Build to ./bin/yttv-bridge
	go fmt ./...
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) .

.PHONY: run
run: ## go run . $(ARGS)
	go run . $(ARGS)

.PHONY: serve
serve: ## Run the daemon
	go run . serve

.PHONY: test
test: ## Run tests
	go test -v ./...

.PHONY: fmt
fmt: ## Format Go source
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

##@ Container

.PHONY: docker-build
docker-build: ## Build the Docker image
	docker build -t $(IMAGE):$(IMAGE_TAG) .

.PHONY: docker-run
docker-run: ## Run the image
	docker run --rm --network host \
		-e YTTV_SAPISID -e YTTV_SECURE_3PSID -e YTTV_GOOGLE_ACCOUNT_ID \
		-e YTTV_LISTEN -e YTTV_LOG_LEVEL -e YTTV_LOG_FORMAT \
		-e YTTV_EPG_REFRESH -e YTTV_DISCOVER_REFRESH -e YTTV_DISCOVER_TIMEOUT \
		$(IMAGE):$(IMAGE_TAG)

##@ Build & Release

.PHONY: install
install: ## go install
	go install -ldflags "$(LDFLAGS)" .

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
