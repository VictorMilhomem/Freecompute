# Project variables
BINARY_NAME ?= go-freecompute
BUILD_DIR ?= bin
MAIN_SRC ?= ./cmd/main.go
GO ?= go
export CGO_ENABLED ?= 0
export GOFLAGS ?= -tags=containers_image_openpgp

.PHONY: all build run test fmt vet tidy clean help

all: build ## Build the Go binary (default)

build: ## Build the Go binary to bin/ directory
	@echo "Building binary..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_SRC)

run: ## Run the Go application directly
	$(GO) run $(MAIN_SRC)

test: ## Run unit tests
	$(GO) test -v ./...

fmt: ## Format source code using go fmt
	$(GO) fmt ./...

vet: ## Run static code analysis using go vet
	$(GO) vet ./...

tidy: ## Tidy module dependencies
	$(GO) mod tidy

clean: ## Remove build artifacts and binary directory
	@echo "Cleaning up..."
	rm -rf $(BUILD_DIR)

help: ## Display available Makefile targets
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
