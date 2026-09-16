# kcert-checker Helm Chart

This chart deploys `kcert-checker` — a Kubernetes-native TLS certificate expiry scanner — into a cluster. It creates a Deployment, ServiceAccount, ClusterRole/ClusterRoleBinding, ConfigMap, and Service. An optional ServiceMonitor is available for kube-prometheus-stack environments.

---

## Prerequisites

- Kubernetes 1.21+
- Helm 3.x
- `monitoring` namespace (or pass `--create-namespace`)
- For local kind testing: image loaded via `kind load docker-image`

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
    ├── service.yaml
    ├── serviceaccount.yaml
    └── servicemonitor.yaml    # only rendered when serviceMonitor.enabled=true
```

---

## Install

### Local kind cluster (no registry)

```bash
# Build and load image first
docker build -t kcert-checker:local .
kind load docker-image kcert-checker:local

# Install chart
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set image.repository=kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never
```

### From a registry (real cluster)

```bash
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace
```

This uses the default `values.yaml` image: `sirajudheenam/kcert-checker:latest`.

---

## Upgrade

```bash
helm upgrade kcert-checker ./helm/kcert-checker -n monitoring

# Upgrade with a new image tag
helm upgrade kcert-checker ./helm/kcert-checker \
  -n monitoring \
  --set image.tag=v1.2.3
```

---

## Uninstall

```bash
helm uninstall kcert-checker -n monitoring
```

This removes the Deployment, Service, ConfigMap, and ServiceAccount. The ClusterRole and ClusterRoleBinding are also removed.

---

## Key values

| Key | Default | Description |
|---|---|---|
| `image.repository` | `sirajudheenam/kcert-checker` | Image repository |
| `image.tag` | `latest` | Image tag |
| `image.pullPolicy` | `IfNotPresent` | Set to `Never` for local kind images |
| `securityContext.runAsUser` | `65532` | Required: must be numeric when `runAsNonRoot: true` |
| `config.scan.interval_seconds` | `10` | Scan interval in **seconds** |
| `config.kubernetes.mode` | `auto` | `auto` / `in-cluster` / `kubeconfig` |
| `serviceMonitor.enabled` | `false` | Enable only for kube-prometheus-stack (Prometheus Operator) |

See `values.yaml` for the full list with inline documentation.

---

## Prometheus integration

### Standalone Prometheus (Helm chart)

Apply `prometheus-kcert-values.yaml` from the repo root to add the scrape job and alert rules:

```bash
helm upgrade prometheus prometheus-community/prometheus \
  -n monitoring \
  -f prometheus-kcert-values.yaml

# Trigger config reload (no restart needed)
kubectl port-forward -n monitoring svc/prometheus-server 9092:80
curl -s -XPOST http://localhost:9092/-/reload
```

### kube-prometheus-stack (Prometheus Operator)

Enable the ServiceMonitor instead:

```bash
helm upgrade kcert-checker ./helm/kcert-checker \
  -n monitoring \
  --set serviceMonitor.enabled=true
```

---

## Validate before deploying

```bash
helm lint ./helm/kcert-checker
helm template kcert-checker ./helm/kcert-checker --namespace monitoring
```

---

## RBAC requirements

The chart creates a `ClusterRole` with:
- `get`, `list`, `watch` on `pods`, `secrets`, `namespaces`
- `create` on `pods/exec` (needed for `cat <cert-path>` inside containers)

If your cluster restricts `pods/exec` cluster-wide, kcert-checker will still scan Secrets but will not be able to scan Pod containers.

---

## Scanning configuration

Certificate paths checked inside every container are configured in `values.yaml` under `config.scanner.certificates.paths`. The defaults cover common patterns:

```yaml
config:
  scanner:
    certificates:
      paths:
        - /etc/ssl/postfix/tls.crt
        - /etc/ssl/postfix/ca.crt
        - /etc/tls/tls.crt
        - /etc/tls/ca.crt
        - /etc/certs/tls.crt
        - /etc/certs/server.crt
        - /etc/certs/ca.crt
        - /var/run/secrets/tls/tls.crt
        - /var/run/secrets/tls/ca.crt
```

Missing paths are silently skipped — most containers won't have most paths.
