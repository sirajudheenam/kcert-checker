# SESSION COMMANDS REFERENCE

````bash
# Commands executed during this session (Sept 15 2026)
# Covers: Next.js UI, graceful shutdown, output table split, --once flag,
#         demo secrets, integration test fix, Makefile improvements.
#
# All remote commands run on sam@studio via ssh/scp unless noted otherwise.
# Local machine = /Users/I072278/workdir/internal/I072278/kcert-checker
# Remote repo   = sam@studio:/Users/sam/workdir/GoRepo/kcert-checker

# ─────────────────────────────────────────────────────────────────────────────
# 1. NEXT.JS UI (ui/ folder at repo root)
# ─────────────────────────────────────────────────────────────────────────────

# Scaffold was already at /tmp/kcert-ui on local machine.
# Install dependencies and verify build locally:
cd /tmp/kcert-ui
npm install
npm run build

# Files written/edited locally in /tmp/kcert-ui/:
#   app/page.tsx          - server component, SSR fetches cert list
#   app/Dashboard.tsx     - client component, sort/filter/auto-refresh
#   app/layout.tsx        - dark-mode root layout
#   app/globals.css       - Tailwind base + scrollbar styles
#   app/types.ts          - Certificate TypeScript interface
#   app/api-client.ts     - fetchCertificates() using NEXT_PUBLIC_API_URL
#   app/status.ts         - STATUS_CONFIG, daysColor(), formatDays()
#   next.config.ts        - rewrites /api/* -> Go backend
#   .env.local.example    - documents NEXT_PUBLIC_API_URL=http://localhost:8080

# Create ui/ directory on remote and sync (excluding node_modules/.next):
ssh sam@studio 'mkdir -p /Users/sam/workdir/GoRepo/kcert-checker/ui'
rsync -av --exclude='.next' --exclude='node_modules' /tmp/kcert-ui/ sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/ui/

# Clean up stray files synced accidentally:
ssh sam@studio 'rm -rf /Users/sam/workdir/GoRepo/kcert-checker/ui/.git \
  /Users/sam/workdir/GoRepo/kcert-checker/ui/AGENTS.md \
  /Users/sam/workdir/GoRepo/kcert-checker/ui/CLAUDE.md'

# Write .gitignore for ui/:
ssh sam@studio 'cat > /Users/sam/workdir/GoRepo/kcert-checker/ui/.gitignore << "EOF"
node_modules/
.next/
out/
.env.local
.env.*.local
npm-debug.log*
EOF'

# Verify ui/ contents:
ssh sam@studio 'ls /Users/sam/workdir/GoRepo/kcert-checker/ui/'
ssh sam@studio 'ls /Users/sam/workdir/GoRepo/kcert-checker/ui/app/'

# ─────────────────────────────────────────────────────────────────────────────
# 2. GRACEFUL HTTP SERVER SHUTDOWN
# ─────────────────────────────────────────────────────────────────────────────

# Diagnose the "address already in use" error:
ssh sam@studio 'lsof -ti :8080'
ssh sam@studio 'ps -p 5418 -o pid,comm,args'
ssh sam@studio 'kill 5418'

# Fetch main.go from remote, edit locally, push back:
ssh sam@studio 'cat /Users/sam/workdir/GoRepo/kcert-checker/cmd/kcert-checker/main.go' > /tmp/remote_main.go
# (edited /tmp/remote_main.go to replace http.ListenAndServe with http.Server + Shutdown)
scp /tmp/remote_main.go sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/cmd/kcert-checker/main.go

# Key change: replaced bare http.ListenAndServe with:
#   srv := &http.Server{Addr: cfg.Metrics.ListenAddress, Handler: mux}
#   go srv.ListenAndServe()
#   go func() { <-ctx.Done(); srv.Shutdown(context.WithTimeout(5s)) }()

# Verify build and tests:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/Cellar/go/1.27.1/libexec/bin && \
  cd /Users/sam/workdir/GoRepo/kcert-checker && go build ./... && go test ./...'

# ─────────────────────────────────────────────────────────────────────────────
# 3. CONTAINER EXEC TIMEOUT FIX (scan getting stuck)
# ─────────────────────────────────────────────────────────────────────────────

# Inspect cluster to understand scale:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka get pods -A --no-headers | wc -l'

# Verified cert is readable via exec (proving scanner logic was correct):
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka exec -n monitoring kcert-test -c expired-container \
  -- cat /etc/certs/server.crt | head -5'

