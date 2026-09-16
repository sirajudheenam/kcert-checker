Yes. I’d turn **kcert into a small but serious Kubernetes product** in a very deliberate order. The mistake would be jumping straight into dashboards, SaaS, authentication, billing, etc. First make the core scanner excellent.

You already have a good base: Go CLI, Kubernetes Secrets, Ingress, Gateway API, container scanning, certificate parsing, and ConfigMap-based configuration.

## Phase 1 — Make the scanner solid

Your immediate next milestone should be:

> **kcert v0.1 — reliably discover certificates and explain where every certificate came from.**

### Step 1: Finish config-driven exclusions

This is the feature you are already working toward.

I would make `config.yaml` look roughly like:

```yaml
scan:
  namespaces:
    exclude:
      - kube-system
      - kube-public
      - monitoring
      - "openshift-*"

  containers:
    exclude:
      - istio-proxy
      - linkerd-proxy
      - "init-*"

  secrets:
    exclude:
      - "sh.helm.release.*"

  paths:
    include:
      - /etc/tls
      - /etc/certs
      - /certs
      - /var/run/secrets

    exclude:
      - /var/run/secrets/kubernetes.io/serviceaccount
```

Keep this logic outside the scanner itself.

For example:

```text
internal/config
    config.go

internal/filter
    namespace.go
    container.go
    secret.go
    path.go
```

Then the scanner asks:

```go
if filters.SkipNamespace(namespace) {
    continue
}
```

rather than knowing anything about patterns itself.

That will keep your code maintainable.

---

## Step 2: Define one common certificate result model

This becomes extremely important later.

Don't let Secret scanning, container scanning and Gateway scanning produce different structures.

Create something like:

```go
type CertificateResult struct {
    Cluster     string
    Namespace   string

    SourceType  string
    SourceName  string
    Container   string
    Path        string

    Subject     string
    Issuer      string
    Serial      string

    NotBefore   time.Time
    NotAfter    time.Time
    DaysLeft    int

    DNSNames    []string

    IsCA        bool
    Expired     bool

    Fingerprint string
}
```

Later I'd replace `SourceType string` with an enum-like custom type:

```go
type SourceType string

const (
    SourceSecret    SourceType = "secret"
    SourceContainer SourceType = "container"
    SourceIngress   SourceType = "ingress"
    SourceGateway   SourceType = "gateway"
)
```

This one decision will make Prometheus, JSON output, APIs, databases and SaaS much easier later.

---

# Step 3 — Separate discovery from certificate parsing

I'd structure the scanner conceptually like this:

```text
                Kubernetes
                    │
       ┌────────────┼────────────┐
       │            │            │
    Secrets        Pods       Gateway API
       │            │            │
       └────────────┼────────────┘
                    ▼
               Discoverers
                    │
                    ▼
               []Candidate
                    │
                    ▼
             Certificate Parser
                    │
                    ▼
          []CertificateResult
```

Your discoverer should find something containing certificate bytes.

Something like:

```go
type Candidate struct {
    Namespace  string
    SourceType SourceType
    SourceName string
    Container  string
    Path       string
    Data       []byte
}
```

Then parsing becomes independent:

```go
func ParseCertificate(candidate Candidate) ([]CertificateResult, error)
```

That's a very useful architectural boundary.

---

# Step 4 — Improve PEM chain handling

Certificates are often bundles:

```text
-----BEGIN CERTIFICATE-----
leaf
-----END CERTIFICATE-----

-----BEGIN CERTIFICATE-----
intermediate
-----END CERTIFICATE-----

-----BEGIN CERTIFICATE-----
root
-----END CERTIFICATE-----
```

Don't assume one certificate per file.

Parse **every certificate**.

Then identify:

```text
Leaf
Intermediate CA
Root CA
```

You can determine likely leaf certificates using:

```go
cert.IsCA == false
```

and CA certificates using:

```go
cert.IsCA == true
```

Eventually your result could show:

```text
NAMESPACE   SOURCE      CERTIFICATE       TYPE          EXPIRES
monitoring  prometheus  server.crt        Leaf          12d
monitoring  prometheus  intermediate.crt  Intermediate  250d
monitoring  prometheus  ca.crt            Root          1820d
```

This makes kcert much more useful than a simple expiration checker.

---

# Step 5 — Add certificate fingerprints

Calculate SHA-256 fingerprints.

For example:

```go
sum := sha256.Sum256(cert.Raw)

fingerprint :=
    strings.ToUpper(
        hex.EncodeToString(sum[:]),
    )
```

Why?

Because the same certificate may appear in:

```text
Secret A
Pod X
Pod Y
Ingress
Gateway
```

Without fingerprinting, kcert thinks these are four certificates.

With fingerprinting, you can eventually say:

```text
Certificate
    SHA256: ABCDEF...

Used by:
    Secret monitoring/prometheus-tls
    Pod prometheus-0
    Pod prometheus-1
    Gateway monitoring/gateway
```

This becomes a very valuable feature.

---

# Step 6 — Add certificate deduplication

Once fingerprints exist, build:

