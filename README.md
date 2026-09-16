# kcert-checker

A Kubernetes-native certificate expiry checker. It scans every running Pod container and every Secret cluster-wide, parses PEM certificates found at known paths, and exposes their `NotAfter` timestamps as Prometheus metrics. Alert rules fire at 7, 30, 60, and 90 days before expiry.

---

## How it works

```
kcert-checker pod
  ├── On startup: scan all namespaces
  ├── Every N hours (default 24): scan again
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
        kcert_certificate_expiry_timestamp_seconds{namespace,pod,container,path,source}
        kcert_scan_timestamp_seconds
        kcert_scan_errors_total
```

Prometheus scrapes the metrics endpoint and evaluates five alert rules:

| Alert                                 | Condition        | Severity |
| ------------------------------------- | ---------------- | -------- |
| `KCertCertificateExpired`             | `NotAfter ≤ now` | critical |
| `KCertCertificateExpiresWithin7Days`  | `0 < days ≤ 7`   | critical |
| `KCertCertificateExpiresWithin30Days` | `7 < days ≤ 30`  | warning  |
| `KCertCertificateExpiresWithin60Days` | `30 < days ≤ 60` | warning  |
| `KCertCertificateExpiresWithin90Days` | `60 < days ≤ 90` | warning  |

---

## Repository layout

```
kcert-checker/
├── cmd/kcert-checker/main.go       # entry point
├── internal/
│   ├── config/config.go            # config structs + YAML loading
│   ├── kubernetes/                 # k8s client, pod/namespace listing
│   ├── metrics/metrics.go          # Prometheus gauge/counter definitions
│   └── scanner/                    # certificate scanning logic
│       ├── scanner.go              # scan loop (secrets + pods)
│       ├── container.go            # exec cat via SPDY
│       └── certificate.go          # PEM → x509 parsing
├── deploy/                         # raw kubectl manifests
├── helm/kcert-checker/             # Helm chart (preferred)
│   ├── Chart.yaml
│   ├── values.yaml
│   └── templates/
├── alerts/
│   ├── certificate-alerts.yaml     # standalone PrometheusRule (optional)
│   └── prometheus-kcert-values.yaml
├── prometheus-kcert-values.yaml    # Helm values to wire Prometheus scrape + alerts
├── test-certs-deploy/
│   └── kcert-test-pod.yaml         # test pod covering all 5 alert tiers
├── Dockerfile
├── COMMANDS.md                     # full command reference
└── SETUP.md                        # end-to-end setup guide (kind → running alerts)
```

---

## Quick start (local kind cluster)

For the full step-by-step guide including kind setup, image loading, Prometheus wiring, and alert verification, see **[SETUP.md](SETUP.md)**.

For every individual command used in this project, see **[COMMANDS.md](COMMANDS.md)**.

### 1. Prerequisites

```bash
# Install tools (macOS)
brew install go docker kind kubectl helm
```

### 2. Build the image

```bash
# Build for your local architecture
docker build -t kcert-checker:local .

# Load into kind (no registry needed)
kind load docker-image kcert-checker:local
```

### 3. Deploy with Helm

```bash
kubectl create namespace monitoring

helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --set image.repository=kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never
```

### 4. Wire Prometheus

```bash
# Install Prometheus (if not already present)
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install prometheus prometheus-community/prometheus -n monitoring

# Add kcert scrape job + alert rules
helm upgrade prometheus prometheus-community/prometheus \
  -n monitoring \
  -f prometheus-kcert-values.yaml

# Trigger config reload
kubectl port-forward -n monitoring svc/prometheus-server 9092:80 &
curl -s -XPOST http://localhost:9092/-/reload
```

### 5. Verify

```bash
# Check metrics directly
kubectl port-forward -n monitoring svc/kcert-checker 9091:8080
curl http://localhost:9091/metrics | grep kcert

# Check Prometheus targets (should show job=kcert-checker health=up)
kubectl port-forward -n monitoring svc/prometheus-server 9090:80
# Open http://localhost:9090 → Status → Targets
```

---

## Deploying to a real cluster

Use the Helm chart with a real image from a registry:

```bash
# Build and push (cross-platform for amd64 clusters)
docker buildx build \
  --platform linux/amd64 \
  -t sirajudheenam/kcert-checker:latest \
  --push .

# Deploy
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set image.repository=sirajudheenam/kcert-checker \
  --set image.tag=latest
```

---

## Certificate paths scanned

The scanner checks these paths inside every container (missing paths are silently skipped):