# Fetch scanner.go, add per-exec 5s timeout + parallel path scanning:
ssh sam@studio 'cat /Users/sam/workdir/GoRepo/kcert-checker/internal/scanner/scanner.go' > /tmp/scanner.go
# (edited /tmp/scanner.go: rewrote scanContainer to fan out goroutines per path
#  each with context.WithTimeout(ctx, 5*time.Second))
scp /tmp/scanner.go sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/internal/scanner/scanner.go

ssh sam@studio 'export PATH=$PATH:/opt/homebrew/Cellar/go/1.27.1/libexec/bin && \
  cd /Users/sam/workdir/GoRepo/kcert-checker && go build ./... && go test ./...'

# ─────────────────────────────────────────────────────────────────────────────
# 4. SPLIT TABLE OUTPUT (Secrets + Pod Containers as separate sections)
# ─────────────────────────────────────────────────────────────────────────────

# Write new output.go to /tmp then push:
# (writeTable now splits results by SourceType, prints two tabwriter sections)
scp /tmp/output.go sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/internal/output/output.go
scp /tmp/output_test.go sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/internal/output/output_test.go

ssh sam@studio 'export PATH=$PATH:/opt/homebrew/Cellar/go/1.27.1/libexec/bin && \
  cd /Users/sam/workdir/GoRepo/kcert-checker && go test ./internal/output/... -v'

# ─────────────────────────────────────────────────────────────────────────────
# 5. --once FLAG (one-shot scan, no daemon, no HTTP server)
# ─────────────────────────────────────────────────────────────────────────────

# Edited /tmp/remote_main.go to add:
#   once := flag.Bool("once", false, "Run a single scan, print results, and exit")
#   if cfg.Metrics.Enabled && !*once { ... start HTTP server ... }
#   if *once || *failOn != "" { os.Exit(code) }

scp /tmp/remote_main.go sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/cmd/kcert-checker/main.go

# Usage:
  ./kcert-checker --config config.yaml --output table --once
  ./kcert-checker --config config.yaml --output json --once

# ─────────────────────────────────────────────────────────────────────────────
# 6. SUMMARY LINE + IMPROVED EMPTY-STATE MESSAGES IN TABLE OUTPUT
# ─────────────────────────────────────────────────────────────────────────────

# Added to writeTable():
#   Total: N  |  OK: N  WARNING: N  CRITICAL: N  EXPIRED: N
# Changed "(none found)" to descriptive messages per section.

scp /tmp/output.go sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/internal/output/output.go
scp /tmp/output_test.go sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/internal/output/output_test.go

ssh sam@studio 'export PATH=$PATH:/opt/homebrew/Cellar/go/1.27.1/libexec/bin && \
  cd /Users/sam/workdir/GoRepo/kcert-checker && go test ./internal/output/... -v'

# ─────────────────────────────────────────────────────────────────────────────
# 7. DEMO SECRETS IN monitoring NAMESPACE
# ─────────────────────────────────────────────────────────────────────────────

# Inspect existing secrets to confirm no TLS certs present:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka get secrets -A --no-headers | \
  grep -v "kubernetes.io/service-account-token"'

