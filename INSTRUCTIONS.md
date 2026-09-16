```bash
CERT_PATHS=(
    "/etc/ssl/postfix/tls.crt"
    "/etc/ssl/postfix/ca.crt"
    "/etc/ssl/certs/tls.crt"
    "/etc/tls/tls.crt"
    "/etc/tls/ca.crt"
    "/etc/certs/tls.crt"
    "/etc/certs/server.crt"
    "/etc/certs/ca.crt"
    "/tls/tls.crt"
    "/tls/server.crt"
    "/var/run/secrets/tls/tls.crt"
    "/var/run/secrets/tls/ca.crt"
)
PODS=$(kubectl get pods -A \
    -o jsonpath='{range .items[*]}{.metadata.namespace}{"|"}{.metadata.name}{"\n"}{end}' \
    2>/dev/null)

echo $PODS

CONTAINERS=$(kubectl get pod "$POD" \
    -n "$NAMESPACE" \
    -o jsonpath='{range .spec.containers[*]}{.name}{"\n"}{end}' \
    2>/dev/null)

kubectl get pod postfixin-edge-0 \
    -n postfixin \
    -o jsonpath='{range .spec.containers[*]}{.name}{"\n"}{end}' \
    2>/dev/null


CONTAINERS=$(kubectl get pod postfixin-edge-0 \
    -n postfixin \
    -o jsonpath='{range .spec.containers[*]}{.name}{"\n"}{end}' \
    2>/dev/null)

# ----

kubectl exec \
    -n "$NAMESPACE" \
    "$POD" \
    -c "$CONTAINER" \
    -- test -f "$CERT_PATH"

kubectl exec \
    -n postfixin \
    postfixin-edge-0 \
    -c postfixin-edge \
    -- test -f "/etc/ssl/postfix/tls.crt"

kubectl exec \
    -n postfixin \
    postfixin-edge-0 \
    -c postfixin-edge \
    -- openssl x509 -text -in "/etc/ssl/postfix/tls.crt"
```
