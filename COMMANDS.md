# kcert-checker: Command Reference
# All commands used in this session, in order.
# Generated: 2026-09-12

# ─────────────────────────────────────────────
# 1. IMAGE: Build & load into kind
# ─────────────────────────────────────────────

docker build -t kcert-checker:local .

kind load docker-image kcert-checker:local


# ─────────────────────────────────────────────
# 2. DEPLOYMENT: Switch from DockerHub image to local
# ─────────────────────────────────────────────

# Update image
kubectl set image deployment/kcert-checker \
  kcert-checker=kcert-checker:local \
  -n monitoring

# Set imagePullPolicy to Never (image is local, not in a registry)
kubectl patch deployment kcert-checker -n monitoring \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"kcert-checker","imagePullPolicy":"Never"}]}}}}'

# Fix runAsNonRoot: Kubernetes requires a numeric UID — "nonroot" (string) is rejected.
# 65532 is the standard distroless/nonroot UID.
kubectl patch deployment kcert-checker -n monitoring \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"kcert-checker","securityContext":{"allowPrivilegeEscalation":false,"readOnlyRootFilesystem":true,"runAsNonRoot":true,"runAsUser":65532}}]}}}}'

# Fix volume mount path mismatch:
#   Original deploy/ manifest mounted ConfigMap at /etc/kcert
#   but --config arg pointed to /etc/kcert-checker/config.yaml
kubectl patch deployment kcert-checker -n monitoring --type=json \
  -p='[
    {"op":"replace","path":"/spec/template/spec/volumes/0","value":{"name":"config","configMap":{"name":"kcert-checker-config"}}},
    {"op":"replace","path":"/spec/template/spec/containers/0/volumeMounts/0","value":{"name":"config","mountPath":"/etc/kcert-checker","readOnly":true}}
  ]'

# Watch rollout
kubectl rollout status deployment/kcert-checker -n monitoring


# ─────────────────────────────────────────────
# 3. STATUS CHECKS
# ─────────────────────────────────────────────

# All pods in monitoring namespace
kubectl get pods -n monitoring

# kcert-checker pod specifically
kubectl get pods -n monitoring | grep kcert

# kcert-checker logs (scan results, errors)
kubectl logs -n monitoring deployment/kcert-checker

# Only certificate findings
kubectl logs -n monitoring deployment/kcert-checker | grep "Certificate found"

# Pod events (useful for ImagePullBackOff, CreateContainerConfigError, etc.)
kubectl get events -n monitoring --sort-by='.lastTimestamp' | grep kcert

# Describe a specific pod (replace pod name)
kubectl describe pod kcert-checker-<hash> -n monitoring

# Check kcert-test pod containers and mounts
kubectl describe pod kcert-test -n monitoring


# ─────────────────────────────────────────────
# 4. TRIGGER AN IMMEDIATE SCAN
# (kcert-checker scans on startup, then every 24 h)
# ─────────────────────────────────────────────

kubectl rollout restart deployment/kcert-checker -n monitoring
kubectl rollout status deployment/kcert-checker -n monitoring


# ─────────────────────────────────────────────
# 5. CONFIGMAP: View / edit scan config
# ─────────────────────────────────────────────

# View current config
kubectl get configmap kcert-checker-config -n monitoring \
  -o jsonpath='{.data.config\.yaml}'

# Edit in-place
kubectl edit configmap kcert-checker-config -n monitoring


# ─────────────────────────────────────────────
# 6. METRICS: Direct check from kcert-checker pod
# (port-forward since it's ClusterIP)
# ─────────────────────────────────────────────

kubectl port-forward -n monitoring svc/kcert-checker 9091:8080

# In another terminal:
curl http://localhost:9091/metrics
curl http://localhost:9091/metrics | grep kcert


# ─────────────────────────────────────────────
# 7. PROMETHEUS: Apply kcert scrape job + alert rules
#
# NOTE on relabel fix: the endpointslice SD role does NOT expose
# __meta_kubernetes_service_name. Use the label-based equivalent:
#   __meta_kubernetes_endpointslice_label_kubernetes_io_service_name
# The correct prometheus-kcert-values.yaml already has this fix applied.
# ─────────────────────────────────────────────

helm upgrade prometheus prometheus-community/prometheus \
  -n monitoring \
  -f prometheus-kcert-values.yaml

# Verify Prometheus rolled out
kubectl rollout status deployment/prometheus-server -n monitoring

# Force a config reload without restart (useful after helm upgrade)
# (requires --web.enable-lifecycle flag — enabled by default in this chart)
kubectl port-forward -n monitoring svc/prometheus-server 9092:80
curl -s -XPOST http://localhost:9092/-/reload


# ─────────────────────────────────────────────
# 8. PROMETHEUS: Query via port-forward
# ─────────────────────────────────────────────

kubectl port-forward -n monitoring svc/prometheus-server 9092:80

# In another terminal:

# All scrape targets and their health
curl -s http://localhost:9092/api/v1/targets | python3 -c "
import sys,json
d=json.load(sys.stdin)
for t in d['data']['activeTargets']:
    print(t.get('labels',{}).get('job'), '->', t.get('health'), '|', t.get('scrapeUrl'), '| err:', t.get('lastError',''))
"

