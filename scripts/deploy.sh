#!/usr/bin/env bash
# deploy.sh — build the kcert-checker image, load it into the kind cluster,
# apply all manifests under deploy/, and verify the rollout.
#
# Usage:
#   ./scripts/deploy.sh                     # build + deploy backend only
#   ./scripts/deploy.sh --with-ui           # also build + deploy the Next.js UI
#   ./scripts/deploy.sh --skip-build        # skip docker build (re-deploy existing image)
#
# Prerequisites:
#   - Docker running
#   - kind cluster "cka" running  (make sure: kind get clusters)
#   - kubectl context set to kind-cka

set -euo pipefail

# ── config ────────────────────────────────────────────────────────────────────
CLUSTER="cka"
PLATFORM="linux/arm64"
BACKEND_IMAGE="kcert-checker:local" # "sirajudheenam/kcert-checker:latest"
UI_IMAGE="kcert-checker-ui:local"  # "sirajudheenam/kcert-checker-ui:latest"
NAMESPACE="monitoring"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="$REPO_ROOT/deploy"
UI_DIR="$REPO_ROOT/ui"

# ── resolve tool paths (macOS Docker Desktop + Homebrew) ─────────────────────
export PATH="/Applications/Docker.app/Contents/Resources/bin:/opt/homebrew/bin:$PATH"

DOCKER="$(command -v docker   || { echo "ERROR: docker not found"; exit 1; })"
KIND="$(command -v kind     || { echo "ERROR: kind not found";   exit 1; })"
KUBECTL="$(command -v kubectl || { echo "ERROR: kubectl not found"; exit 1; })"

# ── flags ─────────────────────────────────────────────────────────────────────
WITH_UI=true
SKIP_BUILD=true
for arg in "$@"; do
  case "$arg" in
    --with-ui)    WITH_UI=true ;;
    --skip-build) SKIP_BUILD=true ;;
    *) echo "Unknown flag: $arg"; exit 1 ;;
  esac
done

# ── pre-flight checks ─────────────────────────────────────────────────────────
echo "==> Checking kind cluster '$CLUSTER'..."
if ! "$KIND" get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  echo "ERROR: kind cluster '$CLUSTER' not found. Run: kind create cluster --name $CLUSTER"
  exit 1
fi

# Ensure kubectl context matches the cluster we're deploying to.
"$KUBECTL" config use-context "kind-$CLUSTER" >/dev/null
echo "    context: kind-$CLUSTER"
echo "    nodes:   $("$KUBECTL" get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ')"

# ── build + load backend image ────────────────────────────────────────────────
if [ "$SKIP_BUILD" = false ]; then
  echo ""
  echo "==> Building backend image: $BACKEND_IMAGE (platform $PLATFORM)..."
  "$DOCKER" buildx build \
    --platform "$PLATFORM" \
    -t "$BACKEND_IMAGE" \
    "$REPO_ROOT"
fi

echo ""
echo "==> Loading backend image into kind cluster '$CLUSTER'..."
"$KIND" load docker-image "$BACKEND_IMAGE" --name="$CLUSTER"
echo "    image loaded:"
"$DOCKER" exec "${CLUSTER}-control-plane" crictl images 2>/dev/null \
  | grep "kcert-checker" | awk '{printf "    %-45s %s\n", $1":"$2, $3}' || true

# ── build + load UI image (optional) ─────────────────────────────────────────
if [ "$WITH_UI" = true ]; then
  if [ "$SKIP_BUILD" = false ]; then
    echo ""
    echo "==> Building UI image: $UI_IMAGE (platform $PLATFORM)..."
    "$DOCKER" buildx build \
      --platform "$PLATFORM" \
      -t "$UI_IMAGE" \
      "$UI_DIR"
  fi
  echo ""
  echo "==> Loading UI image into kind cluster '$CLUSTER'..."
  "$KIND" load docker-image "$UI_IMAGE" --name="$CLUSTER"
fi

# ── apply manifests ───────────────────────────────────────────────────────────
echo ""
echo "==> Applying manifests in $DEPLOY_DIR ..."

# Apply in dependency order: namespace → rbac → config → workloads.
for manifest in \
  namespace.yaml \
  serviceaccount.yaml \
  clusterrole.yaml \
  clusterrolebinding.yaml \
  configmap.yaml \
  service.yaml \
  deployment.yaml; do
  file="$DEPLOY_DIR/$manifest"
  if [ -f "$file" ]; then
    echo "    kubectl apply -f $manifest"
    "$KUBECTL" apply -f "$file"
  fi
done

# Skip servicemonitor.yaml unless the CRD is installed (Prometheus Operator).
if "$KUBECTL" get crd servicemonitors.monitoring.coreos.com >/dev/null 2>&1; then
  echo "    kubectl apply -f servicemonitor.yaml (ServiceMonitor CRD found)"
  "$KUBECTL" apply -f "$DEPLOY_DIR/servicemonitor.yaml"
else
  echo "    skipping servicemonitor.yaml (monitoring.coreos.com CRD not installed)"
fi

# ── rollout ───────────────────────────────────────────────────────────────────
echo ""
echo "==> Restarting deployment to pick up the new image..."
"$KUBECTL" rollout restart deployment/kcert-checker -n "$NAMESPACE"

echo ""
echo "==> Waiting for rollout to complete (timeout 120s)..."
"$KUBECTL" rollout status deployment/kcert-checker -n "$NAMESPACE" --timeout=120s

if [ "$WITH_UI" = true ]; then
  echo ""
  echo "==> Waiting for UI rollout to complete (timeout 60s)..."
  "$KUBECTL" rollout status deployment/kcert-checker-ui -n "$NAMESPACE" --timeout=60s 2>/dev/null || true
fi

# ── result ────────────────────────────────────────────────────────────────────
echo ""
echo "==> Pods in namespace $NAMESPACE:"
"$KUBECTL" get pods -n "$NAMESPACE" -l app=kcert-checker -o wide
if [ "$WITH_UI" = true ]; then
  "$KUBECTL" get pods -n "$NAMESPACE" -l app=kcert-checker-ui -o wide
fi

echo ""
echo "==> Recent logs from kcert-checker:"
"$KUBECTL" logs -n "$NAMESPACE" deployment/kcert-checker --tail=20 2>/dev/null || true

echo ""
echo "==> Endpoints:"
echo "    Backend (metrics/healthz/readyz/ui/api):"
echo "      kubectl port-forward svc/kcert-checker 8080:8080 -n $NAMESPACE"
if [ "$WITH_UI" = true ]; then
  echo "    Next.js UI:"
  echo "      kubectl port-forward svc/kcert-checker-ui 3000:3000 -n $NAMESPACE"
fi
echo ""
echo "Done."