```
/etc/ssl/postfix/tls.crt    /etc/ssl/postfix/ca.crt
/etc/tls/tls.crt            /etc/tls/ca.crt
/etc/certs/tls.crt          /etc/certs/server.crt    /etc/certs/ca.crt
/var/run/secrets/tls/tls.crt  /var/run/secrets/tls/ca.crt
```

To add more paths, edit `scanner.certificates.paths` in `helm/kcert-checker/values.yaml` or the ConfigMap.

---

## Testing

### Unit tests

Run all unit tests (no cluster required):

```bash
go test ./...
```

Run with coverage report:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out        # open in browser
go tool cover -func=coverage.out        # per-function summary
```

Run a specific package:

```bash
go test -v ./internal/scanner/...
go test -v ./internal/output/...
```

### Integration tests

Integration tests run against a live Kubernetes cluster. They apply fixtures to a
dedicated `kcert-integration` namespace and verify the scanner finds and classifies
real certificates correctly.

**Prerequisites:**

- A running cluster accessible via `KUBECONFIG` (kind is fine)
- `kubectl` in `$PATH`

**1. Apply fixtures (one-time setup):**

```bash
kubectl apply -f tests/integration/fixtures/
```

This creates:
- `kcert-integration` namespace
- 4 Secrets with real PEM certs at different expiry tiers (expired, critical, warning, healthy)
- A Pod with 7 containers each mounting a cert at a known path

**2. Run integration tests:**

```bash
go test -v -count=1 -tags integration -timeout 5m ./tests/integration/...
```

Or via Make:

```bash
# Spin up a fresh kind cluster, apply fixtures, run tests, tear down
make integration-test

# Run against your already-running cluster (skip kind lifecycle)
make integration-test-existing
```

**3. Tear down fixtures (optional):**

```bash
kubectl delete namespace kcert-integration
```

### Manual end-to-end test (demo secrets)

The `monitoring` namespace has four demo secrets pre-loaded covering all status tiers:

| Secret | Subject | Status |
|---|---|---|
| `monitoring-ca` | `monitoring-ca` | OK (~5 years) |
| `monitoring-api-tls` | `api.monitoring.svc` | OK (~400 days) |
| `monitoring-grafana-tls` | `grafana.monitoring.svc` | WARNING (~20 days) |
| `monitoring-alertmanager-tls` | `alertmanager.monitoring.svc` | CRITICAL (~5 days) |

To re-apply them if they are deleted:

```bash
kubectl apply -f test-certs-deploy/monitoring-demo-secrets.yaml
```

Run a one-shot scan to see all certs in both tables:

```bash
go build -o kcert-checker ./cmd/kcert-checker
./kcert-checker --config config.yaml --output table --once
```

Expected output (Secrets section):

```
=== Kubernetes Secrets ===
STATUS    NAMESPACE   SOURCE NAME                          SUBJECT                      ISSUER         EXPIRES     DAYS LEFT
------    ---------   -----------                          -------                      ------         -------     ---------
CRITICAL  monitoring  monitoring-alertmanager-tls/tls.crt  alertmanager.monitoring.svc  monitoring-ca  ...         4
OK        monitoring  monitoring-api-tls/tls.crt           api.monitoring.svc           monitoring-ca  ...         399
OK        monitoring  monitoring-ca/ca.crt                 monitoring-ca                monitoring-ca  ...         1824
WARNING   monitoring  monitoring-grafana-tls/tls.crt       grafana.monitoring.svc       monitoring-ca  ...         19
```

---

## Known issues / gotchas

| Issue                                   | Root cause                                                                               | Fix                                                                    |
| --------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `ImagePullBackOff` on Apple Silicon     | No arm64 image on DockerHub                                                              | Build locally + `kind load docker-image`                               |
| `CreateContainerConfigError`            | `runAsNonRoot: true` rejects string usernames                                            | Set `runAsUser: 65532` (numeric UID)                                   |
| Config file not found                   | Volume mounted at `/etc/kcert` but `--config` points to `/etc/kcert-checker/config.yaml` | Fixed in Helm chart; patch raw deploy with `kubectl patch`             |
| kcert-checker not scraped by Prometheus | `endpointslice` SD doesn't expose `__meta_kubernetes_service_name`                       | Use `__meta_kubernetes_endpointslice_label_kubernetes_io_service_name` |

See COMMANDS.md §11 for the full list with patch commands.

---

## From Scratch

```bash
# mkdir -p kcert-checker/{bin,cmd,internal,scripts,helm,k8s,test-certs-deploy}

```
