```bash
CERT_PATHS=(
    "/etc/ssl/postfix/*"
    "/etc/ssl/certs/*"
    "/etc/tls/*"
    "/etc/certs/*"
    "/tls/*"
    "/var/run/secrets/tls/*"

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
