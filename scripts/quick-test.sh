#!/bin/bash

# Test runner script to quickly check if our code compiles and basic tests pass

echo "🔧 SSH Tunnel System - Quick Test Runner"
echo "======================================"

# Check if we can build
echo "1. Building project..."
if ! go build ./...; then
    echo "❌ Build failed!"
    exit 1
fi
echo "✅ Build successful"

# Check basic syntax with go vet
echo "2. Running go vet..."
if ! go vet ./...; then
    echo "❌ Go vet failed!"
    exit 1
fi
echo "✅ Go vet passed"

# Run basic tests (just compilation, no actual test execution for now)
echo "3. Testing compilation of test files..."
if ! go test -c ./pkg/tunnel/ -o /tmp/tunnel-test; then
    echo "❌ Test compilation failed!"
    exit 1
fi
echo "✅ Test compilation successful"

# Run a simple config test
echo "4. Testing basic functionality..."
go test -run TestServerCreation -v ./pkg/tunnel/ -timeout 30s

echo ""
echo "🎉 Basic tests completed!"
echo "To run full test suite: make test"
echo "To run with coverage: make test-cover"
