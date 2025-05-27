# SSH Tunnel System Makefile

.PHONY: help build test test-verbose test-cover bench clean install deps lint fmt vet

# Default target
help:
	@echo "Available targets:"
	@echo "  build        - Build all binaries"
	@echo "  test         - Run all tests"
	@echo "  test-verbose - Run tests with verbose output"
	@echo "  test-cover   - Run tests with coverage"
	@echo "  bench        - Run benchmarks"
	@echo "  lint         - Run linter"
	@echo "  fmt          - Format code"
	@echo "  vet          - Run go vet"
	@echo "  clean        - Clean build artifacts"
	@echo "  install      - Install binaries to GOPATH/bin"
	@echo "  deps         - Download dependencies"

# Build targets
build:
	@echo "Building tunnel-server..."
	go build -o bin/tunnel-server ./cmd/tunnel-server
	@echo "Building tunnel-client..."
	go build -o bin/tunnel-client ./cmd/tunnel-client
	@echo "Building tunnel-keygen..."
	go build -o bin/tunnel-keygen ./cmd/tunnel-keygen
	@echo "Build complete!"

build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 go build -o bin/tunnel-server-linux ./cmd/tunnel-server
	GOOS=linux GOARCH=amd64 go build -o bin/tunnel-client-linux ./cmd/tunnel-client
	GOOS=linux GOARCH=amd64 go build -o bin/tunnel-keygen-linux ./cmd/tunnel-keygen

build-windows:
	@echo "Building for Windows..."
	GOOS=windows GOARCH=amd64 go build -o bin/tunnel-server.exe ./cmd/tunnel-server
	GOOS=windows GOARCH=amd64 go build -o bin/tunnel-client.exe ./cmd/tunnel-client
	GOOS=windows GOARCH=amd64 go build -o bin/tunnel-keygen.exe ./cmd/tunnel-keygen

# Test targets
test:
	@echo "Running tests..."
	go test -race ./...

test-verbose:
	@echo "Running tests with verbose output..."
	go test -race -v ./...

test-cover:
	@echo "Running tests with coverage..."
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-tunnel:
	@echo "Running tunnel package tests..."
	go test -race -v ./pkg/tunnel/...

bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./pkg/tunnel/

bench-tunnel:
	@echo "Running tunnel benchmarks..."
	go test -bench=. -benchmem -v ./pkg/tunnel/

# Quality targets
fmt:
	@echo "Formatting code..."
	go fmt ./...

vet:
	@echo "Running go vet..."
	go vet ./...

lint:
	@echo "Running golangci-lint..."
	golangci-lint run

# Dependency management
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Install targets
install:
	@echo "Installing binaries..."
	go install ./cmd/tunnel-server
	go install ./cmd/tunnel-client
	go install ./cmd/tunnel-keygen

# Clean targets
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	go clean ./...

# Development targets
dev-setup:
	@echo "Setting up development environment..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	mkdir -p bin keys logs

# Testing specific scenarios
test-server:
	@echo "Testing SSH tunnel server..."
	go test -race -run TestServer -v ./pkg/tunnel/

test-auth:
	@echo "Testing SSH authentication..."
	go test -race -run TestSSHAuth -v ./pkg/tunnel/

test-ports:
	@echo "Testing port allocation..."
	go test -race -run TestPort -v ./pkg/tunnel/

test-tunnels:
	@echo "Testing tunnel functionality..."
	go test -race -run TestReverseTunnel -v ./pkg/tunnel/

test-reliability:
	@echo "Testing system reliability..."
	go test -race -run TestTunnelReliability -v ./pkg/tunnel/

test-performance:
	@echo "Testing large data transfers..."
	go test -race -run TestLargeDataTransfer -v ./pkg/tunnel/

# Docker targets (for future use)
docker-build:
	@echo "Building Docker images..."
	docker build -t ssh-tunnel-server -f deploy/Dockerfile.server .
	docker build -t ssh-tunnel-client -f deploy/Dockerfile.client .

# Quick development cycle
quick-test: fmt vet test

# Full CI pipeline
ci: deps fmt vet lint test-cover

# Generate example configs and keys for testing
setup-test-env:
	@echo "Setting up test environment..."
	mkdir -p keys configs logs
	cp configs/server.example.yaml configs/server.yaml
	cp configs/client.example.yaml configs/client.yaml
	@echo "Test environment ready!"

# Generate SSH keys for development
generate-keys:
	@echo "Generating SSH keys for development..."
	mkdir -p keys
	ssh-keygen -t ed25519 -f keys/ssh_host_ed25519_key -N "" -C "tunnel-server-host-key"
	ssh-keygen -t ed25519 -f keys/client_key -N "" -C "tunnel-client-key"
	cp keys/client_key.pub keys/authorized_keys
	@echo "SSH keys generated in keys/ directory"

# Run server with example config (for development)
run-server:
	@echo "Starting tunnel server..."
	./bin/tunnel-server -config configs/server.yaml

# Run client with example config (for development)
run-client:
	@echo "Starting tunnel client..."
	./bin/tunnel-client -config configs/client.yaml

# Monitor logs during development
monitor-logs:
	@echo "Monitoring logs..."
	tail -f logs/*.log

# Performance profiling
profile-cpu:
	@echo "Running CPU profile..."
	go test -cpuprofile=cpu.prof -bench=. ./pkg/tunnel/
	go tool pprof cpu.prof

profile-mem:
	@echo "Running memory profile..."
	go test -memprofile=mem.prof -bench=. ./pkg/tunnel/
	go tool pprof mem.prof

# Show project statistics
stats:
	@echo "Project Statistics:"
	@echo "==================="
	@find . -name "*.go" -not -path "./vendor/*" | xargs wc -l | tail -1
	@echo "Go files:"
	@find . -name "*.go" -not -path "./vendor/*" | wc -l
	@echo "Test files:"
	@find . -name "*_test.go" -not -path "./vendor/*" | wc -l

# Validate project structure
validate:
	@echo "Validating project structure..."
	@test -f go.mod || (echo "go.mod not found" && exit 1)
	@test -f README.md || (echo "README.md not found" && exit 1)
	@test -d cmd || (echo "cmd directory not found" && exit 1)
	@test -d pkg || (echo "pkg directory not found" && exit 1)
	@test -d configs || (echo "configs directory not found" && exit 1)
	@echo "Project structure is valid!"

# Show available make targets
list:
	@LC_ALL=C $(MAKE) -pRrq -f $(lastword $(MAKEFILE_LIST)) : 2>/dev/null | awk -v RS= -F: '/^# File/,/^# Finished Make data base/ {if ($$1 !~ "^[#.]") {print $$1}}' | sort | grep -E -v -e '^[^[:alnum:]]' -e '^$@$$'
