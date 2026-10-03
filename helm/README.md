# kcert-checker Helm Chart

Deploys `kcert-checker` — a Kubernetes-native TLS certificate expiry scanner — into any cluster. Creates a Deployment, ServiceAccount, ClusterRole/ClusterRoleBinding, ConfigMap, and Service. An optional ServiceMonitor is available for kube-prometheus-stack environments.

---

## Chart repository

The chart is published to GitHub Pages on every `helm/v*` tag via GitHub Actions.

```bash
helm repo add kcert-checker https://sirajudheenam.github.io/kcert-checker
helm repo update
helm search repo kcert-checker
```

---

## Installing

### From the published Helm repository (recommended)

```bash
# Default install
helm install kcert-checker kcert-checker/kcert-checker \
  --namespace monitoring \
  --create-namespace

# With Prometheus Operator (ServiceMonitor)
helm install kcert-checker kcert-checker/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set serviceMonitor.enabled=true

# Specific version
helm install kcert-checker kcert-checker/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --version 0.1.0
```

### From source (local chart directory)

```bash
# Local kind cluster — image loaded via kind load
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set image.repository=kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never

# Real cluster — image from a registry
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set image.repository=sirajudheenam/kcert-checker \
  --set image.tag=latest
```

---

## Upgrading

```bash
# From published repo
helm upgrade kcert-checker kcert-checker/kcert-checker -n monitoring

# From source, pinning a new tag
helm upgrade kcert-checker ./helm/kcert-checker \
  -n monitoring \
  --set image.tag=v1.2.3
```

---

## Uninstalling

```bash
helm uninstall kcert-checker -n monitoring
```

Removes the Deployment, Service, ConfigMap, ServiceAccount, ClusterRole, and ClusterRoleBinding.

---

## Chart structure

```
helm/kcert-checker/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── _helpers.tpl
    ├── clusterrole.yaml
    ├── clusterrolebinding.yaml
    ├── configmap.yaml
    ├── deployment.yaml
    ├── deployment-ui.yaml        # only rendered when ui.enabled=true
    ├── service.yaml
    ├── serviceaccount.yaml
    ├── servicemonitor.yaml       # only rendered when serviceMonitor.enabled=true
    └── tests.yaml
```

---

## Key values

| Key | Default | Description |
|---|---|---|
| `image.repository` | `sirajudheenam/kcert-checker` | Backend image repository |
| `image.tag` | `latest` | Backend image tag |
| `image.pullPolicy` | `IfNotPresent` | Set to `Never` for local kind images |
| `ui.enabled` | `true` | Deploy the Next.js dashboard UI |
| `ui.image.repository` | `sirajudheenam/kcert-checker-ui` | UI image repository |
| `ui.image.tag` | `latest` | UI image tag |
| `securityContext.runAsUser` | `65532` | Must be numeric when `runAsNonRoot: true` |
| `config.kubernetes.mode` | `in-cluster` | `in-cluster` / `kubeconfig` / `auto` |
| `config.scan.interval_seconds` | `3600` | Scan interval in seconds |
| `config.scan.warning_days` | `30` | Days before expiry to fire a warning |
| `config.scan.critical_days` | `7` | Days before expiry to fire a critical alert |
| `serviceMonitor.enabled` | `false` | Enable for kube-prometheus-stack (Prometheus Operator) |
| `serviceMonitor.interval` | `30s` | Prometheus scrape interval |

See [`values.yaml`](values.yaml) for the full list with inline documentation.

---

## Prometheus integration

### Standalone Prometheus (Helm chart)

Apply `alerts/prometheus-kcert-values.yaml` from the repo root to add the scrape job and alert rules:

```bash
helm upgrade prometheus prometheus-community/prometheus \
  -n monitoring \
  -f alerts/prometheus-kcert-values.yaml

# Trigger a config reload without restart
kubectl port-forward -n monitoring svc/prometheus-server 9092:80
curl -s -XPOST http://localhost:9092/-/reload
```

### kube-prometheus-stack (Prometheus Operator)

Enable the ServiceMonitor:

```bash
helm upgrade kcert-checker kcert-checker/kcert-checker \
  -n monitoring \
  --set serviceMonitor.enabled=true
```

---

## RBAC

The chart creates a `ClusterRole` with read access across the whole cluster:

- `get`, `list`, `watch` on `pods`, `secrets`, `namespaces`
- `create` on `pods/exec` (needed to exec `cat <cert-path>` inside containers)

If your cluster restricts `pods/exec` cluster-wide, kcert-checker will still scan Secrets but will skip in-container path scanning.

---

## Scanning configuration

Certificate paths checked inside every container are set in `values.yaml` under `config.scanner.certificates.paths`. The defaults cover common patterns:

```yaml
config:
  scanner:
    certificates:
      paths:
        - /etc/ssl/postfix/*
        - /etc/tls/*
        - /etc/certs/*
        - /var/run/secrets/tls/*
```

Glob patterns are expanded inside each container via `ls -1 <pattern>`. Missing paths are silently skipped.

---

## Validating before deploying

```bash
helm lint ./helm/kcert-checker
helm template kcert-checker ./helm/kcert-checker --namespace monitoring
```

---

## Releasing a new chart version

Chart releases are independent of the application image and are driven by a `helm/v*` git tag.

```bash
# Bump version, commit, tag, and trigger CI publish in one step
make helm-release VERSION=0.2.0

# Test the package locally without pushing
make helm-dry-run
```

The GitHub Actions [helm-release workflow](../.github/workflows/helm-release.yml) uses `chart-releaser` to package the chart and commit it to the `gh-pages` branch. The Helm repository index at `https://sirajudheenam.github.io/kcert-checker/index.yaml` is updated automatically.

**One-time GitHub Pages setup:**
```bash
git checkout --orphan gh-pages
git reset --hard
git commit --allow-empty -m "init gh-pages"
git push origin gh-pages
git checkout main
```
Then in GitHub **Settings → Pages → Source**: Branch = `gh-pages` / root.

---

## Syncing into a private/internal chart repo

If you maintain an internal Helm chart repository (e.g. a SAP-internal `cronus-helm-charts` repo), keep the vendored chart in sync with the public source by running from the internal downstream repo:

```bash
make sync-chart-from-public
```

This rsyncs `helm/kcert-checker/templates/` and bumps the chart version in the internal `Chart.yaml`, without touching the internal `values.yaml` (which carries private registry and environment overrides).
