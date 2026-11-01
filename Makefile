BINARY_NAME ?= vcnode
FAULTY_BINARY_NAME ?= vcnode-faulty
EXAMPLE_BINARY_NAME ?= example-simple
BUILD_DIR ?= bin
MAIN_SRC ?= ./cmd/vcnode
FAULTY_SRC ?= ./cmd/vcnode-faulty
EXAMPLE_SRC ?= ./examples/simple
GO ?= go
PODMAN_TAGS ?= containers_image_openpgp,exclude_graphdriver_btrfs
export GOFLAGS ?= -tags=$(PODMAN_TAGS)
export VC_IMAGE ?= docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e
export CCACHE_DIR ?= /tmp/ccache

.PHONY: all build build-faulty build-example example test race demo demo-divergent fmt vet tidy clean

all: build build-example

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_SRC)

build-faulty:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -tags faulty,$(PODMAN_TAGS) -o $(BUILD_DIR)/$(FAULTY_BINARY_NAME) $(FAULTY_SRC)

build-example:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(EXAMPLE_BINARY_NAME) $(EXAMPLE_SRC)

example: build-example

test:
	CGO_ENABLED=0 $(GO) test ./...

race:
	CGO_ENABLED=1 $(GO) test -race ./...

demo: build build-example
	./scripts/demo.sh

demo-divergent: build build-faulty build-example
	./scripts/demo.sh --divergent

fmt:
	$(GO) fmt ./...

vet:
	CGO_ENABLED=0 $(GO) vet ./...

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BUILD_DIR)
