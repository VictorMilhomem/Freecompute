BINARY_NAME ?= vcnode
BUILD_DIR ?= bin
MAIN_SRC ?= ./cmd/vcnode
GO ?= go
export GOFLAGS ?= -tags=containers_image_openpgp,exclude_graphdriver_btrfs
export VC_IMAGE ?= docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e

.PHONY: all build test race demo demo-divergent fmt vet tidy clean

all: build

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_SRC)

test:
	CGO_ENABLED=0 $(GO) test ./...

race:
	CGO_ENABLED=1 $(GO) test -race ./...

demo: build
	./scripts/demo.sh

demo-divergent: build
	./scripts/demo.sh --divergent

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BUILD_DIR)
