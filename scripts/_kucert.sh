#!/usr/bin/env bash

set -uo pipefail

# ============================================================
# Kubernetes Certificate Scanner
#
# Usage:
#   kcert
#   kcert 30
#   kcert 60
#   kcert 90
#
# Default threshold: 90 days
# ============================================================

THRESHOLD_DAYS="${1:-90}"
THRESHOLD_SECONDS=$((THRESHOLD_DAYS * 86400))

TMP_DIR="$(mktemp -d)"

trap 'rm -rf "$TMP_DIR"' EXIT

echo
echo "============================================================"
echo " Kubernetes Certificate Scanner"
echo "============================================================"
echo " Threshold: certificates expiring within ${THRESHOLD_DAYS} days"
echo

# ------------------------------------------------------------
# Check dependencies
# ------------------------------------------------------------

if ! command -v u8s >/dev/null 2>&1; then
    echo "ERROR: u8s not found"
    exit 1
fi

# if ! command -v kubectl >/dev/null 2>&1; then
#     echo "ERROR: kubectl not found"
#     exit 1
# fi

if ! command -v openssl >/dev/null 2>&1; then
    echo "ERROR: openssl not found"
    exit 1
fi

# ------------------------------------------------------------
# Check Kubernetes connection
# ------------------------------------------------------------

if ! u8s kubectl cluster-info >/dev/null 2>&1; then
    echo "ERROR: Cannot connect to Kubernetes cluster"
    exit 1
fi

CURRENT_CONTEXT=$(u8s kubectl config current-context)

echo "Context: $CURRENT_CONTEXT"
echo

NOW=$(date +%s)

FOUND=0
EXPIRING=0
EXPIRED=0

# ============================================================
# PART 1
# Kubernetes Secrets containing certificates
# ============================================================

echo
echo "------------------------------------------------------------"
echo "1. Kubernetes Secrets"
echo "------------------------------------------------------------"
echo

SECRET_LIST=$(u8s kubectl get secrets -A \
    -o jsonpath='{range .items[*]}{.metadata.namespace}{"|"}{.metadata.name}{"\n"}{end}' \
    2>/dev/null)

while IFS='|' read -r NAMESPACE SECRET; do

    [[ -z "$NAMESPACE" ]] && continue
    [[ -z "$SECRET" ]] && continue

    SECRET_JSON=$(u8s kubectl get secret "$SECRET" \
        -n "$NAMESPACE" \
        -o json 2>/dev/null) || continue

    # Get all keys from the secret
    KEYS=$(echo "$SECRET_JSON" |
        jq -r '.data // {} | keys[]' 2>/dev/null) || continue

    while read -r KEY; do

        [[ -z "$KEY" ]] && continue

        # Only inspect likely certificate files
        if [[ ! "$KEY" =~ (crt|pem|cert|certificate)$ ]]; then
            continue
        fi

        CERT_DATA=$(echo "$SECRET_JSON" |
            jq -r --arg key "$KEY" '.data[$key] // empty' |
            base64 --decode 2>/dev/null) || continue

        [[ -z "$CERT_DATA" ]] && continue

        if ! echo "$CERT_DATA" | openssl x509 -noout >/dev/null 2>&1; then
            continue
        fi

        FOUND=$((FOUND + 1))

        SUBJECT=$(echo "$CERT_DATA" |
            openssl x509 -noout -subject 2>/dev/null |
            sed 's/^subject=//')

        ISSUER=$(echo "$CERT_DATA" |
            openssl x509 -noout -issuer 2>/dev/null |
            sed 's/^issuer=//')

        END_DATE=$(echo "$CERT_DATA" |
            openssl x509 -noout -enddate 2>/dev/null |
            cut -d= -f2)

        END_EPOCH=$(date -j -f "%b %e %H:%M:%S %Y %Z" \
            "$END_DATE" "+%s" 2>/dev/null || echo 0)

        if [[ "$END_EPOCH" == "0" ]]; then
            continue
        fi

        REMAINING=$((END_EPOCH - NOW))
        DAYS=$((REMAINING / 86400))

        if (( REMAINING < 0 )); then
            STATUS="🔴 EXPIRED"
            EXPIRED=$((EXPIRED + 1))

        elif (( REMAINING <= THRESHOLD_SECONDS )); then

            if (( DAYS <= 30 )); then
                STATUS="🔴 CRITICAL"
            elif (( DAYS <= 60 )); then
                STATUS="🟠 WARNING"
            else
                STATUS="🟡 NOTICE"
            fi

            EXPIRING=$((EXPIRING + 1))

        else
            STATUS="🟢 OK"
        fi

        echo "Namespace : $NAMESPACE"
        echo "Secret    : $SECRET"
        echo "Key       : $KEY"
        echo "Subject   : $SUBJECT"
        echo "Issuer    : $ISSUER"
        echo "Expires   : $END_DATE"
        echo "Remaining : $DAYS days"
        echo "Status    : $STATUS"
        echo

    done <<< "$KEYS"