# Check what secret data contains:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka get secrets -A -o json | python3 -c "
import json,sys,base64
data=json.load(sys.stdin)
for item in data[\"items\"]:
    ns=item[\"metadata\"][\"namespace\"]
    name=item[\"metadata\"][\"name\"]
    d=item.get(\"data\",{})
    for k,v in d.items():
        raw=base64.b64decode(v)
        if b\"BEGIN CERTIFICATE\" in raw:
            print(f\"CERT: {ns}/{name} key={k}\")
"'

# Generate 4 self-signed certs (CA, OK ~400d, WARNING ~20d, CRITICAL ~5d):
# Written to /tmp/gencerts.go locally, copied to remote:
scp /tmp/gencerts.go sam@studio:/tmp/gencerts.go
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/Cellar/go/1.27.1/libexec/bin && \
  go run /tmp/gencerts.go'
# Outputs: /tmp/ca.crt /tmp/tls-ok.crt /tmp/tls-warn.crt /tmp/tls-crit.crt

# Build secrets YAML with base64-encoded cert data and apply:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  CA=$(base64 -i /tmp/ca.crt | tr -d "\n") && \
  TLS_OK=$(base64 -i /tmp/tls-ok.crt | tr -d "\n") && \
  TLS_WARN=$(base64 -i /tmp/tls-warn.crt | tr -d "\n") && \
  TLS_CRIT=$(base64 -i /tmp/tls-crit.crt | tr -d "\n") && \
  cat > /tmp/monitoring-secrets.yaml << YAML
---
apiVersion: v1
kind: Secret
metadata:
  name: monitoring-ca
  namespace: monitoring
  labels:
    app: kcert-demo
type: Opaque
data:
  ca.crt: \${CA}
---
apiVersion: v1
kind: Secret
metadata:
  name: monitoring-api-tls
  namespace: monitoring
  labels:
    app: kcert-demo
type: Opaque
data:
  tls.crt: \${TLS_OK}
---
apiVersion: v1
kind: Secret
metadata:
  name: monitoring-grafana-tls
  namespace: monitoring
  labels:
    app: kcert-demo
type: Opaque
data:
  tls.crt: \${TLS_WARN}
---
apiVersion: v1
kind: Secret
metadata:
  name: monitoring-alertmanager-tls
  namespace: monitoring
  labels:
    app: kcert-demo
type: Opaque
data:
  tls.crt: \${TLS_CRIT}
YAML

kubectl --context kind-cka apply -f /tmp/monitoring-secrets.yaml'

# Save manifest to repo for re-apply:
ssh sam@studio 'cp /tmp/monitoring-secrets.yaml \
  /Users/sam/workdir/GoRepo/kcert-checker/test-certs-deploy/monitoring-demo-secrets.yaml'

# Verify secrets appear in scan output:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/Cellar/go/1.27.1/libexec/bin && \
  cd /Users/sam/workdir/GoRepo/kcert-checker && \
  ./kcert-checker --config config.yaml --output table --once 2>&1 | grep -v "^2026"'

# To re-apply demo secrets after cluster restart:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka apply -f \
  /Users/sam/workdir/GoRepo/kcert-checker/test-certs-deploy/monitoring-demo-secrets.yaml'

# ─────────────────────────────────────────────────────────────────────────────
# 8. README.md — TESTING SECTION ADDED
# ─────────────────────────────────────────────────────────────────────────────

ssh sam@studio 'cat /Users/sam/workdir/GoRepo/kcert-checker/README.md' > /tmp/README.md
# (edited /tmp/README.md to add ## Testing section covering unit, integration,
#  and manual e2e tests with demo secrets table)
scp /tmp/README.md sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/README.md

# ─────────────────────────────────────────────────────────────────────────────
# 9. INTEGRATION TEST FIX (TestPodCertsDiscovered EXPIRED assertion)
# ─────────────────────────────────────────────────────────────────────────────

# Root cause: 02-pod.yaml generated expired cert via openssl -startdate/-enddate
# which varies between OpenSSL versions; fallback produced DaysLeft=0 (CRITICAL
# not EXPIRED).

# Fix: generate a genuine past-dated cert in Go locally:
# (written to /tmp/gen_expired2.go)
go run /tmp/gen_expired2.go
# Outputs PEM with NotBefore=now-10d, NotAfter=now-2d

# Rewrote tests/integration/fixtures/02-pod.yaml:
#   - Added ConfigMap kcert-expired-cert with the pre-baked PEM
#   - vol-expired now uses configMap volume (not emptyDir)
#   - init container only generates critical/warning/healthy certs
#   - expired-container mounts the ConfigMap at /etc/certs

scp /tmp/02-pod.yaml sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/tests/integration/fixtures/02-pod.yaml

# ─────────────────────────────────────────────────────────────────────────────
# 10. MAKEFILE IMPROVEMENTS
# ─────────────────────────────────────────────────────────────────────────────

ssh sam@studio 'cat /Users/sam/workdir/GoRepo/kcert-checker/Makefile' > /tmp/Makefile
# Changes:
#   - GO/KIND/KUBECTL resolved via $(shell which ...) with fallback full paths
#   - export PATH includes Docker Desktop and Homebrew bin for sub-shells
#   - Fixed `make help` sed pattern (was broken on macOS BSD sed)

scp /tmp/Makefile sam@studio:/Users/sam/workdir/GoRepo/kcert-checker/Makefile

# ─────────────────────────────────────────────────────────────────────────────
# 11. FINAL VERIFICATION — ALL MAKE TARGETS
# ─────────────────────────────────────────────────────────────────────────────

ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && make help'
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && make build'
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && make check'
# ^ runs vet + unit tests across all 11 packages
# coverage: config 100% | filter 100% | graph 100% | health 100% |
#           kubernetes 95.7% | metrics 98.6% | output 100% | result 100% |
#           scanner 82.3% | ui 100% | cmd 9% (main() untestable as unit)

ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && make integration-test'
# ^ spins up kind cluster (k8s v1.37.0), applies fixtures,
#   runs 3 integration tests, tears down cluster
# Results: TestSecretsDiscovered PASS | TestPodCertsDiscovered PASS |
#          TestCertificateFingerprintUnique PASS

ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && make clean && make build'

# ─────────────────────────────────────────────────────────────────────────────
# 12. QUICK REFERENCE — DAILY USE COMMANDS
# ─────────────────────────────────────────────────────────────────────────────

# Build binary on remote:
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && \
  export PATH=$PATH:/opt/homebrew/Cellar/go/1.27.1/libexec/bin && \
  go build -o kcert-checker ./cmd/kcert-checker'

# One-shot scan (table):
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && \
  ./kcert-checker --config config.yaml --output table --once'

# One-shot scan (JSON):

ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && \
  ./kcert-checker --config config.yaml --output json --once'

# Daemon mode (HTTP server on :8080, scans every hour):
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && \
  ./kcert-checker --config config.yaml --output table &'

# Kill any stale kcert-checker process holding :8080:
ssh sam@studio 'kill $(lsof -ti :8080)'

# Run unit tests:
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && make check'

# Run integration tests (needs Docker running):
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && make integration-test'

# Run integration tests against existing cluster (no kind lifecycle):
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && make integration-test-existing'

# Start Next.js UI dev server (needs node/npm locally or on studio):
cd /Users/sam/workdir/GoRepo/kcert-checker/ui
cp .env.local.example .env.local
npm install && npm run dev    # http://localhost:3000

# Re-apply demo secrets if lost after cluster restart:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka apply -f \
  /Users/sam/workdir/GoRepo/kcert-checker/test-certs-deploy/monitoring-demo-secrets.yaml'


# =============================================================================
# SESSION 2 COMMANDS (Sept 15 2026 — continued)
# Covers: extensive code comments, deploy/helm fixes, deploy.sh rewrite,
#         kubectl context fix, Next.js UI Dockerfile, next.config.ts standalone
# =============================================================================

# ─────────────────────────────────────────────────────────────────────────────
# 13. EXTENSIVE CODE COMMENTS — ALL GO + NEXT.JS + TEST FILES + MAKEFILE
# ─────────────────────────────────────────────────────────────────────────────

# Comments were added where missing or unclear across all packages.
# Rule: only WHY comments — no WHAT comments where names are self-explanatory.
# Each Test* function got a one-line scenario comment.

# Files annotated (Go):
#   internal/config/config.go          - zero-value replacement, container modes
#   internal/filter/filter.go          - match = EXCLUDE (counterintuitive)
#   internal/graph/graph.go            - Fingerprint meaning, shared cert detection
#   internal/metrics/metrics.go        - NewWithRegistry for test isolation
#   internal/result/result.go          - SourceIngress/Gateway reserved, DaysLeft sign
#   internal/result/deduplicate.go     - why order slice exists (stable output)
#   internal/scanner/scanner.go        - namespaceScanWorkers=5 rationale, execTimeout=5s,
#                                        buffered channel, goroutine fan-out, ServiceAccount skip
#   internal/output/output.go          - two tabwriter sections, podFromSourceName separator
#   internal/ui/handler.go             - sync.RWMutex (concurrent writes), embedded HTML
#   cmd/kcert-checker/main.go          - --once, --fail-on, goroutine separation, MarkReady timing

# Files annotated (Next.js):
#   ui/app/page.tsx         - force-dynamic rationale, SSR pre-population
#   ui/app/Dashboard.tsx    - "use client" boundary, useCallback stability, spread+sort
#   ui/app/api-client.ts    - NEXT_PUBLIC_API_URL, cache: no-store
#   ui/app/types.ts         - snake_case from Go JSON tags, source_type values, DaysLeft sign
#   ui/app/status.ts        - threshold mirror with Go backend, static Tailwind classes
#   ui/next.config.ts       - CORS-avoidance proxy, production vs dev behaviour

# Test files annotated (all 11 packages):
#   internal/config/config_test.go
#   internal/filter/filter_test.go
#   internal/graph/graph_test.go
#   internal/metrics/metrics_test.go
#   internal/output/output_test.go
#   internal/result/status_test.go
#   internal/result/deduplicate_test.go
#   internal/scanner/scanner_test.go
#   internal/scanner/testhelper_test.go
#   internal/ui/handler_test.go
#   internal/kubernetes/client_test.go

# Verify build + tests still pass after comment additions:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/Cellar/go/1.27.1/libexec/bin && \
  cd /Users/sam/workdir/GoRepo/kcert-checker && go build ./... && go test ./...'


# ─────────────────────────────────────────────────────────────────────────────
# 14. KUBECTL CONTEXT FIX
# ─────────────────────────────────────────────────────────────────────────────

# Symptom: kubectl cluster-info showed connection refused on localhost:8080
# Root cause: default context was unset (no current-context in kubeconfig)

kubectl was falling back to localhost:8080 instead of the kind cluster

# List available contexts:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && kubectl config get-contexts'

# Switch to the kind-cka context:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && kubectl config use-context kind-cka'

# Verify cluster is reachable:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && kubectl cluster-info'


# ─────────────────────────────────────────────────────────────────────────────
# 15. DEPLOY/ AND HELM/ — ADD HEALTHZ/READYZ PROBES + NEXT.JS UI RESOURCES
# ─────────────────────────────────────────────────────────────────────────────

# All HTTP endpoints on port 8080: /metrics /healthz /readyz /ui /api/certificates

# deploy/service.yaml

# - Added comment listing all 4 endpoint paths on port 8080

# deploy/deployment.yaml (rewritten)

# - kcert-checker: added livenessProbe (/healthz) + readinessProbe (/readyz)

# - kcert-checker-ui Deployment: separate pod running Next.js on port 3000

# with NEXT_PUBLIC_API_URL=http://kcert-checker.monitoring.svc.cluster.local:8080

# - kcert-checker-ui Service: ClusterIP on port 3000

# helm/kcert-checker/values.yaml (updated)

# - probes.liveness / probes.readiness blocks (path, delays, failureThreshold)

# - ui section: enabled (bool), image, backendURL, service.port, resources, securityContext

# - scan.warning_days / scan.critical_days added (were missing)

# - namespaces: all fixed to namespaces: { exclude: [] }

# - pods.exclude / secrets.exclude / containers.exclude added

# helm/kcert-checker/templates/deployment.yaml (updated)

# - Renders probes from .Values.probes

# - Optional UI Deployment block gated on .Values.ui.enabled

# helm/kcert-checker/templates/service.yaml (updated)

# - Optional UI Service block gated on .Values.ui.enabled

# helm/kcert-checker/templates/configmap.yaml (rewritten)

# - Renders struct-shaped namespaces/pods/secrets/containers config correctly

# Verify helm chart lints cleanly:

ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  helm lint /Users/sam/workdir/GoRepo/kcert-checker/helm/kcert-checker'


# ─────────────────────────────────────────────────────────────────────────────
# 16. NEXT.JS UI DOCKERFILE + output: standalone
# ─────────────────────────────────────────────────────────────────────────────

# ui/Dockerfile (new)

# Multi-stage: node:22-alpine builder → minimal runner as UID 1001

# Stage 1: npm ci + npm run build

# Stage 2: copies only .next/standalone (no node_modules at runtime)

# Matches securityContext.runAsUser: 1001 in values.yaml

# ui/next.config.ts (updated)

# Added: output: "standalone"

# Required for the Dockerfile to copy .next/standalone as the runtime artifact.

# Without it, `node server.js` would fail with module-not-found errors.

# To build the UI image (requires Docker Desktop running, run in terminal not SSH):

cd /Users/sam/workdir/GoRepo/kcert-checker
docker buildx build --platform linux/arm64 -t kcert-checker-ui:local ./ui

kind load docker-image kcert-checker-ui:local --name=cka

# ─────────────────────────────────────────────────────────────────────────────
# 17. deploy/configmap.yaml — FIX INVALID namespaces: all
# ─────────────────────────────────────────────────────────────────────────────

# Root cause: deploy/configmap.yaml had `namespaces: all` which cannot unmarshal

# into config.NamespaceConfig (a struct with Exclude []string).

# The pod was CrashLoopBackOff with exit code 10 (exitFailure from main.go).

# Verify the crash reason:

ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka logs -n monitoring deployment/kcert-checker 2>&1 | tail -5'

# Output: failed to load config: parse config file: yaml: unmarshal errors:

# line 5: cannot unmarshal !!str `all` into config.NamespaceConfig

# Fix applied to deploy/configmap.yaml:

# namespaces:

# exclude: [] (was: namespaces: all)

# Also added: pods.exclude, secrets.exclude, containers.exclude, warning_days, critical_days

# Changed: kubernetes.mode: in-cluster (was kubeconfig — not valid inside a pod)

# ─────────────────────────────────────────────────────────────────────────────
# 18. scripts/deploy.sh — FULL REWRITE
# ─────────────────────────────────────────────────────────────────────────────

# Old deploy.sh problems:

# - No error handling (missing set -euo pipefail)

# - Only applied deployment.yaml, skipped namespace/rbac/configmap/service

# - Bare kubectl/kind/docker — not found over SSH without PATH

# - No UI deployment support

# - No graceful handling of missing ServiceMonitor CRD

# New deploy.sh features:

# set -euo pipefail

# Flags: --skip-build (reuse existing image), --with-ui (also deploy Next.js UI)

# Resolves docker/kind/kubectl from /Applications/Docker.app + /opt/homebrew/bin

# Pre-flight: verifies kind cluster exists, sets kubectl context

# Build + kind load backend image (and optionally UI image)

# Applies manifests in dependency order:

# namespace → serviceaccount → clusterrole → clusterrolebinding →

# configmap → service → deployment

# Skips servicemonitor.yaml gracefully if monitoring.coreos.com CRD absent

# Waits for rollout completion (kubectl rollout status --timeout=120s)

# Prints pods + last 20 log lines + port-forward instructions

# Note on docker build over SSH:

# macOS keychain blocks Docker credential access in headless SSH sessions.

# Use --skip-build when the image already exists, or run from a local terminal:

security -v unlock-keychain ~/Library/Keychains/login.keychain-db

# Execute (skip build — image already loaded):

ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && bash scripts/deploy.sh --skip-build'

# Execute with UI (once UI image is built):

ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && \
  bash scripts/deploy.sh --skip-build --with-ui'


# ─────────────────────────────────────────────────────────────────────────────
# 19. QUICK REFERENCE — KUBERNETES OPERATIONS
# ─────────────────────────────────────────────────────────────────────────────

# Check pod status after deploy:

ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka get pods -n monitoring -l app=kcert-checker'

# Tail live logs:

ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka logs -n monitoring deployment/kcert-checker -f'

# Access backend endpoints via port-forward:
ssh -L 8080:localhost:8080 sam@studio \
  'kubectl --context kind-cka port-forward svc/kcert-checker 8080:8080 -n monitoring'```
# Then open: http://localhost:8080/ui  or  http://localhost:8080/api/certificates

# Access Next.js UI via port-forward (if deployed with --with-ui):
ssh -L 3000:localhost:3000 sam@studio \
  'kubectl --context kind-cka port-forward svc/kcert-checker-ui 3000:3000 -n monitoring'

# Then open: http://localhost:3000`

# Force re-deploy with new image (after local `docker build`):
ssh sam@studio 'cd /Users/sam/workdir/GoRepo/kcert-checker && bash scripts/deploy.sh'

# Re-apply demo secrets (lost after cluster restart):
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  kubectl --context kind-cka apply -f \
  /Users/sam/workdir/GoRepo/kcert-checker/test-certs-deploy/monitoring-demo-secrets.yaml'

# Helm deploy (alternative to deploy/ raw manifests):
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  helm upgrade --install kcert-checker \
  /Users/sam/workdir/GoRepo/kcert-checker/helm/kcert-checker \
  --namespace monitoring \
  --set image.repository=kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never'

# Helm deploy with Next.js UI enabled:
ssh sam@studio 'export PATH=$PATH:/opt/homebrew/bin && \
  helm upgrade --install kcert-checker \
  /Users/sam/workdir/GoRepo/kcert-checker/helm/kcert-checker \
  --namespace monitoring \
  --set image.repository=kcert-checker \
  --set image.tag=local \
  --set image.pullPolicy=Never \
  --set ui.enabled=true \
  --set ui.image.repository=kcert-checker-ui \
  --set ui.image.tag=local \
  --set ui.image.pullPolicy=Never'
````

```

```