```go
map[string][]CertificateLocation
```

Meaning:

```text
fingerprint
     │
     ├── secret/foo
     ├── pod/bar:/etc/tls/tls.crt
     └── gateway/baz
```

Now kcert becomes more than:

> certificate expiry scanner

It becomes:

> **certificate inventory system**

That's a much stronger product concept.

---

# Step 7 — Add proper status classification

Don't just show `DaysLeft`.

Introduce:

```go
type Status string

const (
    StatusOK       Status = "OK"
    StatusWarning  Status = "WARNING"
    StatusCritical Status = "CRITICAL"
    StatusExpired  Status = "EXPIRED"
)
```

Config:

```yaml
thresholds:
  warningDays: 30
  criticalDays: 7
```

Logic:

```text
days < 0
    EXPIRED

days <= 7
    CRITICAL

days <= 30
    WARNING

otherwise
    OK
```

Then:

```text
STATUS     DAYS LEFT
OK             182
WARNING         23
CRITICAL         4
EXPIRED        -12
```

Now humans can understand the output instantly.

---

# Step 8 — JSON output

This is one of the most important next features.

Support:

```bash
kcert scan
```

for humans.

And:

```bash
kcert scan --output json
```

for machines.

Maybe later:

```bash
kcert scan -o table
kcert scan -o json
kcert scan -o yaml
```

JSON makes integration possible with:

```text
jq
CI/CD
Jenkins
GitHub Actions
Prometheus exporters
REST APIs
databases
```

This is where kcert stops being just a terminal utility.

---

# Step 9 — Meaningful exit codes

This is extremely useful in automation.

For example:

```text
0 = all healthy
1 = warnings found
2 = critical certificates found
3 = expired certificates found
10 = scanner failure
```

Then CI can do:

```bash
kcert scan

if [ $? -ge 2 ]; then
    fail pipeline
fi
```

Or provide:

```bash
kcert scan --fail-on critical
```

That is a great DevOps feature.

---

# Step 10 — Prometheus metrics

This is where kcert starts fitting naturally into enterprise Kubernetes.

Expose:

```text
/metrics
```

For example:

```text
kcert_certificate_expiry_seconds
```

with labels:

```text
cluster
namespace
source_type
source_name
```

Example conceptually:

```text
kcert_certificate_expiry_seconds{
    namespace="monitoring",
    source_type="secret",
    source_name="prometheus-tls"
} 1728000
```

You can also expose:

```text
kcert_certificates_total

kcert_certificates_expired_total

kcert_scan_duration_seconds

kcert_scan_errors_total
```

Then people can create Grafana dashboards and alerts.

cert-manager already tracks its own managed certificates and renewal information, so kcert's differentiator should be discovering certificates **outside cert-manager too** — arbitrary Secrets, mounted files, containers and other TLS consumers. cert-manager itself automatically calculates renewal timing for managed `Certificate` resources. ([cert-manager][1])

---

# Step 11 — Make kcert run continuously

Currently think of:

```bash
kcert scan
```

as one mode.

Add:

```bash
kcert server
```

Conceptually:

```text
kcert server
      │
      ├── scan every 5 minutes
      │
      ├── cache results
      │
      ├── expose /metrics
      │
      ├── expose /healthz
      │
      └── expose /readyz
```

Config:

```yaml
server:
  listenAddress: ":8080"

scan:
  interval: 5m
```

Now it can run as a Kubernetes Deployment.

---

# Step 12 — Add health endpoints

Implement:

```text
/healthz
/readyz
```

For example:

```text
GET /healthz

200 OK
```

and readiness only becomes OK when:

```text
Kubernetes API reachable
initial certificate scan completed
```

That will make the Deployment production-friendly.

---

# Step 13 — Fix the container-scanning architecture carefully

This is an important one.

Your current mechanism uses the Kubernetes:

```text
pods/exec
```

subresource.

So conceptually:

```text
kcert
  │
  │ POST pods/exec
  ▼
container
  │
  └── cat /etc/tls/tls.crt
```

That works, but it requires significant permission:

```yaml
resources:
  - pods/exec

verbs:
  - create
```

That's something security teams may dislike.

Eventually you should support **two scanning modes**:

```yaml
containerScanning:
  enabled: true

  mode: exec
```

while allowing:

```yaml
containerScanning:
  enabled: false
```

Then users who won't allow `pods/exec` can still scan:

```text
Secrets
Ingress
Gateway
cert-manager resources
```

That significantly improves deployability.

---

# Step 14 — Explicit RBAC profiles

Instead of one huge ClusterRole, create two Helm/RBAC modes.

### Basic

Can scan:

```text
Secrets
Ingress
Gateway
cert-manager Certificate
```

No pod exec.

### Extended

Adds:

```text
pods
pods/exec
```

Then document:

```text
Basic Mode
──────────
lower permissions
no filesystem scanning

Extended Mode
─────────────
container filesystem certificate discovery
requires pods/exec
```

This is exactly the kind of thing enterprise users care about.

---

# Step 15 — Improve Gateway API support

You're already looking at Gateway API, which is a good decision.

