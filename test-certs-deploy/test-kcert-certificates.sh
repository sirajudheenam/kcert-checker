#!/usr/bin/env bash

# =============================================================================
# KCert Test Certificate Scanner
#
# Portable across:
#   - macOS
#   - Linux
#
# Requirements:
#   - kubectl
#   - openssl
#
# Usage:
#   ./test-kcert-certificates.sh
# =============================================================================

set -u

# -----------------------------------------------------------------------------
# Configuration
# -----------------------------------------------------------------------------

NAMESPACE="monitoring"
POD="kcert-test"

# -----------------------------------------------------------------------------
# Colors
# -----------------------------------------------------------------------------

if [ -t 1 ]; then
    RED='\033[0;31m'
    YELLOW='\033[1;33m'
    GREEN='\033[0;32m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    NC='\033[0m'
else
    RED=''
    YELLOW=''
    GREEN=''
    CYAN=''
    BOLD=''
    NC=''
fi

# -----------------------------------------------------------------------------
# Counters
# -----------------------------------------------------------------------------

TOTAL=0
SUCCESS=0
FAILED=0
EXPIRED=0
CRITICAL=0
WARNING=0
HEALTHY=0

# -----------------------------------------------------------------------------
# Check required commands
# -----------------------------------------------------------------------------

check_command() {
    local command_name="$1"

    if ! command -v "$command_name" >/dev/null 2>&1; then
        echo "ERROR: '$command_name' is required but was not found."
        exit 1
    fi
}

check_command kubectl
check_command openssl

# -----------------------------------------------------------------------------
# Check certificate
# -----------------------------------------------------------------------------

check_certificate() {
    local container="$1"
    local path="$2"

    TOTAL=$((TOTAL + 1))

    echo
    echo "------------------------------------------------------------"
    echo -e "${BOLD}Container:${NC} $container"
    echo -e "${BOLD}Path:${NC}      $path"
    echo "------------------------------------------------------------"

    # -------------------------------------------------------------------------
    # Read certificate from container
    # -------------------------------------------------------------------------

    cert_output=$(
        kubectl -n "$NAMESPACE" exec "$POD" \
            -c "$container" \
            -- cat "$path" 2>&1
    )

    kubectl_status=$?

    if [ "$kubectl_status" -ne 0 ]; then
        echo -e "${RED}RESULT: FAILED${NC}"
        echo "$cert_output"

        FAILED=$((FAILED + 1))
        return
    fi

    # -------------------------------------------------------------------------
    # Parse certificate
    # -------------------------------------------------------------------------

    cert_info=$(
        printf '%s\n' "$cert_output" |
            openssl x509 -noout -subject -issuer -dates 2>&1
    )

    openssl_status=$?

    if [ "$openssl_status" -ne 0 ]; then
        echo -e "${RED}RESULT: INVALID CERTIFICATE${NC}"
        echo "$cert_info"

        FAILED=$((FAILED + 1))
        return
    fi

    SUCCESS=$((SUCCESS + 1))

    # -------------------------------------------------------------------------
    # Extract certificate fields
    # -------------------------------------------------------------------------

    subject=$(printf '%s\n' "$cert_info" |
        sed -n 's/^subject=//p')

    issuer=$(printf '%s\n' "$cert_info" |
        sed -n 's/^issuer=//p')

    not_before=$(printf '%s\n' "$cert_info" |
        sed -n 's/^notBefore=//p')

    not_after=$(printf '%s\n' "$cert_info" |
        sed -n 's/^notAfter=//p')

    echo -e "${BOLD}Subject:${NC}    $subject"
    echo -e "${BOLD}Issuer:${NC}     $issuer"
    echo -e "${BOLD}Not Before:${NC} $not_before"
    echo -e "${BOLD}Not After:${NC}  $not_after"

    # -------------------------------------------------------------------------
    # Get expiry timestamp
    #
    # OpenSSL handles the certificate date parsing, so we don't depend on:
    #
    #   macOS: date -j -f
    #   Linux: date -d
    #
    # This makes the script portable.
    # -------------------------------------------------------------------------

    expiry_epoch=$(
        printf '%s\n' "$cert_output" |
            openssl x509 -noout -enddate |
            cut -d= -f2 |
            xargs -I{} date -d "{}" +%s 2>/dev/null
    )

    # -------------------------------------------------------------------------
    # macOS fallback
    #
    # macOS does not support `date -d`.
    # Use Python if available.
    # -------------------------------------------------------------------------

    if [ -z "$expiry_epoch" ]; then
        expiry_epoch=$(
            printf '%s\n' "$not_after" |
                python3 -c '
import sys
from datetime import datetime, timezone

value = sys.stdin.read().strip()

try:
    dt = datetime.strptime(value, "%b %d %H:%M:%S %Y %Z")
    print(int(dt.replace(tzinfo=timezone.utc).timestamp()))
except Exception:
    sys.exit(1)
' 2>/dev/null || true
        )
    fi

    # -------------------------------------------------------------------------
    # Last fallback
    #
    # macOS date parser.
    # -------------------------------------------------------------------------

    if [ -z "$expiry_epoch" ]; then
        expiry_epoch=$(
            date -j -f "%b %d %T %Y %Z" \
                "$not_after" \
                "+%s" 2>/dev/null || true
        )
    fi

    if [ -z "$expiry_epoch" ]; then
        echo -e "${YELLOW}STATUS: Could not calculate remaining days${NC}"
        return
    fi

    # -------------------------------------------------------------------------
    # Calculate remaining time
    # -------------------------------------------------------------------------

    now_epoch=$(date "+%s")

    remaining_seconds=$((expiry_epoch - now_epoch))
    remaining_days=$((remaining_seconds / 86400))

    # -------------------------------------------------------------------------
    # Determine certificate status
    # -------------------------------------------------------------------------

    if [ "$remaining_seconds" -le 0 ]; then

        echo -e "${RED}${BOLD}STATUS: EXPIRED${NC}"

        if [ "$remaining_days" -eq 0 ]; then
            echo -e "${RED}Expired today${NC}"
        else
            echo -e "${RED}Expired ${remaining_days#-} days ago${NC}"
        fi

        EXPIRED=$((EXPIRED + 1))

    elif [ "$remaining_days" -le 7 ]; then

        echo -e "${RED}${BOLD}STATUS: CRITICAL${NC}"
        echo -e "${RED}Expires in ${remaining_days} days${NC}"

        CRITICAL=$((CRITICAL + 1))

    elif [ "$remaining_days" -le 30 ]; then

        echo -e "${YELLOW}${BOLD}STATUS: WARNING (<= 30 days)${NC}"
        echo -e "${YELLOW}Expires in ${remaining_days} days${NC}"

        WARNING=$((WARNING + 1))

    elif [ "$remaining_days" -le 60 ]; then

        echo -e "${YELLOW}${BOLD}STATUS: WARNING (<= 60 days)${NC}"
        echo -e "${YELLOW}Expires in ${remaining_days} days${NC}"

        WARNING=$((WARNING + 1))

    elif [ "$remaining_days" -le 90 ]; then

        echo -e "${YELLOW}${BOLD}STATUS: WARNING (<= 90 days)${NC}"
        echo -e "${YELLOW}Expires in ${remaining_days} days${NC}"

        WARNING=$((WARNING + 1))

    else

        echo -e "${GREEN}${BOLD}STATUS: HEALTHY${NC}"
        echo -e "${GREEN}Expires in ${remaining_days} days${NC}"

        HEALTHY=$((HEALTHY + 1))

    fi
}

# =============================================================================
# Header
# =============================================================================

echo
echo "============================================================"
echo -e "${BOLD}KCert Test Certificate Scanner${NC}"
echo "============================================================"
echo "Namespace : $NAMESPACE"
echo "Pod       : $POD"
echo "Date      : $(date)"
echo "Host      : $(uname -s)"
echo "============================================================"

# =============================================================================
# expired-container
# =============================================================================

check_certificate \
    "expired-container" \
    "/etc/certs/server.crt"

# =============================================================================
# critical-container
# =============================================================================

check_certificate \
    "critical-container" \
    "/etc/certs/ca.crt"

check_certificate \
    "critical-container" \
    "/etc/certs/server.crt"

# =============================================================================
# warn30-container
# =============================================================================

check_certificate \
    "warn30-container" \
    "/etc/tls/tls.crt"

check_certificate \
    "warn30-container" \
    "/etc/tls/ca.crt"

# =============================================================================
# warn60-container
# =============================================================================

check_certificate \
    "warn60-container" \
    "/var/run/secrets/tls/tls.crt"

# =============================================================================
# warn90-container
# =============================================================================

check_certificate \
    "warn90-container" \
    "/var/run/secrets/tls/ca.crt"

check_certificate \
    "warn90-container" \
    "/var/run/secrets/tls/tls.crt"

# =============================================================================
# healthy180-container
# =============================================================================

check_certificate \
    "healthy180-container" \
    "/etc/ssl/postfix/ca.crt"

check_certificate \
    "healthy180-container" \
    "/etc/ssl/postfix/tls.crt"

# =============================================================================
# healthy365-container
# =============================================================================

check_certificate \
    "healthy365-container" \
    "/etc/ssl/postfix/ca.crt"

check_certificate \
    "healthy365-container" \
    "/etc/ssl/postfix/tls.crt"

# =============================================================================
# Summary
# =============================================================================

echo
echo
echo "============================================================"
echo -e "${BOLD}SUMMARY${NC}"
echo "============================================================"

echo "Total certificates checked : $TOTAL"
echo -e "Successful                 : ${GREEN}$SUCCESS${NC}"
echo -e "Failed                     : ${RED}$FAILED${NC}"
echo
echo -e "Expired                    : ${RED}$EXPIRED${NC}"
echo -e "Critical (<= 7 days)       : ${RED}$CRITICAL${NC}"
echo -e "Warning (<= 90 days)       : ${YELLOW}$WARNING${NC}"
echo -e "Healthy (> 90 days)        : ${GREEN}$HEALTHY${NC}"

echo "============================================================"

if [ "$FAILED" -eq 0 ]; then
    echo -e "${GREEN}${BOLD}All certificate checks completed successfully.${NC}"
else
    echo -e "${RED}${BOLD}Some certificate checks failed.${NC}"
fi

echo "============================================================"
echo