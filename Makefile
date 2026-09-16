# kcert-checker Makefile

BINARY      := kcert-checker
BUILD_DIR   := bin

# Resolve Go and Kubernetes tooling at parse time.
# $(shell which ...) checks the system PATH first; the fallback covers macOS
# installations via Homebrew or direct .pkg download that don't always appear
# in make's environment PATH.
GO     := $(shell which go 2>/dev/null || echo /opt/homebrew/Cellar/go/1.27.1/libexec/bin/go)
KIND   := $(shell which kind 2>/dev/null || echo /opt/homebrew/bin/kind)
KUBECTL:= $(shell which kubectl 2>/dev/null || echo /opt/homebrew/bin/kubectl)

CLUSTER     := kcert-integration
K8S_VERSION ?= v1.37.0

# Extend PATH for sub-shells so docker (required by kind) and kubectl are found
# even when make is invoked from an IDE terminal without the full user PATH.
export PATH := /Applications/Docker.app/Contents/Resources/bin:/opt/homebrew/bin:$(PATH)

.PHONY: all build test lint vet integration-test integration-cluster-up integration-cluster-down clean

all: build

## Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(BINARY) ./cmd/kcert-checker

## Run unit tests with coverage
test:
	$(GO) test -count=1 -race -cover ./...

## Run vet
vet:
	$(GO) vet ./...

## Run all checks (vet + unit tests)
check: vet test

## Create a local kind cluster with integration fixtures
integration-cluster-up:
	@echo "Creating kind cluster with Kubernetes $(K8S_VERSION)..."
	$(KIND) create cluster \
		--name $(CLUSTER) \
		--image kindest/node:$(K8S_VERSION) \
		--wait 120s
	@echo "Applying integration fixtures..."
	$(KUBECTL) apply -f tests/integration/fixtures/
	@echo "Waiting for fixture pod..."
	$(KUBECTL) wait -n kcert-integration pod/kcert-fixture \
		--for=condition=Ready \
		--timeout=120s
	@echo "Cluster ready."

## Destroy the integration kind cluster
integration-cluster-down:
	$(KIND) delete cluster --name $(CLUSTER)

## Run integration tests (creates cluster, runs tests, destroys cluster)
# The cluster is torn down even if tests fail (EXIT captures the test exit code).
integration-test: integration-cluster-up
	@echo "Running integration tests..."
	$(GO) test -v -count=1 -tags integration -timeout 5m ./tests/integration/... ; \
	EXIT=$$? ; \
	$(MAKE) integration-cluster-down ; \
	exit $$EXIT

## Run integration tests against an existing cluster (skips cluster lifecycle)
integration-test-existing:
	$(GO) test -v -count=1 -tags integration -timeout 5m ./tests/integration/...

## Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)

## Show help
help:
	@grep -E '^##' Makefile | sed 's/^## //'
