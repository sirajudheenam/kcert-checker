# kcert-checker: End-to-End Setup Guide

This guide covers everything from a fresh machine to verified Prometheus alerts firing in a local kind cluster.

---

## Prerequisites

### Install required tools (macOS)

```bash
brew install go docker kind kubectl helm
```

Verify versions:

```bash
go version        # 1.21+
docker --version
kind --version    # 0.20+
kubectl version --client
helm version      # 3.x
```

### Docker must be running

```bash
docker info   # must return without error
```

---

## 1. Create a kind cluster

kind runs Kubernetes inside Docker containers — no cloud account or VM needed.

```bash
# Create a single-node cluster
kind create cluster --name kind

# Verify it's running
kubectl cluster-info --context kind-kind
kubectl get nodes
```

Expected output:

```
NAME                 STATUS   ROLES           AGE   VERSION
kind-control-plane   Ready    control-plane   30s   v1.37.x
```

---

## 2. Build the kcert-checker image

```bash
# The DockerHub image (`sirajudheenam/kcert-checker:latest`) only has an amd64 build. On Apple Silicon (M1/M2/M3) you must build locally.
docker buildx build \
  --platform linux/arm64 \
  -t sirajudheenam/kcert-checker:local \
  --push .

docker buildx build \
  --platform linux/arm64 \
  -t kcert-checker:local .

kind load docker-image sirajudheenam/kcert-checker:local --name=kind
kind load docker-image sirajudheenam/kcert-checker:latest --name=kind

docker run --rm -it \
  -e KUBECONFIG=/kube/config \
  -v "$HOME/.kube/config:/kube/config:ro" \
  -v "$(pwd)/config.yaml:/app/config.yaml:ro" \
  sirajudheenam/kcert-checker:local-arm64 \
  --config /app/config.yaml

# Build for your local architecture (works on both amd64 and arm64)
docker build -t kcert-checker:local .

# docker run --rm -it kcert-checker:local \
#   --config /config.yaml

# Load the image into kind (bypasses any registry entirely)
kind load docker-image kcert-checker:local


```

Verify the image is in kind:

```bash
docker exec kind-control-plane crictl images | grep kcert

# docker exec cka-control-plane crictl images | grep kcert
# docker.io/library/kcert-checker                 local                                   b867fb5b31b4f       10.5MB
# docker.io/sirajudheenam/kcert-checker           local-arm64                             0f5638e6aac46       9.36MB

```

### For a real cluster (not kind): push to a registry

```bash
# Cross-compile for amd64 clusters from an Apple Silicon Mac
docker buildx build \
  --platform linux/amd64 \
  -t sirajudheenam/kcert-checker:latest \
  --push .
```

---

## 3. Create the monitoring namespace

```bash
kubectl create namespace monitoring
```

---

## 4. Install Prometheus

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm install prometheus prometheus-community/prometheus \
  --namespace monitoring

# The Prometheus server can be accessed via port 80 on the following DNS name from within your cluster:
# prometheus-server.monitoring.svc.cluster.local

# Get the Prometheus server URL by running these commands in the same shell:
export PROM_SERVER_POD_NAME=$(kubectl get pods --namespace monitoring -l "app.kubernetes.io/name=prometheus,app.kubernetes.io/instance=prometheus" -o jsonpath="{.items[0].metadata.name}")
echo "PROMETHEUS_SERVER POD: $PROM_SERVER_POD_NAME"
kubectl --namespace monitoring port-forward $PROM_SERVER_POD_NAME 9090

# Prometheus alertmanager can be accessed via port 9093 on the following DNS name from within your cluster:
# prometheus-alertmanager.monitoring.svc.cluster.local
# Get the Alertmanager URL by running these commands in the same shell:
export PROM_ALER_MANAGER_POD_NAME=$(kubectl get pods --namespace monitoring -l "app.kubernetes.io/name=alertmanager,app.kubernetes.io/instance=prometheus" -o jsonpath="{.items[0].metadata.name}")
echo "ALER MANAGER POD: $PROM_ALER_MANAGER_POD_NAME"
kubectl --namespace monitoring port-forward $PROM_ALER_MANAGER_POD_NAME 9093

