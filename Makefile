# Kube Sentinel Makefile

# Variables
APP_NAME := kube-sentinel
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go settings
GO := go
GOFLAGS := -trimpath
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

# Docker settings
DOCKER_REGISTRY ?= ghcr.io/kube-sentinel
DOCKER_IMAGE := $(DOCKER_REGISTRY)/$(APP_NAME)
DOCKER_TAG ?= $(VERSION)

# Build directories
BUILD_DIR := ./build
BIN_DIR := $(BUILD_DIR)/bin

.PHONY: all build build-linux build-darwin build-windows clean test lint fmt vet docker docker-push run help

all: build

## Build

build: ## Build the binary for current platform
	@echo "Building $(APP_NAME) $(VERSION)..."
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME) ./cmd/$(APP_NAME)

build-linux: ## Build for Linux (amd64 and arm64)
	@echo "Building for Linux..."
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-linux-amd64 ./cmd/$(APP_NAME)
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-linux-arm64 ./cmd/$(APP_NAME)

build-darwin: ## Build for macOS (amd64 and arm64)
	@echo "Building for macOS..."
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-amd64 ./cmd/$(APP_NAME)
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-arm64 ./cmd/$(APP_NAME)

build-windows: ## Build for Windows
	@echo "Building for Windows..."
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-windows-amd64.exe ./cmd/$(APP_NAME)

build-all: build-linux build-darwin build-windows ## Build for all platforms

## Development

run: build ## Build and run locally
	$(BIN_DIR)/$(APP_NAME) --config config.yaml --rules rules.yaml --log-level debug

dev: ## Run with hot reload (requires air)
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	air

## Testing

test: ## Run tests
	$(GO) test -v -race -cover ./...

test-coverage: ## Run tests with coverage report
	@mkdir -p $(BUILD_DIR)
	$(GO) test -v -race -coverprofile=$(BUILD_DIR)/coverage.out ./...
	$(GO) tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html
	@echo "Coverage report: $(BUILD_DIR)/coverage.html"

## Code Quality

lint: ## Run linter
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

fmt: ## Format code
	$(GO) fmt ./...
	@which goimports > /dev/null || (echo "Installing goimports..." && go install golang.org/x/tools/cmd/goimports@latest)
	goimports -w .

vet: ## Run go vet
	$(GO) vet ./...

check: fmt vet lint test ## Run all checks

## Docker

docker: ## Build Docker image
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest \
		.

docker-push: docker ## Push Docker image to registry
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

docker-buildx: ## Build multi-arch Docker image
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest \
		--push \
		.

## Kubernetes

deploy: ## Deploy to Kubernetes using kustomize
	kubectl apply -k deploy/kubernetes/

undeploy: ## Remove from Kubernetes
	kubectl delete -k deploy/kubernetes/

logs: ## View pod logs
	kubectl logs -n kube-sentinel -l app.kubernetes.io/name=kube-sentinel -f

port-forward: ## Port forward to access dashboard locally
	kubectl port-forward -n kube-sentinel svc/kube-sentinel 8080:80

## Dependencies

deps: ## Download dependencies
	$(GO) mod download

deps-update: ## Update dependencies
	$(GO) get -u ./...
	$(GO) mod tidy

## Cleanup

clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
	rm -f $(APP_NAME)

## Help

help: ## Show this help
	@echo "Kube Sentinel - Kubernetes Error Prioritization & Auto-Remediation"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
