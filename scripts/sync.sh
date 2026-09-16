#!/usr/bin/env bash
# sync.sh — two-way sync between local machine and sam@studio.
#
# Usage:
#   ./scripts/sync.sh pull          # remote → local  (default when no arg given)
#   ./scripts/sync.sh push          # local  → remote
#   ./scripts/sync.sh watch         # pull once, then watch local dir and auto-push on change
#   ./scripts/sync.sh --dry-run pull|push   # preview without transferring
#
# The script always excludes build artefacts, node_modules, .next, and the
# compiled binary so you never overwrite a platform-specific build.
#
# Prerequisites (macOS):
#   brew install rsync fswatch   # fswatch only needed for `watch` mode

set -euo pipefail

# ── config ────────────────────────────────────────────────────────────────────
REMOTE_HOST="sam@studio"
REMOTE_DIR="/Users/sam/workdir/GoRepo/kcert-checker/"
LOCAL_DIR="$(cd "$(dirname "$0")/.." && pwd)/"   # repo root, trailing slash required by rsync

# ── rsync options ─────────────────────────────────────────────────────────────
# -a  archive mode (recursive, preserve symlinks/times/perms)
# -v  verbose (one line per file transferred)
# -z  compress in transit
# --delete  remove files on destination that no longer exist on source
RSYNC_OPTS=(
  -avz
  --delete
  --exclude='.git/'
  --exclude='bin/'
  --exclude='kcert-checker'       # compiled binary (platform-specific)
  --exclude='ui/node_modules/'
  --exclude='ui/.next/'
  --exclude='ui/out/'
  --exclude='*.key'
  --exclude='*.pem'
  --exclude='kubeconfig'
  --exclude='*.kubeconfig'
  --exclude='.DS_Store'
  --exclude='*.swp'
  --exclude='*.swo'
  --exclude='helm/*/charts/'
)

# ── helpers ───────────────────────────────────────────────────────────────────
RSYNC="$(command -v rsync || { echo "ERROR: rsync not found — brew install rsync"; exit 1; })"
DRY_RUN=false

usage() {
  echo "Usage: $0 [--dry-run] pull|push|watch"
  echo "  pull   — copy remote → local"
  echo "  push   — copy local  → remote"
  echo "  watch  — pull once, then watch local and auto-push on change (requires fswatch)"
  exit 1
}

do_pull() {
  echo "==> pull: ${REMOTE_HOST}:${REMOTE_DIR} → ${LOCAL_DIR}"
  "$RSYNC" "${RSYNC_OPTS[@]}" \
    $( $DRY_RUN && echo "--dry-run" ) \
    "${REMOTE_HOST}:${REMOTE_DIR}" \
    "${LOCAL_DIR}"
  echo "==> pull complete"
}

do_push() {
  echo "==> push: ${LOCAL_DIR} → ${REMOTE_HOST}:${REMOTE_DIR}"
  "$RSYNC" "${RSYNC_OPTS[@]}" \
    $( $DRY_RUN && echo "--dry-run" ) \
    "${LOCAL_DIR}" \
    "${REMOTE_HOST}:${REMOTE_DIR}"
  echo "==> push complete"
}

do_watch() {
  FSWATCH="$(command -v fswatch || { echo "ERROR: fswatch not found — brew install fswatch"; exit 1; })"

  # Initial pull to get latest remote state before we start editing.
  do_pull

  echo ""
  echo "==> Watching ${LOCAL_DIR} for changes — push on save (Ctrl-C to stop)..."
  echo "    (ignoring .git/, bin/, node_modules/, .next/)"

  "$FSWATCH" \
    --recursive \
    --exclude='\.git' \
    --exclude='/bin/' \
    --exclude='node_modules' \
    --exclude='\.next' \
    --exclude='\.DS_Store' \
    --exclude='\.swp$' \
    --latency 1 \
    "${LOCAL_DIR}" \
  | while read -r event; do
      echo "  changed: $event"
      do_push
    done
}

# ── argument parsing ──────────────────────────────────────────────────────────
if [[ $# -eq 0 ]]; then
  do_pull
  exit 0
fi

COMMAND=""
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    pull|push|watch) COMMAND="$arg" ;;
    *) echo "Unknown argument: $arg"; usage ;;
  esac
done

case "$COMMAND" in
  pull)  do_pull  ;;
  push)  do_push  ;;
  watch) do_watch ;;
  "")    do_pull  ;;
  *)     usage    ;;
esac