done <<< "$SECRET_LIST"


# ============================================================
# PART 2
# Certificates mounted inside containers
# ============================================================

echo
echo "------------------------------------------------------------"
echo "2. Certificates inside containers"
echo "------------------------------------------------------------"
echo

# ------------------------------------------------------------
# Known certificate locations
#
# Add your organization's paths here.
# ------------------------------------------------------------

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

PODS=$(u8s kubectl get pods -A \
    -o jsonpath='{range .items[*]}{.metadata.namespace}{"|"}{.metadata.name}{"\n"}{end}' \
    2>/dev/null)

# echo "PODS: $PODS"

while IFS='|' read -r NAMESPACE POD; do
    # echo "NAMESPACE: $NAMESPACE : POD: $POD"
    [[ -z "$NAMESPACE" ]] && continue
    [[ -z "$POD" ]] && continue

    CONTAINERS=$(u8s kubectl get pod "$POD" \
        -n "$NAMESPACE" \
        -o jsonpath='{range .spec.containers[*]}{.name}{"\n"}{end}' \
        2>/dev/null)

    # echo "CONTAINERS: $CONTAINERS"

    while read -r CONTAINER; do
        # echo "CONTAINER: $CONTAINER"

        [[ -z "$CONTAINER" ]] && continue

        # echo "CERT_PATHS[@]: ${CERT_PATHS[@]}"

        for CERT_PATH in "${CERT_PATHS[@]}"; do

            # echo "CERT_PATH: $CERT_PATH"

            # Check whether the certificate exists
            if ! u8s kubectl exec \
                -n "$NAMESPACE" \
                "$POD" \
                -c "$CONTAINER" \
                -- test -f "$CERT_PATH" \
                >/dev/null 2>&1; then
                continue
            fi

            # Extract certificate
            CERT_CONTENT=$(u8s kubectl exec \
                -n "$NAMESPACE" \
                "$POD" \
                -c "$CONTAINER" \
                -- cat "$CERT_PATH" \
                2>/dev/null) || continue

            # echo "CERT_CONTENT: $CERT_CONTENT"

            # Is it actually a certificate?
            if ! echo "$CERT_CONTENT" |
                openssl x509 -noout >/dev/null 2>&1; then
                continue
            fi

            FOUND=$((FOUND + 1))

            # echo "FOUND: $FOUND"

            SUBJECT=$(echo "$CERT_CONTENT" |
                openssl x509 -noout -subject 2>/dev/null |
                sed 's/^subject=//')

            # echo "SUBJECT: $SUBJECT"

            ISSUER=$(echo "$CERT_CONTENT" |
                openssl x509 -noout -issuer 2>/dev/null |
                sed 's/^issuer=//')

            # echo "ISSUER: $ISSUER"

            END_DATE=$(echo "$CERT_CONTENT" |
                openssl x509 -noout -enddate 2>/dev/null |
                cut -d= -f2)

            # echo "END_DATE: $END_DATE"

            END_EPOCH=$(date -j -f "%b %e %H:%M:%S %Y %Z" \
                "$END_DATE" "+%s" 2>/dev/null || echo 0)

            # echo "END_EPOCH: $END_EPOCH"

            if [[ "$END_EPOCH" == "0" ]]; then
                continue
            fi

            REMAINING=$((END_EPOCH - NOW))
            # echo "REMAINING: $REMAINING"

            DAYS=$((REMAINING / 86400))
            # echo "DAYS: $DAYS"

            if (( REMAINING < 0 )); then
                STATUS="🔴 EXPIRED"
                EXPIRED=$((EXPIRED + 1))

            elif (( REMAINING <= THRESHOLD_SECONDS )); then

                if (( DAYS <= 30 )); then
                    STATUS="🔴 CRITICAL"
                elif (( DAYS <= 60 )); then
                    STATUS="🟠 WARNING"
                else
                    STATUS="🟡 NOTICE"
                fi

                EXPIRING=$((EXPIRING + 1))

            else
                STATUS="🟢 OK"
            fi

            echo "Namespace : $NAMESPACE"
            echo "Pod       : $POD"
            echo "Container : $CONTAINER"
            echo "Path      : $CERT_PATH"
            echo "Subject   : $SUBJECT"
            echo "Issuer    : $ISSUER"
            echo "Expires   : $END_DATE"
            echo "Remaining : $DAYS days"
            echo "Status    : $STATUS"
            echo

        done

    done <<< "$CONTAINERS"

done <<< "$PODS"


# ============================================================
# SUMMARY
# ============================================================

echo
echo "============================================================"
echo " Summary"
echo "============================================================"
echo
echo "Certificates found : $FOUND"
echo "Expiring soon       : $EXPIRING"
echo "Expired             : $EXPIRED"
echo