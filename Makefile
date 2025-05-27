# SSH Tunnel System Makefile

# Build variables
BINARY_SERVER = tunnel-server
BINARY_CLIENT = tunnel-client
BINARY_CLI = tunnel-cli

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME = $(shell date -u '+%Y-%m-%d_%H:%M:%S')
COMMIT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go build flags
LDFLAGS = -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.Commit=$(COMMIT)"
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Build directories
BUILD_DIR = build
DIST_DIR = dist

.PHONY: all build clean test fmt vet deps help dev-setup

# Default target
all: build

# Build all binaries
build: build-server build-client build-cli

# Build server
build-server:
	@echo "Building tunnel-server..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER) ./cmd/tunnel-server

# Build client
build-client:
	@echo "Building tunnel-client..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_CLIENT) ./cmd/tunnel-client

# Build CLI
build-cli:
	@echo "Building tunnel-cli..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_CLI) ./cmd/tunnel-cli

# Cross-compile for multiple platforms
build-all: clean
	@echo "Cross-compiling for multiple platforms..."
	@mkdir -p $(DIST_DIR)
	
	# Linux amd64
	@echo "Building for Linux amd64..."
	@GOOS=linux GOARCH=amd64 $(MAKE) build
	@mkdir -p $(DIST_DIR)/linux-amd64
	@cp $(BUILD_DIR)/* $(DIST_DIR)/linux-amd64/
	
	# Linux arm64
	@echo "Building for Linux arm64..."
	@GOOS=linux GOARCH=arm64 $(MAKE) build
	@mkdir -p $(DIST_DIR)/linux-arm64
	@cp $(BUILD_DIR)/* $(DIST_DIR)/linux-arm64/
	
	# Windows amd64
	@echo "Building for Windows amd64..."
	@GOOS=windows GOARCH=amd64 $(MAKE) build
	@mkdir -p $(DIST_DIR)/windows-amd64
	@cp $(BUILD_DIR)/* $(DIST_DIR)/windows-amd64/
	
	# macOS amd64
	@echo "Building for macOS amd64..."
	@GOOS=darwin GOARCH=amd64 $(MAKE) build
	@mkdir -p $(DIST_DIR)/darwin-amd64
	@cp $(BUILD_DIR)/* $(DIST_DIR)/darwin-amd64/
	
	# macOS arm64
	@echo "Building for macOS arm64..."
	@GOOS=darwin GOARCH=arm64 $(MAKE) build
	@mkdir -p $(DIST_DIR)/darwin-arm64
	@cp $(BUILD_DIR)/* $(DIST_DIR)/darwin-arm64/

# Development setup
dev-setup:
	@echo "Setting up development environment..."
	@go mod download
	@go mod tidy
	@mkdir -p configs logs
	@cp configs/server.example.yaml configs/server.yaml 2>/dev/null || true
	@cp configs/client.example.yaml configs/client.yaml 2>/dev/null || true
	@echo "Development environment ready!"

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Vet code
vet:
	@echo "Vetting code..."
	@go vet ./...

# Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@golangci-lint run

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	@go clean

# Run server locally
run-server: build-server
	@echo "Running tunnel-server..."
	@./$(BUILD_DIR)/$(BINARY_SERVER) -config configs/server.yaml

# Run client locally  
run-client: build-client
	@echo "Running tunnel-client..."
	@./$(BUILD_DIR)/$(BINARY_CLIENT) -config configs/client.yaml

# Generate SSH keys for development
gen-keys:
	@echo "Generating SSH keys for development..."
	@mkdir -p keys
	@ssh-keygen -t ed25519 -f keys/server_host_key -N "" -C "tunnel-server@dev"
	@ssh-keygen -t ed25519 -f keys/client_key -N "" -C "tunnel-client@dev"
	@echo "Keys generated in keys/ directory"

# Docker build
docker-build:
	@echo "Building Docker images..."
	@docker build -t ssh-tunnel-server -f deploy/Dockerfile.server .
	@docker build -t ssh-tunnel-client -f deploy/Dockerfile.client .

# Help
help:
	@echo "SSH Tunnel System - Available commands:"
	@echo ""
	@echo "Build commands:"
	@echo "  build          - Build all binaries"
	@echo "  build-server   - Build tunnel-server"
	@echo "  build-client   - Build tunnel-client"
	@echo "  build-cli      - Build tunnel-cli"
	@echo "  build-all      - Cross-compile for all platforms"
	@echo ""
	@echo "Development commands:"
	@echo "  dev-setup      - Setup development environment"
	@echo "  run-server     - Run server locally"
	@echo "  run-client     - Run client locally"
	@echo "  gen-keys       - Generate SSH keys for development"
	@echo ""
	@echo "Quality commands:"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage"
	@echo "  fmt            - Format code"
	@echo "  vet            - Vet code"
	@echo "  lint           - Run linter (requires golangci-lint)"
	@echo ""
	@echo "Other commands:"
	@echo "  clean          - Clean build artifacts"
	@echo "  deps           - Install dependencies"
	@echo "  docker-build   - Build Docker images"
	@echo "  help           - Show this help"
