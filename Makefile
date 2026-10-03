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
DOCKER:= $(shell which docker 2>/dev/null || echo /usr/local/bin/docker)

CLUSTER     := kcert-integration
K8S_VERSION ?= v1.37.0

# Extend PATH for sub-shells so docker (required by kind) and kubectl are found
# even when make is invoked from an IDE terminal without the full user PATH.
export PATH := /Applications/Docker.app/Contents/Resources/bin:/opt/homebrew/bin:$(PATH)

.PHONY: all build test lint vet gen-test-certs \
        integration-fixtures-up integration-fixtures-down \
        integration-test integration-test-existing \
        integration-cluster-up integration-cluster-down \
        monitoring-stack monitoring-stack-down \
        grafana-up perses-up perses-down \
        clean help

all: build

## Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(BINARY) ./cmd/kcert-checker

## Build and push arm64 image (Apple Silicon / kind local dev)
docker-build-arm64:
	$(DOCKER) buildx build \
  		--platform linux/arm64 \
  		-t sirajudheenam/kcert-checker:local \
		-f ./Dockerfile.local --push .

## Build and push amd64 image (Linux CI / real clusters)
docker-build-amd64:
	$(DOCKER) buildx build \
  		--platform linux/amd64 \
  		-t sirajudheenam/kcert-checker:latest \
		-t sirajudheenam/kcert-checker:1.0.0 \
		-f ./Dockerfile.cluster --push .


## Run unit tests with coverage
test:
	$(GO) test -count=1 -race -cover ./...

## Run vet
vet:
	$(GO) vet ./...

## Run all checks (vet + unit tests)
check: vet test

## Regenerate integration fixture certs (required on macOS — LibreSSL emits explicit EC params)
gen-test-certs:
	./scripts/gen-test-certs.sh

## Apply integration test fixtures to the current cluster (creates kcert-integration namespace)
integration-fixtures-up: gen-test-certs
	@echo "==> Applying integration fixtures to cluster: $$($(KUBECTL) config current-context)"
	$(KUBECTL) apply -f tests/integration/fixtures/
	@echo "==> Waiting for kcert-fixture pod to be Ready..."
	$(KUBECTL) wait -n kcert-integration pod/kcert-fixture \
		--for=condition=Ready \
		--timeout=120s
	@echo "==> Fixtures ready."

## Delete the kcert-integration namespace and all fixtures from the current cluster
integration-fixtures-down:
	@echo "==> Deleting kcert-integration namespace..."
	$(KUBECTL) delete namespace kcert-integration --ignore-not-found
	@echo "==> Done."

## Run integration tests against the current cluster, then clean up fixtures
integration-test-existing: integration-fixtures-up
	@echo "==> Running integration tests..."
	$(GO) test -v -count=1 -tags integration -timeout 5m ./tests/integration/... ; \
	EXIT=$$? ; \
	$(MAKE) integration-fixtures-down ; \
	exit $$EXIT

## Create a dedicated kcert-integration kind cluster, run tests, then destroy it (CI use)
integration-cluster-up: gen-test-certs
	@echo "Creating kind cluster with Kubernetes $(K8S_VERSION)..."
	$(KIND) create cluster \
		--name $(CLUSTER) \
		--image kindest/node:$(K8S_VERSION) \
		--wait 120s
	$(KUBECTL) --context kind-$(CLUSTER) apply -f tests/integration/fixtures/
	$(KUBECTL) --context kind-$(CLUSTER) wait -n kcert-integration pod/kcert-fixture \
		--for=condition=Ready \
		--timeout=120s
	@echo "==> Cluster ready."

## Destroy the dedicated kcert-integration kind cluster
integration-cluster-down:
	$(KIND) delete cluster --name $(CLUSTER)

## Run integration tests in a dedicated kind cluster (creates + destroys cluster)
integration-test: integration-cluster-up
	@echo "==> Running integration tests..."
	KUBECONFIG="$$($(KIND) get kubeconfig --name $(CLUSTER) 2>/dev/null)" \
	$(GO) test -v -count=1 -tags integration -timeout 5m ./tests/integration/... ; \
	EXIT=$$? ; \
	$(MAKE) integration-cluster-down ; \
	exit $$EXIT

## Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)

# ── monitoring stack (Prometheus + Grafana + Perses) ──────────────────────────
HELM_GRAFANA_RELEASE  := grafana
HELM_PERSES_RELEASE   := perses
MONITORING_NS         := monitoring

## Install/upgrade Prometheus + Grafana + Perses into the monitoring namespace
monitoring-stack: prometheus-up grafana-up perses-up
	@echo "==> Monitoring stack ready."
	@echo "    Prometheus:  make prometheus-port-forward   → http://localhost:9090"
	@echo "    Grafana:     make grafana-port-forward      → http://localhost:3000  (admin/admin)"
	@echo "    Perses:      make perses-port-forward       → http://localhost:8080"

## Uninstall Prometheus + Grafana + Perses from the monitoring namespace
monitoring-stack-down:
	helm uninstall prometheus        --namespace $(MONITORING_NS) --ignore-not-found || true
	helm uninstall $(HELM_GRAFANA_RELEASE) --namespace $(MONITORING_NS) --ignore-not-found || true
	helm uninstall $(HELM_PERSES_RELEASE)  --namespace $(MONITORING_NS) --ignore-not-found || true

## Install/upgrade Prometheus (with kcert scrape config + alert rules)
prometheus-up:
	@echo "==> Installing/upgrading Prometheus..."
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
	helm repo update prometheus-community
	helm upgrade --install prometheus prometheus-community/prometheus \
	  --namespace $(MONITORING_NS) \
	  --create-namespace \
	  -f alerts/prometheus-kcert-values.yaml \
	  --wait --timeout=120s

## Install/upgrade Grafana with kcert dashboard provisioned via ConfigMap sidecar
grafana-up:
	@echo "==> Installing/upgrading Grafana..."
	helm repo add grafana-community https://grafana-community.github.io/helm-charts 2>/dev/null || true
	helm repo update grafana-community
	$(KUBECTL) create namespace $(MONITORING_NS) --dry-run=client -o yaml | $(KUBECTL) apply -f -
	$(KUBECTL) apply -f deploy/grafana/dashboards/
	helm upgrade --install $(HELM_GRAFANA_RELEASE) grafana-community/grafana \
	  --namespace $(MONITORING_NS) \
	  -f deploy/grafana/grafana-values.yaml \
	  --wait --timeout=120s
	@echo "==> Grafana ready. Default credentials: admin / admin"
	@echo "    Port-forward: make grafana-port-forward"

## Install/upgrade Perses (CNCF open dashboarding, native PromQL support)
perses-up:
	@echo "==> Installing/upgrading Perses..."
	helm repo add perses https://perses.github.io/helm-charts 2>/dev/null || true
	helm repo update perses
	$(KUBECTL) create namespace $(MONITORING_NS) --dry-run=client -o yaml | $(KUBECTL) apply -f -
	$(KUBECTL) apply -f deploy/perses/perses-provisioning-cm.yaml
	helm upgrade --install $(HELM_PERSES_RELEASE) perses/perses \
	  --namespace $(MONITORING_NS) \
	  -f deploy/perses/perses-values.yaml \
	  --wait --timeout=120s
	@echo "==> Perses ready."
	@echo "    Port-forward: make perses-port-forward"

## Uninstall Perses
perses-down:
	helm uninstall $(HELM_PERSES_RELEASE) --namespace $(MONITORING_NS) --ignore-not-found || true

## Port-forward Prometheus UI → localhost:9090
prometheus-port-forward:
	$(KUBECTL) port-forward svc/prometheus-server 9090:80 -n $(MONITORING_NS)

## Port-forward Grafana UI → localhost:3000
grafana-port-forward:
	$(KUBECTL) port-forward svc/$(HELM_GRAFANA_RELEASE) 3000:80 -n $(MONITORING_NS)

