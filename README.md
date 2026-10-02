# kcert-checker

A Kubernetes-native certificate expiry checker. It scans every running Pod container and every Secret cluster-wide, parses PEM certificates found at known paths, and exposes their `NotAfter` timestamps as Prometheus metrics. Alert rules fire at 7, 30, 60, and 90 days before expiry.

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
├── deploy/                         # raw kubectl manifests (reference)
├── helm/kcert-checker/             # Helm chart (preferred deployment method)
│   ├── Chart.yaml
│   ├── values.yaml
│   └── templates/
├── alerts/
│   ├── certificate-alerts.yaml     # standalone PrometheusRule (optional)
│   └── prometheus-kcert-values.yaml
├── scripts/
│   ├── helm-publish.sh             # manual chart publish to gh-pages
│   └── gen-test-certs.sh
├── test-certs-deploy/
│   └── kcert-test-pod.yaml         # test pod covering all 5 alert tiers
├── Dockerfile
├── COMMANDS.md                     # full command reference
└── SETUP.md                        # end-to-end setup guide (kind → running alerts)
```

---

## Installing from the published Helm chart

The chart is published to GitHub Pages. This is the recommended install method for real clusters.

```bash
helm repo add kcert-checker https://sirajudheenam.github.io/kcert-checker
helm repo update

# Install with defaults (Docker Hub image, ServiceMonitor disabled)
helm install kcert-checker kcert-checker/kcert-checker \
  --namespace monitoring \
  --create-namespace

# Install with kube-prometheus-stack (Prometheus Operator)
helm install kcert-checker kcert-checker/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set serviceMonitor.enabled=true

# Install a specific chart version
helm install kcert-checker kcert-checker/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --version 0.1.0

# Upgrade to latest chart
helm upgrade kcert-checker kcert-checker/kcert-checker -n monitoring
```

See [`helm/kcert-checker/values.yaml`](helm/kcert-checker/values.yaml) for all configurable options, or [`helm/README.md`](helm/README.md) for full chart documentation.

---

## Quick start (local kind cluster)

For the full step-by-step guide including kind setup, image loading, Prometheus wiring, and alert verification, see **[SETUP.md](SETUP.md)**.

For every individual command used in this project, see **[COMMANDS.md](COMMANDS.md)**.

### 1. Prerequisites

```bash
brew install go docker kind kubectl helm
```

### 2. Build and load the image

```bash
docker build -t kcert-checker:local .
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
# Check metrics
kubectl port-forward -n monitoring svc/kcert-checker 9091:8080
curl http://localhost:9091/metrics | grep kcert

# Check Prometheus targets (Status → Targets, look for job=kcert-checker, health=up)
kubectl port-forward -n monitoring svc/prometheus-server 9090:80
```

---

## Deploying to a real cluster (from source)

```bash
# Build and push a multi-arch image
docker buildx build \
  --platform linux/amd64 \
  -t sirajudheenam/kcert-checker:latest \
  --push .

# Deploy using local chart
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set image.repository=sirajudheenam/kcert-checker \
  --set image.tag=latest
```

---

## Releasing a new chart version

The chart is versioned independently of the application image. Releases are driven by a `helm/v*` git tag which triggers the [helm-release](.github/workflows/helm-release.yml) GitHub Actions workflow — it packages the chart with `chart-releaser` and publishes it to the `gh-pages` branch.

```bash
# Bump chart version, commit, tag, and push — CI does the rest
make helm-release VERSION=0.2.0

