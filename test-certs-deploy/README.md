# Test Certificate Deployment

This directory contains a test pod that exercises every kcert-checker alert tier in a local kind cluster. It generates real X.509 certificates with different expiry windows so you can verify all alert rules are working without waiting for real certs to expire.

---

## Files

| File                   | Purpose                                                                              |
| ---------------------- | ------------------------------------------------------------------------------------ |
| `kcert-test-pod.yaml`  | **Use this.** Multi-container pod covering all 5 alert tiers + 2 healthy containers. |
| `_kcert-test-old.yaml` | Old placeholder pod with fake cert content — kept for reference only.                |

---

## Alert coverage

| Container              | Cert lifetime        | Expected alert                                  |
| ---------------------- | -------------------- | ----------------------------------------------- |
| `expired-container`    | backdated to Sep 1–2 | `KCertCertificateExpired` (critical)            |
| `critical-container`   | 5 days               | `KCertCertificateExpiresWithin7Days` (critical) |
| `warn30-container`     | 20 days              | `KCertCertificateExpiresWithin30Days` (warning) |
| `warn60-container`     | 45 days              | `KCertCertificateExpiresWithin60Days` (warning) |
| `warn90-container`     | 75 days              | `KCertCertificateExpiresWithin90Days` (warning) |
| `healthy180-container` | 180 days             | **No alert**                                    |
| `healthy365-container` | 365 days             | **No alert**                                    |

Certificates are generated at runtime by an `initContainer` using `openssl` in Alpine, so the pod requires internet access to pull `alpine:3.22`.

---

## How to deploy

### 1. Deploy the test pod

```bash
# Delete any previous version and apply fresh
kubectl delete pod kcert-test -n monitoring --ignore-not-found
kubectl apply -f test-certs-deploy/kcert-test-pod.yaml

# Wait for the initContainer to finish generating certs and all containers to start
kubectl wait --for=condition=Ready pod/kcert-test -n monitoring --timeout=120s
```

### 2. Verify the certs were generated

```bash
# Check initContainer logs to confirm all certs were generated
kubectl logs kcert-test -n monitoring -c cert-generator

# Spot-check a cert inside a running container
kubectl exec -n monitoring kcert-test -c expired-container \
  -- openssl x509 -in /etc/certs/server.crt -noout -subject -dates

kubectl exec -n monitoring kcert-test -c critical-container \
  -- openssl x509 -in /etc/certs/server.crt -noout -subject -dates

kubectl exec -n monitoring kcert-test -c warn90-container \
  -- openssl x509 -in /var/run/secrets/tls/tls.crt -noout -subject -dates
```

### 3. Trigger an immediate kcert-checker scan

kcert-checker scans on startup then every 24 hours. Restart it to force an immediate scan after deploying the test pod:

```bash
kubectl rollout restart deployment/kcert-checker -n monitoring
kubectl rollout status deployment/kcert-checker -n monitoring

# Watch the logs to confirm certs from kcert-test were found
kubectl logs -n monitoring deployment/kcert-checker | grep "Certificate found"
```

```bash
# If you want to verify kcert-test's certificates sooner, you can temporarily reduce the interval to 1 sec. Update the ConfigMap and restart the pod:

kubectl patch configmap kcert-checker-config -n monitoring \
  --type=merge \
  -p '{"data":{"config.yaml":"kubernetes:\n  mode: \"auto\"\n\nscanner:\n  namespaces: all\n  containers:\n    include_init_containers: false\n  certificates:\n    paths:\n      - /etc/ssl/postfix/tls.crt\n      - /etc/tls/tls.crt\n      - /etc/certs/tls.crt\n      - /var/run/secrets/tls/tls.crt\n\nmetrics:\n  enabled: true\n  listen_address: \"0.0.0.0:8080\"\n  path: \"/metrics\"\n\nscan:\n  interval_seconds: 10\n"}}'

  kubectl rollout restart deployment/kcert-checker -n monitoring
```

### 4. Check metrics

```bash
kubectl port-forward -n monitoring svc/kcert-checker 9091:8080

# In another terminal — filter by the test pod
curl -s http://localhost:9091/metrics | grep 'kcert_certificate_expiry.*kcert-test'
curl http://localhost:9091/metrics | grep kcert

kubectl get events -n monitoring --sort-by='.lastTimestamp' | grep kcert

```

Expected output (days column will vary):

```
kcert_certificate_expiry_timestamp_seconds{container="expired-container",...}   1.757e+09
kcert_certificate_expiry_timestamp_seconds{container="critical-container",...}  1.758e+09
...
```

### 5. Check Prometheus alerts

```bash
kubectl port-forward -n monitoring svc/prometheus-server 9092:80

# In another terminal — currently firing alerts
curl -s "http://localhost:9092/api/v1/alerts" | python3 -c "
import sys,json
d=json.load(sys.stdin)
alerts=d['data']['alerts']
if not alerts: print('No alerts firing (may still be in pending window)')
for a in alerts:
    print(f\"[{a['state']}] {a['labels'].get('alertname')} severity={a['labels'].get('severity')}\")
    print(f\"  {a['annotations'].get('description','')}\")
"
```

Alert rules have `for: 10m` — so a freshly deployed test pod will show alerts as `pending` for up to 10 minutes before transitioning to `firing`.

---

## Checking in the Prometheus UI

```bash
kubectl port-forward -n monitoring svc/prometheus-server 9090:80
# Open http://localhost:9090
```

