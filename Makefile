.PHONY: all proto lint-go build test clean install-tools

# Default target
all: proto build

# Install required tools using Go 1.24+ tool directives
install-tools:
	@echo "Installing tools using Go 1.24+ tool directives..."
	@go mod download
	@go build -o $$(go env GOPATH)/bin/ \
		github.com/bufbuild/buf/cmd/buf \
		github.com/golangci/golangci-lint/cmd/golangci-lint \
		google.golang.org/grpc/cmd/protoc-gen-go-grpc \
		google.golang.org/protobuf/cmd/protoc-gen-go
	@echo "Verifying tool installation..."
	@test -f "$$(go env GOPATH)/bin/protoc-gen-go" || (echo "Error: protoc-gen-go not installed" && exit 1)
	@test -f "$$(go env GOPATH)/bin/protoc-gen-go-grpc" || (echo "Error: protoc-gen-go-grpc not installed" && exit 1)
	@test -f "$$(go env GOPATH)/bin/buf" || (echo "Error: buf not installed" && exit 1)
	@test -f "$$(go env GOPATH)/bin/golangci-lint" || (echo "Error: golangci-lint not installed" && exit 1)
	@echo "Tools installed successfully with pinned versions from go.mod tool directives"

# Define tool commands with fallback paths
GOPATH := $(shell go env GOPATH)
BUF := $(shell which buf 2>/dev/null || echo "$(GOPATH)/bin/buf")
GOLANGCILINT := $(shell which golangci-lint 2>/dev/null || echo "$(GOPATH)/bin/golangci-lint")

# Update proto definitions and generate code (checks if update needed)
update-proto:
	@./ci/update-proto.sh

# Generate Go code from existing proto files
proto:
	@echo "Generating Go code from proto files..."
	@$(BUF) generate
	@echo "Proto generation complete"

# Lint Go code
lint-go:
	@echo "Linting Go code..."
	@$(GOLANGCILINT) run
	@echo "Go linting complete"


# Build the project
build: proto
	@echo "Building the project..."
	@go build ./...
	@echo "Build complete"

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...
	@echo "Tests complete"

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -cover ./...
	@echo "Coverage report complete"

# Clean generated files
clean:
	@echo "Cleaning generated files..."
	@rm -rf grpc/a2a/
	@echo "Clean complete"

# Update dependencies
deps:
	@echo "Updating dependencies..."
	@go mod download
	@go mod tidy
	@echo "Dependencies updated"

# Development setup (install tools and generate code)
setup: install-tools proto
	@echo "Development setup complete"

# CI setup (fetch latest proto and generate code)
ci-setup: update-proto
	@echo "CI setup complete"

# Check if proto definitions are up to date
check-proto-updated:
	@echo "Checking if proto definitions are up to date..."
	@./ci/update-proto.sh --check || (echo "Proto definitions are outdated. Run 'make update-proto' to update." && exit 1)

# Show metadata about current proto definitions
proto-info:
	@echo "Current A2A Proto Definition Information:"
	@echo "========================================"
	@if [ -f "buf.gen.yaml" ]; then \
		echo "Source: buf.gen.yaml remote git input"; \
		echo "Repository: $$(grep 'git_repo:' buf.gen.yaml | sed 's/.*git_repo: *//')"; \
		echo "Version/Ref: $$(grep 'ref:' buf.gen.yaml | sed 's/.*ref: *//')"; \
		echo "Subdir: $$(grep 'subdir:' buf.gen.yaml | sed 's/.*subdir: *//')"; \
	    echo "Output: $$(grep 'out:' buf.gen.yaml | head -n1 | sed 's/.*out: *//')"
	else \
		echo "No buf.gen.yaml found. Run 'make setup' to initialize."; \
	fi

# Help target
help:
	@echo "Available targets:"
	@echo "  make all            - Generate proto and build (default)"
	@echo "  make install-tools  - Install required development tools"
	@echo "  make update-proto   - Update proto definitions from official repo"
	@echo "  make proto          - Generate Go code from proto files"
	@echo "  make proto-info     - Show metadata about current proto definitions"
	@echo "  make lint-go        - Lint Go code with golangci-lint"
	@echo "  make build          - Build the project"
	@echo "  make test           - Run tests"
	@echo "  make test-coverage  - Run tests with coverage"
	@echo "  make clean          - Clean generated files"
	@echo "  make breaking       - Check for breaking proto changes"
	@echo "  make deps           - Update Go dependencies"
	@echo "  make setup          - Initial development setup"
	@echo "  make ci-setup       - CI setup (fetch latest proto)"
	@echo "  make help           - Show this help message"