# Prometheus Pushgateway can be accessed via port 9091 on the following DNS name from within your cluster:
# prometheus-prometheus-pushgateway.monitoring.svc.cluster.local

# Get the Pushgateway URL by running these commands in the same shell:
export PROM_PUSH_GATEWAY_POD_NAME=$(kubectl get pods --namespace monitoring -l "app.kubernetes.io/name=prometheus-pushgateway,app.kubernetes.io/instance=prometheus" -o jsonpath="{.items[0].metadata.name}")
echo "PROMETHEUS PUSH_GATEWAY POD: $PROM_PUSH_GATEWAY_POD_NAME"
  kubectl --namespace monitoring port-forward $PROM_PUSH_GATEWAY_POD_NAME 9091

# Wait for Prometheus to be ready
kubectl rollout status deployment/prometheus-server -n monitoring
```

---

## 5. Deploy kcert-checker

### Option A: Helm chart (recommended)

```bash
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --set image.repository=sirajudheenam/kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never
```

For a real cluster with a registry image, omit the `--set` overrides:

```bash
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring
```

### Option B: Raw kubectl manifests

```bash
kubectl apply -f deploy/serviceaccount.yaml
kubectl apply -f deploy/clusterrole.yaml
kubectl apply -f deploy/clusterrolebinding.yaml
kubectl apply -f deploy/configmap.yaml
kubectl apply -f deploy/deployment.yaml
kubectl apply -f deploy/service.yaml

# Fix: patch image to use local kind image
kubectl set image deployment/kcert-checker \
  kcert-checker=kcert-checker:local -n monitoring
kubectl patch deployment kcert-checker -n monitoring \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"kcert-checker","imagePullPolicy":"Never"}]}}}}'
```

Note: `deploy/servicemonitor.yaml` requires the Prometheus Operator CRD (`monitoring.coreos.com/v1`) which is not present with the standalone `prometheus-community/prometheus` chart. Skip it unless using kube-prometheus-stack.

### Verify kcert-checker is running

```bash
kubectl get pods -n monitoring | grep kcert
kubectl logs -n monitoring deployment/kcert-checker

kubectl port-forward svc/kcert-checker 8080:8080 -n monitoring
kubectl port-forward svc/kcert-checker-ui 3000:3000 -n monitoring

curl -s http://localhost:8080/healthz
echo
curl -s http://localhost:8080/readyz
echo
curl -s http://localhost:8080/metrics
echo
curl -s http://localhost:3000/
echo

```

Expected log output:

```
Starting kcert-checker
Scanning namespace: monitoring
Certificate found: pod=... container=... path=...
Scan complete
```

---

## 6. Wire Prometheus to scrape kcert-checker

The file `prometheus-kcert-values.yaml` contains:

- A scrape job (`job_name: kcert-checker`) using endpointslice service discovery
- Five alert rules covering expired through 90-day expiry windows

```bash
helm upgrade prometheus prometheus-community/prometheus \
  --namespace monitoring \
  -f prometheus-kcert-values.yaml

# Verify Prometheus config rolled out
kubectl rollout status deployment/prometheus-server -n monitoring

# Trigger a live config reload (no restart needed)
kubectl port-forward -n monitoring svc/prometheus-server 9092:80 &
sleep 2
curl -s -XPOST http://localhost:9092/-/reload
echo "Config reloaded"