- **Targets page**: Status → Targets → look for `job=kcert-checker` with state `UP`
- **Graph page** — useful PromQL queries:

  ```
  # Days until expiry for all certs
  (kcert_certificate_expiry_timestamp_seconds - time()) / 86400

  # Only certs from the test pod
  (kcert_certificate_expiry_timestamp_seconds{pod="kcert-test"} - time()) / 86400

  # Certs expiring within 7 days
  (kcert_certificate_expiry_timestamp_seconds - time()) / 86400 < 7
  ```

- **Alerts page**: click "Alerts" → expand `kcert-certificate-expiry` group

---

## Cleanup

```bash
kubectl delete pod kcert-test -n monitoring
```

---

## Troubleshooting

**Pod stuck in `Init:0/1`**
The `cert-generator` initContainer is still running `apk add openssl`. Check logs:

```bash
kubectl logs kcert-test -n monitoring -c cert-generator
```

**No metrics for kcert-test after restarting kcert-checker**
Check that kcert-checker has `pods/exec` permission:

```bash
kubectl auth can-i create pods/exec \
  --as=system:serviceaccount:monitoring:kcert-checker -n monitoring
```

**Alerts stuck in `pending` forever**
The `for: 10m` window requires the condition to hold for 10 continuous minutes. Verify the kcert-checker scrape target is `UP` in Prometheus and that the cert expiry metric is present.

---

### Others

```bash
# inspect configurations

kubectl -n monitoring describe pod kcert-test
kubectl get events -n monitoring --sort-by='.lastTimestamp' | grep kcert

kubectl -n monitoring get configmaps kcert-checker-config -o yaml
apiVersion: v1
data:
  config.yaml: |
    kubernetes:
      mode: "auto"

    scanner:
      namespaces: all
      containers:
        include_init_containers: false
      certificates:
        paths:
          - /etc/ssl/postfix/tls.crt
          - /etc/tls/tls.crt
          - /etc/certs/tls.crt
          - /var/run/secrets/tls/tls.crt

    metrics:
      enabled: true
      listen_address: "0.0.0.0:8080"
      path: "/metrics"

    scan:
      interval_seconds: 10
kind: ConfigMap
metadata:
  creationTimestamp: "2026-09-11T19:04:11Z"
  name: kcert-checker-config
  namespace: monitoring
  resourceVersion: "15814"
  uid: c855e863-20d9-41ff-b90f-4133d04ad2be
---
/etc/ssl/postfix
/var/run/secrets/tls
/etc/tls
/etc/certs

kubectl -n monitoring get pods kcert-test

kubectl -n monitoring exec kcert-test -- cat /etc/tls/tls.crt

kubectl get pod kcert-test -n monitoring \
  -o jsonpath='{.spec.containers[*].name}'

kubectl delete pod kcert-test -n monitoring --ignore-not-found
kubectl apply -f test-certs-deploy/kcert-test-pod.yaml
kubectl wait --for=condition=Ready pod/kcert-test -n monitoring --timeout=90s
kubectl rollout restart deployment/kcert-checker -n monitoring

expired-container
/etc/certs/server.crt

kubectl -n monitoring exec kcert-test \
  -c expired-container \
  -- cat /etc/certs/server.crt \
  | openssl x509 -noout -subject -issuer -dates

critical-container
/etc/certs/server.crt
/etc/certs/ca.crt

kubectl -n monitoring exec kcert-test \
  -c critical-container \
  -- cat /etc/certs/ca.crt \
  | openssl x509 -noout -subject -issuer -dates

kubectl -n monitoring exec kcert-test \
  -c critical-container \
  -- cat /etc/certs/server.crt \
  | openssl x509 -noout -subject -issuer -dates

warn30-container
/etc/tls/tls.crt
/etc/tls/ca.crt

kubectl -n monitoring exec kcert-test \
  -c warn30-container \
  -- cat /etc/tls/tls.crt \
  | openssl x509 -noout -subject -issuer -dates

kubectl -n monitoring exec kcert-test \
  -c warn30-container \
  -- cat /etc/tls/tls.crt \
  | openssl x509 -noout -subject -issuer -dates

warn60-container
/var/run/secrets/tls/tls.crt

kubectl -n monitoring exec kcert-test \
  -c warn60-container \
  -- cat /var/run/secrets/tls/tls.crt \
  | openssl x509 -noout -subject -issuer -dates

warn90-container
/var/run/secrets/tls/tls.crt
/var/run/secrets/tls/ca.crt

kubectl -n monitoring exec kcert-test \
  -c warn90-container \
  -- cat /var/run/secrets/tls/ca.crt \
  | openssl x509 -noout -subject -issuer -dates

kubectl -n monitoring exec kcert-test \
  -c warn90-container \
  -- cat /var/run/secrets/tls/tls.crt \
  | openssl x509 -noout -subject -issuer -dates

healthy180-container
/etc/ssl/postfix/tls.crt
/etc/ssl/postfix/ca.crt

kubectl -n monitoring exec kcert-test \
  -c healthy180-container \
  -- cat /etc/ssl/postfix/ca.crt \
  | openssl x509 -noout -subject -issuer -dates

kubectl -n monitoring exec kcert-test \
  -c healthy180-container \
  -- cat /etc/ssl/postfix/tls.crt \
  | openssl x509 -noout -subject -issuer -dates

healthy365-container
/etc/ssl/postfix/tls.crt
/etc/ssl/postfix/ca.crt


kubectl -n monitoring exec kcert-test \
  -c healthy365-container \
  -- cat /etc/ssl/postfix/ca.crt \
  | openssl x509 -noout -subject -issuer -dates

kubectl -n monitoring exec kcert-test \
  -c healthy365-container \
  -- cat /etc/ssl/postfix/tls.crt \
  | openssl x509 -noout -subject -issuer -dates

```