# Certificate expiry metrics with days remaining
curl -s "http://localhost:9092/api/v1/query?query=kcert_certificate_expiry_timestamp_seconds" | python3 -c "
import sys,json,time
d=json.load(sys.stdin)
results=d['data']['result']
if not results: print('(no data)')
for r in results:
    l=r['metric']
    days=(float(r['value'][1])-time.time())/86400
    print(f\"source={l.get('source')} ns={l.get('namespace')} pod={l.get('pod')} path={l.get('path')} expires_in={days:.1f}d\")
"

# Last scan timestamp
curl -s "http://localhost:9092/api/v1/query?query=kcert_scan_timestamp_seconds" | python3 -c "
import sys,json,datetime
d=json.load(sys.stdin)
for r in d['data']['result']:
    ts=float(r['value'][1])
    print('Last scan:', datetime.datetime.fromtimestamp(ts))
"

# Scan errors
curl -s "http://localhost:9092/api/v1/query?query=kcert_scan_errors_total" | python3 -c "
import sys,json
d=json.load(sys.stdin)
for r in d['data']['result']: print('Errors:', r['value'][1])
"

# Currently firing alerts
curl -s "http://localhost:9092/api/v1/alerts" | python3 -c "
import sys,json
d=json.load(sys.stdin)
alerts=d['data']['alerts']
if not alerts: print('No alerts firing')
for a in alerts:
    print(f\"[{a['state']}] {a['labels'].get('alertname')} severity={a['labels'].get('severity')}\")
    print(f\"  {a['annotations'].get('description','')}\")
"

# All alert rules and their current state (inactive / pending / firing)
curl -s "http://localhost:9092/api/v1/rules" | python3 -c "
import sys,json
d=json.load(sys.stdin)
for g in d['data']['groups']:
    if 'kcert' in g['name'].lower():
        print(f\"Group: {g['name']}\")
        for r in g['rules']:
            print(f\"  [{r.get('state','n/a')}] {r['name']}\")
"


# ─────────────────────────────────────────────
# 9. PROMETHEUS UI (browser)
# ─────────────────────────────────────────────

kubectl port-forward -n monitoring svc/prometheus-server 9090:80
# Then open: http://localhost:9090

# --- Targets page ---
# Status -> Targets
# Look for job="kcert-checker" with state=UP

# --- Graph page (useful PromQL expressions) ---
# Seconds until each certificate expires (positive = not expired):
#   kcert_certificate_expiry_timestamp_seconds - time()
#
# Days until each certificate expires:
#   (kcert_certificate_expiry_timestamp_seconds - time()) / 86400
#
# Hours since last scan:
#   (time() - kcert_scan_timestamp_seconds) / 3600
#
# Total scan errors:
#   kcert_scan_errors_total

# --- Alerts page ---
# Click "Alerts" in the top nav
# Expand the "kcert-certificate-expiry" group
# States: inactive (not triggered) -> pending (triggered, within for: window) -> firing


# ─────────────────────────────────────────────
# 10. HELM CHART (converted from deploy/)
# ─────────────────────────────────────────────

# Install
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace

# Install with local image (kind cluster)
helm install kcert-checker ./helm/kcert-checker \
  --namespace monitoring \
  --create-namespace \
  --set image.repository=kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never

# Upgrade
helm upgrade kcert-checker ./helm/kcert-checker -n monitoring

# Uninstall
helm uninstall kcert-checker -n monitoring

# Lint / dry-run
helm lint ./helm/kcert-checker
helm template kcert-checker ./helm/kcert-checker --namespace monitoring


# ─────────────────────────────────────────────
# 11. KNOWN ISSUES FIXED IN THIS SESSION
# ─────────────────────────────────────────────

# Issue 1: ImagePullBackOff
#   Image sirajudheenam/kcert-checker:latest has no arm64 build on DockerHub.
#   Fix: build locally and load into kind (see steps 1 + 2).

# Issue 2: CreateContainerConfigError — runAsNonRoot with string username
#   Kubernetes rejects a non-numeric USER (e.g. "nonroot") with runAsNonRoot: true.
#   Fix: add runAsUser: 65532 to securityContext (standard distroless nonroot UID).

# Issue 3: Config file not found (/etc/kcert-checker/config.yaml)
#   Original deploy/deployment.yaml mounted ConfigMap at /etc/kcert
#   but --config arg pointed to /etc/kcert-checker/config.yaml.
#   Fix: mount at /etc/kcert-checker (done in both the patch above and Helm chart).

# Issue 4: kcert-checker not appearing as Prometheus scrape target
#   The extraScrapeConfigs relabel rule used __meta_kubernetes_service_name
#   which does not exist for the endpointslice SD role.
#   The correct label is:
#     __meta_kubernetes_endpointslice_label_kubernetes_io_service_name
#   Fix: updated prometheus-kcert-values.yaml with the correct label name,
#   then ran: helm upgrade + curl -XPOST /-/reload to apply without restart.
#   Result: job=kcert-checker now shows health=up in Prometheus targets.

# Issue 5: Prometheus metric result used r['labels'] instead of r['metric']
#   The Prometheus HTTP API returns labels under the 'metric' key, not 'labels'.
#   Fixed in all curl/python3 query snippets in section 8 above.
