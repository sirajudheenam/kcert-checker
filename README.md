# kcert-checker

A Kubernetes-native certificate expiry checker. Scans every running pod container and every Secret cluster-wide, parses PEM certificates, and exposes expiry timestamps as Prometheus metrics. Alert rules fire at 7, 30, 60, and 90 days before expiry.

---

## Table of contents

1. [How it works](#how-it-works)
2. [Repository layout](#repository-layout)
3. [Prerequisites](#prerequisites)
4. [Quick start — local kind cluster](#quick-start--local-kind-cluster)
5. [Building the image](#building-the-image)
6. [Deploying kcert-checker](#deploying-kcert-checker)
   - [Helm (recommended)](#helm-recommended)
   - [Raw kubectl manifests](#raw-kubectl-manifests)
7. [Installing from the published Helm chart](#installing-from-the-published-helm-chart)
8. [Wiring Prometheus](#wiring-prometheus)
9. [Monitoring stack — Grafana + Perses](#monitoring-stack--grafana--perses)
10. [Verifying the deployment](#verifying-the-deployment)
11. [Testing](#testing)
12. [Helm chart development](#helm-chart-development)
13. [Configuration reference](#configuration-reference)
14. [Makefile reference](#makefile-reference)
15. [Clean up](#clean-up)
16. [Troubleshooting](#troubleshooting)
17. [Two-repo architecture and syncing](#two-repo-architecture-and-syncing)

---

## How it works

```
kcert-checker pod
  ├── On startup: scan all namespaces
  ├── Every N hours (default 1): scan again
  │
  ├── For each Pod container:
  │     exec cat <path> inside the container (via pods/exec SPDY)
  │     parse PEM → extract NotAfter
  │
  ├── For each Secret:
  │     read .crt / .pem / tls.crt / ca.crt keys
  │     parse PEM → extract NotAfter
  │
  └── Expose Prometheus metrics on :8080/metrics
        kcert_certificate_expiry_timestamp_seconds{namespace,source_type,source_name,
            subject_common_name,issuer_common_name,path,container}
        kcert_certificates_total
        kcert_certificates_expired_total
        kcert_scan_timestamp_seconds
        kcert_scan_duration_seconds
        kcert_scan_errors_total
```

Prometheus scrapes the metrics endpoint and evaluates alert rules:

| Alert | Condition | Severity |
|---|---|---|
| `KCertCertificateExpired` | `NotAfter ≤ now` | critical |
| `KCertCertificateExpiresWithin7Days` | `0 < days ≤ 7` | critical |
| `KCertCertificateExpiresWithin30Days` | `7 < days ≤ 30` | warning |
| `KCertCertificateExpiresWithin60Days` | `30 < days ≤ 60` | warning |
| `KCertCertificateExpiresWithin90Days` | `60 < days ≤ 90` | warning |

---

## Repository layout

```
kcert-checker/
├── cmd/kcert-checker/main.go           # entry point
├── app/app.go                          # Run(Options) — public surface for downstream import
├── internal/
│   ├── config/                         # config structs + YAML loading
│   ├── kubernetes/                     # k8s client, pod/namespace listing
│   ├── metrics/                        # Prometheus gauge/counter definitions
│   └── scanner/                        # certificate scanning logic
│       ├── scanner.go                  # scan loop (secrets + pods)
│       ├── container.go                # exec cat via SPDY
│       └── certificate.go             # PEM → x509 parsing
├── tests/integration/                  # integration test suite (build tag: integration)
│   └── fixtures/                       # k8s manifests: secrets + pod covering all expiry tiers
├── scripts/
│   ├── gen-test-certs.sh               # regenerate fixture certs (macOS-safe P-256)
│   └── helm-publish.sh                 # manual gh-pages chart publish
├── deploy/                             # raw kubectl manifests (reference; Helm preferred)
├── helm/kcert-checker/                 # Helm chart
│   ├── Chart.yaml
│   ├── values.yaml
│   └── templates/
├── alerts/
│   ├── certificate-alerts.yaml         # standalone PrometheusRule (no Helm)
│   └── prometheus-kcert-values.yaml    # Helm values: scrape job + alert rules
├── deploy/grafana/                     # Grafana helm values + dashboard ConfigMap
├── deploy/perses/                      # Perses helm values + provisioning ConfigMap
├── test-certs-deploy/                  # demo manifests covering all 5 alert tiers
├── config.yaml                         # local run config (kubeconfig mode)
├── Dockerfile                          # production image (linux/amd64)
├── Dockerfile.local                    # local dev image (linux/arm64)
└── Makefile                            # all common tasks (run: make help)
```

---

## Prerequisites

### Tools (macOS)

```bash
brew install go docker kind kubectl helm
```

Verify:

```bash
go version          # 1.21+
kind --version      # 0.33+
kubectl version --client
helm version        # 3.x
```

### Docker must be running

```bash
docker info         # must return without error
```

---

## Quick start — local kind cluster

This gets kcert-checker running with Prometheus alerts in ~10 minutes.

### 1. Create a kind cluster

```bash
kind create cluster --name kind
kubectl cluster-info --context kind-kind
kubectl get nodes
```

Expected:
```
NAME                 STATUS   ROLES           AGE   VERSION
kind-control-plane   Ready    control-plane   30s   v1.37.x
```

### 2. Build and load the image

```bash
# Build for your Mac's architecture
docker build -t kcert-checker:local .

# Load into kind (bypasses registry entirely)
kind load docker-image kcert-checker:local
```

Verify it loaded:
```bash
docker exec kind-control-plane crictl images | grep kcert
```

### 3. Deploy kcert-checker + Prometheus

```bash
# Install Prometheus with kcert scrape config and alert rules
make prometheus-up

# Install kcert-checker (local image, kind cluster)
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set image.repository=kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never

# Wait for both to be ready
kubectl rollout status deployment/prometheus-server -n monitoring
kubectl rollout status deployment/kcert-checker -n monitoring
```

### 4. Verify metrics are flowing

```bash
# Port-forward kcert-checker metrics
kubectl port-forward svc/kcert-checker 8080:8080 -n monitoring &
curl -s http://localhost:8080/metrics | grep kcert_certificate
```

### 5. Open Prometheus UI

```bash
make prometheus-port-forward
# Open http://localhost:9090
# Status → Targets: look for job=kcert-checker with state UP
# Alerts: expand kcert-certificate-expiry group
```

---

## Building the image

### Apple Silicon (arm64) — kind / local dev

```bash
# Option A: simple local build (no push)
docker build -t kcert-checker:local .
kind load docker-image kcert-checker:local

# Option B: build and push to DockerHub (arm64)
make docker-build-arm64
```

### Linux clusters (amd64)

```bash
# Build and push to DockerHub (amd64)
make docker-build-amd64

# Or cross-compile from Apple Silicon
docker buildx build \
  --platform linux/amd64 \
  -t sirajudheenam/kcert-checker:latest \
  --push .
```

---

## Deploying kcert-checker

### Helm (recommended)

```bash
# Install (kind — local image)
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set image.repository=kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never

# Install (real cluster — DockerHub image)
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace

# Upgrade after config changes
helm upgrade kcert-checker ./helm/kcert-checker -n monitoring

# Dry-run / template preview
helm lint ./helm/kcert-checker
helm template kcert-checker ./helm/kcert-checker --namespace monitoring

# Uninstall
helm uninstall kcert-checker -n monitoring
```

### Raw kubectl manifests

```bash
kubectl create namespace monitoring
kubectl apply -f deploy/serviceaccount.yaml
kubectl apply -f deploy/clusterrole.yaml
kubectl apply -f deploy/clusterrolebinding.yaml
kubectl apply -f deploy/configmap.yaml
kubectl apply -f deploy/deployment.yaml
kubectl apply -f deploy/service.yaml

# For kind: switch to local image
kubectl set image deployment/kcert-checker \
  kcert-checker=kcert-checker:local -n monitoring
kubectl patch deployment kcert-checker -n monitoring \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"kcert-checker","imagePullPolicy":"Never"}]}}}}'
```

> **Note:** `deploy/servicemonitor.yaml` requires the Prometheus Operator CRD
> (`monitoring.coreos.com/v1`). Skip it unless using `kube-prometheus-stack`.

### Verify the deployment

```bash
kubectl get pods -n monitoring | grep kcert
kubectl logs -n monitoring deployment/kcert-checker

# Health and metrics endpoints
kubectl port-forward svc/kcert-checker 8080:8080 -n monitoring
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
curl -s http://localhost:8080/metrics | grep kcert_scan
```

Expected log output:
```
Starting kcert-checker
Scanning namespace: monitoring
Certificate found: pod=... container=... path=...
Scan complete
```

### Trigger an immediate scan

kcert-checker scans on startup, then every `scan.interval_seconds` (default 3600). Force one:

```bash
kubectl rollout restart deployment/kcert-checker -n monitoring
kubectl rollout status deployment/kcert-checker -n monitoring
```

---

## Installing from the published Helm chart

The chart is published to GitHub Pages automatically when a `helm/v*` tag is pushed.

```bash
helm repo add kcert-checker https://sirajudheenam.github.io/kcert-checker
helm repo update

# Install latest
helm install kcert-checker kcert-checker/kcert-checker \
  --namespace monitoring \
  --create-namespace

# Show available versions
helm search repo kcert-checker --versions
```

---

## Wiring Prometheus

`alerts/prometheus-kcert-values.yaml` contains a scrape job and all five alert rules.

```bash
# Install Prometheus with kcert config baked in
make prometheus-up

# Or upgrade an existing Prometheus installation
helm upgrade prometheus prometheus-community/prometheus \
  --namespace monitoring \
  -f alerts/prometheus-kcert-values.yaml

kubectl rollout status deployment/prometheus-server -n monitoring
```

### Verify scraping

```bash
kubectl port-forward -n monitoring svc/prometheus-server 9092:80 &
sleep 2

# Check kcert-checker appears as a target
curl -s http://localhost:9092/api/v1/targets | python3 -c "
import sys, json
d = json.load(sys.stdin)
for t in d['data']['activeTargets']:
    job = t.get('labels', {}).get('job', '')
    if 'kcert' in job:
        print(job, '->', t.get('health'), '|', t.get('scrapeUrl'))
        if t.get('lastError'): print('  ERROR:', t['lastError'])
"
```

Expected: `kcert-checker -> up | http://10.244.0.x:8080/metrics`

### Useful PromQL expressions

```promql
# Days until expiry for all certs (positive = not yet expired)
(kcert_certificate_expiry_timestamp_seconds - time()) / 86400

# Certs expiring within 7 days
(kcert_certificate_expiry_timestamp_seconds - time()) / 86400 < 7

# Hours since last scan
(time() - kcert_scan_timestamp_seconds) / 3600

# Total scan errors
kcert_scan_errors_total
```

### Check alerts

```bash
# Currently firing or pending
curl -s "http://localhost:9092/api/v1/alerts" | python3 -c "
import sys, json
d = json.load(sys.stdin)
alerts = d['data']['alerts']
if not alerts: print('No alerts yet (may be in 10m pending window)')
for a in alerts:
    print(f'[{a[\"state\"]}] {a[\"labels\"].get(\"alertname\")} severity={a[\"labels\"].get(\"severity\")}')
    print(f'  ns={a[\"labels\"].get(\"namespace\")} source={a[\"labels\"].get(\"source_name\")}')
"

# All kcert rules and their state
curl -s "http://localhost:9092/api/v1/rules" | python3 -c "
import sys, json
d = json.load(sys.stdin)
for g in d['data']['groups']:
    if 'kcert' in g['name'].lower():
        print(f'Group: {g[\"name\"]}')
        for r in g['rules']:
            print(f'  [{r.get(\"state\", \"n/a\")}] {r[\"name\"]}')
"
```

> Alert rules have `for: 10m` — wait up to 10 minutes for `pending → firing`.

---

## Monitoring stack — Grafana + Perses

Install all three (Prometheus + Grafana + Perses) in one shot:

```bash
make monitoring-stack
```

Or individually:

```bash
make prometheus-up     # Prometheus with kcert scrape + alert rules
make grafana-up        # Grafana with kcert dashboard (auto-provisioned via ConfigMap sidecar)
make perses-up         # Perses with Prometheus datasource + kcert dashboard
```

### Port-forwards

```bash
make prometheus-port-forward   # http://localhost:9090
make grafana-port-forward      # http://localhost:3000  (admin / admin)
make perses-port-forward       # http://localhost:8080
```

### Tear down

```bash
make monitoring-stack-down
```

---

## Verifying the deployment

### Demo secrets (all expiry tiers)

```bash
# Apply demo secrets covering OK / WARNING / CRITICAL / EXPIRED
kubectl apply -f test-certs-deploy/

# Force an immediate scan
kubectl rollout restart deployment/kcert-checker -n monitoring
kubectl rollout status deployment/kcert-checker -n monitoring

# Check the scan found them
kubectl logs -n monitoring deployment/kcert-checker | grep "Certificate found"
```

### Check certificate expiry metrics

```bash
kubectl port-forward -n monitoring svc/prometheus-server 9092:80 &
sleep 2

curl -s "http://localhost:9092/api/v1/query?query=kcert_certificate_expiry_timestamp_seconds" | python3 -c "
import sys, json, time
d = json.load(sys.stdin)
results = d['data']['result']
if not results: print('(no data — trigger a scan: kubectl rollout restart deployment/kcert-checker -n monitoring)')
for r in sorted(results, key=lambda x: float(x['value'][1])):
    l = r['metric']
    days = (float(r['value'][1]) - time.time()) / 86400
    print(f'{days:+7.1f}d  {l.get(\"namespace\")}/{l.get(\"source_name\")}  CN={l.get(\"subject_common_name\")}')
"
```

---

## Testing

### Unit tests

```bash
make test                           # unit tests with race detector + coverage
make vet                            # go vet
make check                          # vet + test
```

With coverage report:
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration tests

Integration tests run against a live Kubernetes cluster and verify the scanner correctly classifies certificates at all expiry tiers (expired, critical, warning, healthy).

**Against your existing cluster (fastest):**
```bash
make integration-test-existing
# Applies fixtures → runs tests → deletes kcert-integration namespace
```

**In a fresh dedicated kind cluster (CI-style):**
```bash
make integration-test
# Creates kind cluster → applies fixtures → runs tests → destroys cluster
```

**Manual fixture management:**
```bash
make integration-fixtures-up    # apply fixtures only (leaves them running)
make integration-fixtures-down  # delete kcert-integration namespace
```

### Regenerate fixture certs (macOS only)

macOS ships LibreSSL which generates EC certs with explicit curve parameters that Go's `crypto/x509` rejects. If tests report fewer certs than expected:

```bash
make gen-test-certs
# Regenerates tests/integration/fixtures/01-secrets.yaml with P-256 named-curve certs
```

Run this after a fresh clone on macOS, or whenever fixture certs drift past their expiry windows.

---

## Helm chart development

```bash
# Lint
make helm-lint

# Preview rendered templates
make helm-template

# Package (bump Chart.yaml version first)
make helm-package

# Publish manually to GitHub Pages
make helm-publish

# Tag and release (triggers GitHub Actions CI)
make helm-release VERSION=0.2.0
```

The CI workflow (`.github/workflows/helm-release.yml`) publishes automatically when a `helm/v*` tag is pushed. `make helm-release VERSION=x.y.z` does the version bump, commit, and tag push in one step.

---

## Configuration reference

`config.yaml` (mounted as a ConfigMap in-cluster):

```yaml
kubernetes:
  mode: "kubeconfig"           # "kubeconfig" (local) or "incluster"
  # kubeconfig: "~/.kube/config"  # optional — defaults to ~/.kube/config
  # context: "kind-kind"          # optional — defaults to current-context

scanner:
  namespaces:
    exclude:
      - kube-system
      - kube-public
      - kube-node-lease
      - "openshift-*"           # glob patterns supported

  certificates:
    paths:                      # paths checked inside each container
      - "/etc/ssl/postfix/*"
      - "/etc/tls/*"
      - "/etc/certs/*"
      - "/tls/*"
      - "/var/run/secrets/tls/*"

  containers:
    include_init_containers: false
    exclude:
      - istio-proxy
      - linkerd-proxy
      - "init-*"

  pods:
    exclude:
      - "debug-*"
      - "test-*"

  secrets:
    exclude:
      - "sh.helm.release.*"

metrics:
  enabled: true
  listen_address: "0.0.0.0:8080"
  path: "/metrics"

scan:
  interval_seconds: 3600        # 3600 = 1 hour; 86400 = 24 hours
  warning_days: 30
  critical_days: 7
```

Override a single value at deploy time:
```bash
helm upgrade kcert-checker ./helm/kcert-checker -n monitoring \
  --set config.scan.interval_seconds=600
```

View the running config:
```bash
kubectl get configmap kcert-checker-config -n monitoring \
  -o jsonpath='{.data.config\.yaml}'
```

---

## Makefile reference

```
make help
```

All targets, grouped by category. Summary:

| Group | Key targets |
|---|---|
| Build & test | `build`, `test`, `check`, `gen-test-certs` |
| Docker | `docker-build-arm64`, `docker-build-amd64` |
| Integration | `integration-test`, `integration-test-existing`, `integration-fixtures-up/down` |
| Monitoring | `monitoring-stack`, `prometheus-up`, `grafana-up`, `perses-up`, `*-port-forward` |
| Helm | `helm-lint`, `helm-template`, `helm-package`, `helm-release VERSION=x.y.z` |

---

## Clean up

### Uninstall everything from the monitoring namespace

```bash
# 1. Remove demo / test resources
kubectl delete pod kcert-test -n monitoring --ignore-not-found
kubectl delete -f test-certs-deploy/ --ignore-not-found

# 2. Uninstall kcert-checker
#    If installed via Helm:
helm uninstall kcert-checker -n monitoring
#    If installed via raw kubectl manifests (no Helm release found):
kubectl delete deployment,service,configmap/kcert-checker-config \
  -n monitoring --ignore-not-found
kubectl delete serviceaccount kcert-checker -n monitoring --ignore-not-found
kubectl delete clusterrole,clusterrolebinding kcert-checker --ignore-not-found

# 3. Uninstall monitoring stack (Grafana + Perses + Prometheus)
make monitoring-stack-down
# Or individually:
helm uninstall grafana    -n monitoring --ignore-not-found
helm uninstall perses     -n monitoring --ignore-not-found
helm uninstall prometheus -n monitoring --ignore-not-found

# 4. Remove leftover ConfigMaps / Secrets not owned by Helm
kubectl delete configmap kcert-checker-dashboard perses-provisioning \
  cert-script -n monitoring --ignore-not-found
kubectl delete secret test-certificate -n monitoring --ignore-not-found
```

Verify everything is gone:
```bash
helm list -n monitoring                          # should be empty
kubectl get all,configmap,secret -n monitoring   # only kube-root-ca.crt and default SA remain
```

### Delete the kind cluster entirely

```bash
kind delete cluster --name kind
```

---

## Troubleshooting

### `ImagePullBackOff`

The DockerHub image has no arm64 build. Use a locally-built image:

```bash
docker build -t kcert-checker:local .
kind load docker-image kcert-checker:local
kubectl set image deployment/kcert-checker kcert-checker=kcert-checker:local -n monitoring
kubectl patch deployment kcert-checker -n monitoring \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"kcert-checker","imagePullPolicy":"Never"}]}}}}'
```

### `CreateContainerConfigError`

Kubernetes rejects a string username (`nonroot`) with `runAsNonRoot: true`. The Helm chart already sets `runAsUser: 65532`. For raw manifests, patch:

```bash
kubectl patch deployment kcert-checker -n monitoring \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"kcert-checker","securityContext":{"runAsNonRoot":true,"runAsUser":65532,"allowPrivilegeEscalation":false,"readOnlyRootFilesystem":true}}]}}}}'
```

### Config file not found: `/etc/kcert-checker/config.yaml`

The original `deploy/deployment.yaml` mounted the ConfigMap at `/etc/kcert` instead of `/etc/kcert-checker`. The Helm chart is already correct. For raw manifests:

```bash
kubectl patch deployment kcert-checker -n monitoring --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/volumeMounts/0","value":{"name":"config","mountPath":"/etc/kcert-checker","readOnly":true}}]'
```

### `kcert-checker` not appearing as a Prometheus scrape target

The `endpointslice` SD role does not expose `__meta_kubernetes_service_name`. The correct relabel source label is:

```
__meta_kubernetes_endpointslice_label_kubernetes_io_service_name
```

This is already correct in `alerts/prometheus-kcert-values.yaml`. If targets are missing, verify the config was applied:

```bash
kubectl port-forward -n monitoring svc/prometheus-server 9092:80 &
curl -s http://localhost:9092/api/v1/status/config | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['data']['yaml'])" | grep -A30 kcert
```

Force a config reload without restarting Prometheus:

```bash
curl -s -XPOST http://localhost:9092/-/reload
```

### Integration tests find fewer certs than expected

macOS LibreSSL generates EC certs with explicit curve parameters that Go's `crypto/x509` silently rejects. Regenerate the fixture certs:

```bash
make gen-test-certs
```

---

## Two-repo architecture and syncing

This is the **public canonical source**. A separate internal downstream repo imports it as a Go module and adds organisation-specific configuration (Dockerfile, Helm values, deploy pipelines).

### Relationship

```
github.com/sirajudheenam/kcert-checker   ← this repo (canonical Go source)
        │
        │  go replace directive (local path, dev only)
        │  rsync for non-Go assets (alerts/, deploy/, ui/, helm/templates/)
        ▼
internal/cronus/kcert-checker            ← downstream (SAP-specific config)
```

The downstream `go.mod` contains:

```
require github.com/sirajudheenam/kcert-checker v0.0.0
replace github.com/sirajudheenam/kcert-checker => ../../../github/sirajudheenam/kcert-checker
```

This means the downstream builds directly against the local checkout of this repo. Remove the `replace` line and bump to a real tagged version when publishing a release.

### What gets synced vs. what stays separate

| Path | Synced? | Notes |
|---|---|---|
| `cmd/`, `internal/`, `app/` | No | Shared at build time via `replace` directive |
| `alerts/` | Yes | Prometheus rules are identical |
| `deploy/gateway/` | Yes | Gateway API manifests |
| `helm/kcert-checker/templates/` | Yes | Chart templates (values.yaml differs) |
| `test-certs-deploy/` | Yes | Demo cert manifests |
| `ui/` | Yes | Frontend (excluding node_modules/.next) |
| `Dockerfile` | No | Downstream uses a different base image |
| `helm/values.yaml` | No | Downstream has org-specific image registry |
| `config.yaml` | No | Downstream points to internal clusters |
| `Makefile` | No | Downstream has org-specific deploy targets |

### How to sync (run from the downstream repo)

```bash
cd ~/workdir/internal/cronus/kcert-checker
make sync-from-public     # rsyncs non-Go assets from the public repo
git diff                  # review what changed
git add -p && git commit -m "sync: pull latest from public kcert-checker"
git push
```

Sync after changes to `alerts/`, `helm/templates/`, `ui/`, or `test-certs-deploy/`. Go source changes are picked up automatically — no sync needed.