# Test the package locally before tagging (no push)
make helm-dry-run
```

The `helm-release` Make target:
1. Bumps `version:` in `helm/kcert-checker/Chart.yaml`
2. Commits and pushes to `main`
3. Pushes the `helm/v<VERSION>` tag → GitHub Actions packages and publishes

**One-time GitHub Pages setup** (new fork or fresh repo):
```bash
# Create the gh-pages branch
git checkout --orphan gh-pages
git reset --hard
git commit --allow-empty -m "init gh-pages"
git push origin gh-pages
git checkout main
```
Then in GitHub **Settings → Pages → Source**: Branch = `gh-pages` / root.

---

## Certificate paths scanned

The scanner checks these paths inside every container (missing paths are silently skipped):

```
/etc/ssl/postfix/*
/etc/tls/*
/etc/certs/*
/var/run/secrets/tls/*
```

To add more paths, set `config.scanner.certificates.paths` in `values.yaml` or via `--set`.

---

## Testing

### Unit tests

```bash
go test ./...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration tests

Integration tests run against a live cluster and verify the scanner correctly classifies real certificates at all expiry tiers.

```bash
# Spin up a fresh kind cluster, run tests, tear it down
make integration-test

# Run against your already-running cluster
make integration-test-existing
```

### Manual end-to-end test

Four demo secrets are pre-loaded covering all status tiers:

| Secret | Status |
|---|---|
| `monitoring-ca` | OK (~5 years) |
| `monitoring-api-tls` | OK (~400 days) |
| `monitoring-grafana-tls` | WARNING (~20 days) |
| `monitoring-alertmanager-tls` | CRITICAL (~5 days) |

```bash
kubectl apply -f test-certs-deploy/monitoring-demo-secrets.yaml

go build -o kcert-checker ./cmd/kcert-checker
./kcert-checker --config config.yaml --output table --once
```

---

## Known issues / gotchas

| Issue | Root cause | Fix |
| --- | --- | --- |
| `ImagePullBackOff` on Apple Silicon | No arm64 image on DockerHub | Build locally + `kind load docker-image` |
| `CreateContainerConfigError` | `runAsNonRoot: true` rejects string usernames | Set `runAsUser: 65532` (numeric UID) |
| Config file not found | Volume mounted at `/etc/kcert` but `--config` points to `/etc/kcert-checker/config.yaml` | Fixed in Helm chart; patch raw deploy with `kubectl patch` |
| Not scraped by Prometheus | `endpointslice` SD doesn't expose `__meta_kubernetes_service_name` | Use `__meta_kubernetes_endpointslice_label_kubernetes_io_service_name` |

See COMMANDS.md §11 for the full list with patch commands.

---

## Two-repo architecture and syncing

This is the **public canonical source**. A separate internal downstream repo imports it as a Go module and adds organisation-specific configuration (Dockerfile, Helm values, deploy pipelines). The two repos are kept in sync manually when changes land here.

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

This means the downstream builds directly against the local checkout of this repo — no vendoring or tagging needed during development. Remove the `replace` line and bump to a real tagged version when publishing a release.

### What gets synced vs. what stays separate

| Path | Synced to downstream? | Notes |
|---|---|---|
| `cmd/`, `internal/`, `app/` | No — accessed via `replace` | Go source is shared at build time, not copied |
| `alerts/` | Yes | Prometheus rules are identical |
| `deploy/gateway/` | Yes | Gateway API manifests |
| `helm/kcert-checker/templates/` | Yes | Chart templates (values.yaml differs) |
| `test-certs-deploy/` | Yes | Demo cert manifests |
| `ui/` | Yes | Frontend (excluding node_modules/.next) |
| `Dockerfile` | No | Downstream uses a different base image |
| `helm/values.yaml` | No | Downstream has SAP-specific image registry and config |
| `config.yaml` | No | Downstream points to internal clusters |
| `Makefile` | No | Downstream has SAP deploy targets |

### How to sync (from the downstream repo)

```bash
# Pull all non-Go changes from the public repo into the downstream
cd ~/workdir/internal/cronus/kcert-checker
make sync-from-public

# Review what changed
git diff

# Commit and push
git add -p
git commit -m "sync: pull latest from public kcert-checker"
git push
```

`sync-from-public` warns if the public repo has uncommitted changes and skips SAP-specific files automatically.

### When to sync

Sync after any of these land in the public repo:
- Changes to `alerts/` (Prometheus rules or alert thresholds)
- Changes to `helm/kcert-checker/templates/` (new chart features)
- UI changes in `ui/`
- New test fixtures in `test-certs-deploy/`

Go source changes (`cmd/`, `internal/`, `app/`) are picked up automatically at build time via the `replace` directive — no explicit sync step needed.