# Kill the port-forward
pkill -f "port-forward.*9092"
```

---

## 7. Verify Prometheus is scraping kcert-checker

```bash
kubectl port-forward -n monitoring svc/prometheus-server 9092:80 &
sleep 2
```

Check scrape targets:

```bash
curl -s http://localhost:9092/api/v1/targets | python3 -c "
import sys,json
d=json.load(sys.stdin)
for t in d['data']['activeTargets']:
    job = t.get('labels',{}).get('job','')
    if 'kcert' in job:
        print(job, '->', t.get('health'), '|', t.get('scrapeUrl'))
        if t.get('lastError'): print('  ERROR:', t['lastError'])
"
```

Expected:

```
kcert-checker -> up | http://10.244.0.x:8080/metrics
```

If the job does not appear, check the scrape config was applied:

```bash
curl -s http://localhost:9092/api/v1/status/config | python3 -c "
import sys,json; d=json.load(sys.stdin); print(d['data']['yaml'][:3000])
" | grep -A5 kcert
```

---

## 8. Check certificate expiry metrics

```bash
# All certs found with days remaining
curl -s "http://localhost:9092/api/v1/query?query=kcert_certificate_expiry_timestamp_seconds" | python3 -c "
import sys,json,time
d=json.load(sys.stdin)
results=d['data']['result']
if not results: print('(no data — trigger a scan with: kubectl rollout restart deployment/kcert-checker -n monitoring)')
for r in results:
    l=r['metric']
    days=(float(r['value'][1])-time.time())/86400
    print(f\"source={l.get('source')} ns={l.get('namespace')} pod={l.get('pod')} path={l.get('path')} expires_in={days:.1f}d\")
"
```

---

## 9. Deploy the test pod (optional — verify all alert tiers)

The test pod creates certs at all expiry windows so you can verify every alert rule fires correctly.

```bash
kubectl apply -f test-certs-deploy/kcert-test-pod.yaml
kubectl wait --for=condition=Ready pod/kcert-test -n monitoring --timeout=120s

# Force an immediate kcert-checker scan
kubectl rollout restart deployment/kcert-checker -n monitoring
kubectl rollout status deployment/kcert-checker -n monitoring
```

Check the scan found the test certs:

```bash
kubectl logs -n monitoring deployment/kcert-checker | grep "kcert-test"
```

Check metrics for the test pod:

```bash
curl -s "http://localhost:9092/api/v1/query?query=kcert_certificate_expiry_timestamp_seconds" | python3 -c "
import sys,json,time
d=json.load(sys.stdin)
for r in d['data']['result']:
    l=r['metric']
    if l.get('pod') != 'kcert-test': continue
    days=(float(r['value'][1])-time.time())/86400
    print(f\"{l.get('container'):<25} path={l.get('path'):<35} expires_in={days:.1f}d\")
"
```

---

## 10. Verify alerts in Prometheus

Alert rules have `for: 10m` — wait up to 10 minutes for `pending → firing`.

```bash
# Currently firing or pending alerts
curl -s "http://localhost:9092/api/v1/alerts" | python3 -c "
import sys,json
d=json.load(sys.stdin)
alerts=d['data']['alerts']
if not alerts: print('No alerts yet (may still be in 10m pending window)')
for a in alerts:
    print(f\"[{a['state']}] {a['labels'].get('alertname')} severity={a['labels'].get('severity')}\")
    print(f\"  ns={a['labels'].get('namespace')} pod={a['labels'].get('pod')} container={a['labels'].get('container')}\")
"

# All kcert alert rules and their state
curl -s "http://localhost:9092/api/v1/rules" | python3 -c "
import sys,json
d=json.load(sys.stdin)
for g in d['data']['groups']:
    if 'kcert' in g['name'].lower():
        print(f\"Group: {g['name']}\")
        for r in g['rules']:
            print(f\"  [{r.get('state','n/a')}] {r['name']}\")
"
```

---

## 11. Prometheus UI

```bash
kubectl port-forward -n monitoring svc/prometheus-server 9090:80
# Open http://localhost:9090
```

- **Status → Targets**: look for `job=kcert-checker` with state `UP`
- **Alerts**: click Alerts → expand `kcert-certificate-expiry` group
- **Graph**: paste PromQL expressions:

```promql
# Days until expiry for all certs
(kcert_certificate_expiry_timestamp_seconds - time()) / 86400

# Only the test pod
(kcert_certificate_expiry_timestamp_seconds{pod="kcert-test"} - time()) / 86400

# Certs expiring within 7 days
(kcert_certificate_expiry_timestamp_seconds - time()) / 86400 < 7

# Hours since last scan
(time() - kcert_scan_timestamp_seconds) / 3600

# Total scan errors
kcert_scan_errors_total
```

---

## 12. Trigger a manual scan

kcert-checker scans on startup then every 24 hours (configurable). To force an immediate scan:

```bash
kubectl rollout restart deployment/kcert-checker -n monitoring
kubectl rollout status deployment/kcert-checker -n monitoring
```

---

## 13. Configuration

View the current config:

```bash
kubectl get configmap kcert-checker-config -n monitoring \
  -o jsonpath='{.data.config\.yaml}'
```

Edit in-place:

```bash
kubectl edit configmap kcert-checker-config -n monitoring
# Then restart to pick up changes:
kubectl rollout restart deployment/kcert-checker -n monitoring
```

Or change via Helm values:

```bash
helm upgrade kcert-checker ./helm/kcert-checker \
  -n monitoring \
  --set config.scan.interval_seconds=10   # 10 seconds
```

---

## Troubleshooting

### kcert-checker pod is in `ImagePullBackOff`

The DockerHub image has no arm64 build. Use the local image:

```bash
docker build -t kcert-checker:local .
kind load docker-image kcert-checker:local
kubectl set image deployment/kcert-checker kcert-checker=kcert-checker:local -n monitoring
kubectl patch deployment kcert-checker -n monitoring \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"kcert-checker","imagePullPolicy":"Never"}]}}}}'
```

### kcert-checker pod is in `CreateContainerConfigError`

Kubernetes rejects a string username (`nonroot`) with `runAsNonRoot: true`. Fix: ensure `runAsUser: 65532` is set (done in the Helm chart and patched `deploy/deployment.yaml`).

### Config file not found: `/etc/kcert-checker/config.yaml`

The original `deploy/deployment.yaml` mounted the ConfigMap at `/etc/kcert` instead of `/etc/kcert-checker`. Fix with the Helm chart (already correct) or:

```bash
kubectl patch deployment kcert-checker -n monitoring --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/volumeMounts/0","value":{"name":"config","mountPath":"/etc/kcert-checker","readOnly":true}}]'
```

### Prometheus does not show `kcert-checker` as a scrape target

The `endpointslice` SD role does not expose `__meta_kubernetes_service_name`. The correct relabel label is:

```
__meta_kubernetes_endpointslice_label_kubernetes_io_service_name
```

This is already correct in `prometheus-kcert-values.yaml`. If you see `kcert-checker` in `droppedTargets`, recheck the relabel config applied to Prometheus:

```bash
curl -s http://localhost:9092/api/v1/status/config | python3 -c \
  "import sys,json; print(json.load(sys.stdin)['data']['yaml'])" | grep -A 30 kcert-checker
```

### Prometheus config not reloading after `helm upgrade`

The Prometheus config-reloader sidecar watches the ConfigMap. If changes don't appear, force a reload:

```bash
kubectl port-forward -n monitoring svc/prometheus-server 9092:80 &
curl -s -XPOST http://localhost:9092/-/reload
```

---

## Clean up

```bash
# Remove test pod
kubectl delete pod kcert-test -n monitoring --ignore-not-found

# Uninstall kcert-checker
helm uninstall kcert-checker -n monitoring

# Uninstall Prometheus
helm uninstall prometheus -n monitoring

# Delete the kind cluster entirely
kind delete cluster --name kind
```