## Port-forward Perses UI → localhost:8080
perses-port-forward:
	$(KUBECTL) port-forward svc/$(HELM_PERSES_RELEASE) 8080:8080 -n $(MONITORING_NS)

# ── Helm chart packaging and publishing ───────────────────────────────────────
HELM_CHART_DIR := helm/kcert-checker
HELM_PAGES_URL := https://sirajudheenam.github.io/kcert-checker

## Lint the Helm chart
helm-lint:
	helm lint $(HELM_CHART_DIR)

## Render chart templates to stdout (dry-run)
helm-template:
	helm template kcert-checker $(HELM_CHART_DIR) --namespace monitoring

## Package chart into .helm-packages/ (bump Chart.yaml version first)
helm-package: helm-lint
	@mkdir -p .helm-packages
	helm package $(HELM_CHART_DIR) --destination .helm-packages
	@echo "==> Packaged: $$(ls .helm-packages/kcert-checker-*.tgz | tail -1)"

## Dry-run publish: lint + package, no push (safe to run anytime)
helm-dry-run: helm-package
	@echo "==> Dry run complete. Package in .helm-packages/ — nothing pushed."

## Publish chart to GitHub Pages manually (CI publishes automatically on helm/v* tag)
helm-publish:
	./scripts/helm-publish.sh

## Tag and push a chart release — triggers GitHub Actions: make helm-release VERSION=0.2.0
helm-release:
	@if [ -z "$(VERSION)" ]; then echo "ERROR: VERSION= is required (e.g. make helm-release VERSION=0.2.0)"; exit 1; fi
	@sed -i '' "s/^version: .*/version: $(VERSION)/" $(HELM_CHART_DIR)/Chart.yaml
	@echo "==> Chart.yaml version bumped to $(VERSION)"
	git add $(HELM_CHART_DIR)/Chart.yaml
	git commit -m "chore: bump helm chart version to $(VERSION)"
	git tag helm/v$(VERSION)
	git push origin main helm/v$(VERSION)
	@echo "==> Tag helm/v$(VERSION) pushed — GitHub Actions will publish the chart."
	@echo "    Watch: https://github.com/sirajudheenam/kcert-checker/actions"

## Show this help
help:
	@printf "\nUsage: make <target>\n"
	@printf "\nBuild & test\n"
	@awk '/^## /{desc=substr($$0,4);next} /^[a-zA-Z0-9_-]+:/{split($$1,a,":");if(desc){printf "  %-34s %s\n",a[1],desc};desc=""}' Makefile \
	  | grep -E '  (build|test|vet|check|gen-test-certs|clean) ' || true
	@printf "\nDocker images\n"
	@awk '/^## /{desc=substr($$0,4);next} /^[a-zA-Z0-9_-]+:/{split($$1,a,":");if(desc){printf "  %-34s %s\n",a[1],desc};desc=""}' Makefile \
	  | grep -E '  docker-' || true
	@printf "\nIntegration tests\n"
	@awk '/^## /{desc=substr($$0,4);next} /^[a-zA-Z0-9_-]+:/{split($$1,a,":");if(desc){printf "  %-34s %s\n",a[1],desc};desc=""}' Makefile \
	  | grep -E '  integration-' || true
	@printf "\nMonitoring stack (Prometheus + Grafana + Perses)\n"
	@awk '/^## /{desc=substr($$0,4);next} /^[a-zA-Z0-9_-]+:/{split($$1,a,":");if(desc){printf "  %-34s %s\n",a[1],desc};desc=""}' Makefile \
	  | grep -E '  (monitoring|prometheus|grafana|perses)-' || true
	@printf "\nHelm chart\n"
	@awk '/^## /{desc=substr($$0,4);next} /^[a-zA-Z0-9_-]+:/{split($$1,a,":");if(desc){printf "  %-34s %s\n",a[1],desc};desc=""}' Makefile \
	  | grep -E '  helm-' || true
	@printf "\n"