Modern Gateway TLS configuration can reference certificates through listener `certificateRefs`; upstream TLS can also use resources such as `BackendTLSPolicy`. ([Gateway API][2])

So eventually kcert should understand:

```text
Gateway
  └── Listener
        └── certificateRefs
              └── Secret
```

and:

```text
BackendTLSPolicy
      │
      └── caCertificateRefs
```

Then your inventory starts showing actual relationships rather than just resources.

---

# Step 16 — Build a resource relationship graph

This would be one of kcert's killer features.

Instead of showing:

```text
secret: web-tls
```

show:

```text
Certificate
     │
     ▼
Secret web-tls
     │
     ├──── Ingress shop
     │
     ├──── Gateway public-gw
     │
     └──── Pod nginx-7ddc
```

Then a user can answer:

> "What breaks when this certificate expires?"

That's incredibly valuable operationally.

---

# Step 17 — Helm chart

Before SaaS, make installation stupidly easy.

Target experience:

```bash
helm repo add kcert https://...
helm install kcert kcert/kcert
```

Then:

```bash
kubectl port-forward svc/kcert 8080:8080
```

and:

```text
localhost:8080/metrics
```

Your Helm chart should manage:

```text
Deployment
ServiceAccount
ClusterRole
ClusterRoleBinding
ConfigMap
Service
ServiceMonitor optional
```

---

# Step 18 — Test against multiple Kubernetes releases

As of September 2026, upstream Kubernetes supports the three recent minor branches **1.35, 1.36 and 1.37**, with Kubernetes 1.37.0 released August 26, 2026. ([Kubernetes][3])

Your CI should therefore eventually test against multiple `kind` clusters, perhaps:

```text
Kubernetes 1.35
Kubernetes 1.36
Kubernetes 1.37
```

That makes the project look much more professional.

---

# Step 19 — Create an integration-test cluster

This is worth doing soon.

Automatically create fixtures such as:

```text
kcert-test namespace

expired-container
critical-container
warning-container
healthy-container

expired-secret
critical-secret
warning-secret
healthy-secret

Ingress
Gateway
cert-manager Certificate
```

Then:

```bash
make integration-test
```

creates a `kind` cluster, installs fixtures, runs kcert and verifies results.

You've already been manually constructing exactly these certificate scenarios.  Turn those into permanent automated tests.

---

# Step 20 — Only then build the UI

Don't build React first.

Once the backend has:

```text
discovery
parsing
deduplication
status
metrics
JSON
server
```

then add something tiny:

```text
┌──────────────────────────────────────────────────────┐
│ KCERT                           Cluster: production  │
├──────────────────────────────────────────────────────┤
│ Certificates  147                                  │
│ Healthy       131                                  │
│ Warning         9                                  │
│ Critical        5                                  │
│ Expired         2                                  │
├──────────────────────────────────────────────────────┤
│ Certificate       Namespace      Expires   Status   │
│ api.company.com    ingress-nginx    3d      🔴       │
│ internal-ca        monitoring      22d      🟡       │
│ prometheus         monitoring     185d      🟢       │
└──────────────────────────────────────────────────────┘
```

Next.js would be perfectly reasonable for that since you're already comfortable with it.

---

# I would organize the roadmap like this

### `v0.1 — Scanner`

Focus entirely on:

```text
✓ Secrets
✓ Ingress
✓ Gateway
✓ Container files

→ Config exclusions
→ Common CertificateResult
→ PEM chain parsing
→ fingerprinting
→ deduplication
→ status thresholds
→ JSON output
→ tests
```

### `v0.2 — Kubernetes service`

Add:

```text
kcert server
Prometheus metrics
/healthz
/readyz
periodic scanning
Helm chart
RBAC profiles
```

### `v0.3 — Certificate intelligence`

Add:

```text
certificate relationships
cert-manager integration
Gateway relationships
ownership
duplicate detection
orphan certificate detection
```

### `v0.4 — Alerting`

Add:

```text
Slack
Teams
Webhook
Email

"Certificate X expires in 6 days"
```

### `v0.5 — UI`

Add:

```text
Next.js dashboard
cluster overview
search/filter
certificate details
certificate dependency graph
```

### `v1.0`

Then I would aim for:

```text
Single-cluster production-ready kcert
```

After that:

```text
                 kcert OSS
                     │
             ┌───────┴───────┐
             ▼               ▼
      Local Kubernetes     Enterprise
             │               │
             ▼               ▼
         kcert agent ─────► kcert Cloud
                               │
                               ▼
                      Multiple clusters
                               │
                  ┌────────────┼────────────┐
                  ▼            ▼            ▼
               Berlin       AWS-prod     GCP-prod
```

That is when it becomes a potential commercial product.

---

## But right now, don't jump ahead.

I recommend we work through this repository in order.

**Your next four coding tasks should be:**

1. **Implement `config.yaml` exclusions properly.**
2. **Refactor everything to a common `CertificateResult`.**
3. **Add SHA-256 fingerprinting + deduplication.**
4. **Add JSON output and status thresholds.**