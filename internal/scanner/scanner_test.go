package scanner

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/prometheus/client_golang/prometheus"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sirajudheenam/kcert-checker/internal/config"
	"github.com/sirajudheenam/kcert-checker/internal/metrics"
	"github.com/sirajudheenam/kcert-checker/internal/result"
)

func buildTestScanner(objs []runtime.Object, files map[string][]byte, mode string) (*Scanner, *metrics.Metrics) {
	clientset := fake.NewSimpleClientset(objs...)
	reader := &fakeContainerReader{files: files}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())

	if mode == "" {
		mode = "exec"
	}

	s := New(clientset, reader, m, config.ScannerConfig{
		// Use glob patterns: the fake reader expands these via filepath.Match.
		Certificates: config.CertificateConfig{Paths: []string{"/etc/certs/*", "/etc/tls/*"}},
		Containers:   config.ContainerConfig{Mode: mode, Exclude: []string{"istio-proxy"}},
		Namespaces:   config.NamespaceConfig{Exclude: []string{"kube-system"}},
		Secrets:      config.SecretConfig{Exclude: []string{"sh.helm.release.*"}},
	})
	return s, m
}

func runningPod(namespace, name string, containers ...string) *corev1.Pod {
	var specs []corev1.Container
	for _, c := range containers {
		specs = append(specs, corev1.Container{Name: c})
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       corev1.PodSpec{Containers: specs},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func tlsSecret(namespace, name, key string, data []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{key: data},
	}
}

func ns(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

// --- ParseCertificates ---

// TestParseCertificatesValid verifies that a single valid PEM cert is parsed
// and its Subject CN is preserved.
func TestParseCertificatesValid(t *testing.T) {
	pem := selfSignedPEM(t, "test.example.com", time.Now().Add(24*time.Hour))
	certs, err := ParseCertificates(pem)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 1 {
		t.Errorf("expected 1 cert, got %d", len(certs))
	}
	if certs[0].Subject.CommonName != "test.example.com" {
		t.Errorf("subject = %q", certs[0].Subject.CommonName)
	}
}

// TestParseCertificatesChain verifies that a PEM bundle with multiple certs
// (leaf, intermediate, root) returns all three in order.
func TestParseCertificatesChain(t *testing.T) {
	var chain []byte
	chain = append(chain, selfSignedPEM(t, "leaf", time.Now().Add(24*time.Hour))...)
	chain = append(chain, selfSignedPEM(t, "intermediate", time.Now().Add(365*24*time.Hour))...)
	chain = append(chain, selfSignedPEM(t, "root", time.Now().Add(3650*24*time.Hour))...)

	certs, err := ParseCertificates(chain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 3 {
		t.Errorf("expected 3 certs, got %d", len(certs))
	}
}

// TestParseCertificatesEmpty ensures empty input is an error rather than a panic.
func TestParseCertificatesEmpty(t *testing.T) {
	_, err := ParseCertificates([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

// TestParseCertificatesJunk ensures non-PEM text is an error rather than a panic.
func TestParseCertificatesJunk(t *testing.T) {
	_, err := ParseCertificates([]byte("this is not a PEM"))
	if err == nil {
		t.Error("expected error for junk input")
	}
}

// --- looksLikeCertificateKey ---

// TestLooksLikeCertificateKey verifies the key-name allowlist/denylist.
// Private key suffixes (.key) and generic fields (password) must be rejected
// so scanSecrets doesn't attempt to parse them as certs.
func TestLooksLikeCertificateKey(t *testing.T) {
	trueKeys := []string{
		"tls.crt", "ca.crt", "cert", "certificate", "tls.pem",
		"cert.pem", "ca.pem", "chain.pem", "fullchain.crt",
		"fullchain.pem", "bundle.crt", "bundle.pem",
		"server.crt", "my-ca.pem", "custom.crt",
		"TLS.CRT", "CA.CRT",
	}
	falseKeys := []string{
		"password", "token", "username", ".gitignore",
		"tls.key", "ca.key", "private.key",
	}
	for _, k := range trueKeys {
		if !looksLikeCertificateKey(k) {
			t.Errorf("expected true for %q", k)
		}
	}
	for _, k := range falseKeys {
		if looksLikeCertificateKey(k) {
			t.Errorf("expected false for %q", k)
		}
	}
}

// --- buildResult ---

// TestBuildResultFingerprint verifies that the fingerprint is derived from the
// certificate's DER bytes, not from metadata — so the same cert produces the
// same fingerprint regardless of namespace, pod, or path.
func TestBuildResultFingerprint(t *testing.T) {
	pem := selfSignedPEM(t, "fp-test", time.Now().Add(24*time.Hour))
	certs, err := ParseCertificates(pem)
	if err != nil {
		t.Fatal(err)
	}
	r1 := buildResult("ns", "pod", "app", "/etc/tls/tls.crt", result.SourceContainer, certs[0])
	r2 := buildResult("other-ns", "other", "other", "/different", result.SourceSecret, certs[0])

	if r1.Fingerprint != r2.Fingerprint {
		t.Errorf("fingerprints differ for same cert: %q vs %q", r1.Fingerprint, r2.Fingerprint)
	}
	if len(r1.Fingerprint) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(r1.Fingerprint))
	}
}

// TestBuildResultFields verifies that all metadata fields are populated
// correctly and that SourceName follows the "pod:path" convention.
func TestBuildResultFields(t *testing.T) {
	expiry := time.Now().Add(10 * 24 * time.Hour)
	pem := selfSignedPEM(t, "my-service", expiry)
	certs, err := ParseCertificates(pem)
	if err != nil {
		t.Fatal(err)
	}
	r := buildResult("monitoring", "pod-x", "app", "/etc/certs/tls.crt", result.SourceContainer, certs[0])

	if r.Subject != "my-service" {
		t.Errorf("subject = %q", r.Subject)
	}
	if r.SourceType != result.SourceContainer {
		t.Errorf("sourceType = %q", r.SourceType)
	}
	if r.DaysLeft < 9 || r.DaysLeft > 11 {
		t.Errorf("daysLeft = %d, want ~10", r.DaysLeft)
	}
	if r.Expired {
		t.Error("cert should not be expired")
	}
	if r.SourceName != "pod-x:/etc/certs/tls.crt" {
		t.Errorf("sourceName = %q", r.SourceName)
	}
}

// TestBuildResultExpired confirms that DaysLeft < 0 sets the Expired flag.
func TestBuildResultExpired(t *testing.T) {
	pem := selfSignedPEM(t, "expired", time.Now().Add(-24*time.Hour))
	certs, err := ParseCertificates(pem)
	if err != nil {
		t.Fatal(err)
	}
	r := buildResult("ns", "", "", "/etc/certs/tls.crt", result.SourceSecret, certs[0])
	if !r.Expired {
		t.Error("expected cert to be marked expired")
	}
}

// --- Scan integration ---

// TestScanDiscoversContainerCerts is the happy-path for container scanning:
// a running pod has a cert at a configured path → the scan returns 1 result.
func TestScanDiscoversContainerCerts(t *testing.T) {
	certPEM := selfSignedPEM(t, "container-cert", time.Now().Add(30*24*time.Hour))
	pod := runningPod("default", "my-pod", "app")
	objs := []runtime.Object{ns("default"), pod}
	files := map[string][]byte{"/etc/certs/tls.crt": certPEM}

	s, _ := buildTestScanner(objs, files, "exec")
	results := s.Scan(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Subject != "container-cert" {
		t.Errorf("subject = %q", results[0].Subject)
	}
}

// TestScanDiscoversSecretCerts is the happy-path for Secret scanning.
func TestScanDiscoversSecretCerts(t *testing.T) {
	certPEM := selfSignedPEM(t, "secret-cert", time.Now().Add(90*24*time.Hour))
	objs := []runtime.Object{ns("default"), tlsSecret("default", "my-tls", "tls.crt", certPEM)}

	s, _ := buildTestScanner(objs, nil, "disabled")
	results := s.Scan(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SourceType != result.SourceSecret {
		t.Errorf("sourceType = %q", results[0].SourceType)
	}
}

// TestScanSkipsExcludedNamespace ensures the ExcludeNamespaces list is honoured.
func TestScanSkipsExcludedNamespace(t *testing.T) {
	certPEM := selfSignedPEM(t, "kube-cert", time.Now().Add(24*time.Hour))
	objs := []runtime.Object{
		ns("kube-system"),
		tlsSecret("kube-system", "kube-tls", "tls.crt", certPEM),
	}
	s, _ := buildTestScanner(objs, nil, "disabled")
	if results := s.Scan(context.Background()); len(results) != 0 {
		t.Errorf("expected 0 (kube-system excluded), got %d", len(results))
	}
}

// TestScanSkipsExcludedContainer ensures sidecar containers listed in Exclude are skipped.
func TestScanSkipsExcludedContainer(t *testing.T) {
	certPEM := selfSignedPEM(t, "sidecar-cert", time.Now().Add(24*time.Hour))
	pod := runningPod("default", "my-pod", "istio-proxy")
	objs := []runtime.Object{ns("default"), pod}
	files := map[string][]byte{"/etc/certs/tls.crt": certPEM}

	s, _ := buildTestScanner(objs, files, "exec")
	if results := s.Scan(context.Background()); len(results) != 0 {
		t.Errorf("expected 0 (istio-proxy excluded), got %d", len(results))
	}
}

// TestScanSkipsExcludedSecret ensures glob-matched secret names (e.g. Helm releases) are skipped.
func TestScanSkipsExcludedSecret(t *testing.T) {
	certPEM := selfSignedPEM(t, "helm-cert", time.Now().Add(24*time.Hour))
	objs := []runtime.Object{
		ns("default"),
		tlsSecret("default", "sh.helm.release.v1.foo.v1", "tls.crt", certPEM),
	}
	s, _ := buildTestScanner(objs, nil, "disabled")
	if results := s.Scan(context.Background()); len(results) != 0 {
		t.Errorf("expected 0 (helm secret excluded), got %d", len(results))
	}
}

// TestScanDisabledContainerMode ensures container mode "disabled" skips all exec calls.
func TestScanDisabledContainerMode(t *testing.T) {
	certPEM := selfSignedPEM(t, "container-cert", time.Now().Add(24*time.Hour))
	pod := runningPod("default", "my-pod", "app")
	objs := []runtime.Object{ns("default"), pod}
	files := map[string][]byte{"/etc/certs/tls.crt": certPEM}

	s, _ := buildTestScanner(objs, files, "disabled")
	if results := s.Scan(context.Background()); len(results) != 0 {
		t.Errorf("expected 0 with container mode disabled, got %d", len(results))
	}
}

// TestScanMetricsCounts verifies that after a scan the Prometheus gauges
// for total and expired certificate counts reflect what was found.
func TestScanMetricsCounts(t *testing.T) {
	expired := selfSignedPEM(t, "expired", time.Now().Add(-48*time.Hour))
	healthy := selfSignedPEM(t, "healthy", time.Now().Add(90*24*time.Hour))
	objs := []runtime.Object{
		ns("default"),
		tlsSecret("default", "expired-tls", "tls.crt", expired),
		tlsSecret("default", "healthy-tls", "tls.crt", healthy),
	}

	s, m := buildTestScanner(objs, nil, "disabled")
	_ = s.Scan(context.Background())

	total := promtest.ToFloat64(m.CertificatesTotal)
	if total != 2 {
		t.Errorf("CertificatesTotal = %.0f, want 2", total)
	}
	expiredCount := promtest.ToFloat64(m.CertificatesExpired)
	if expiredCount != 1 {
		t.Errorf("CertificatesExpired = %.0f, want 1", expiredCount)
	}
}

// --- Pod exclusion ---

// TestScanSkipsExcludedPod verifies glob exclusion for pod names.
func TestScanSkipsExcludedPod(t *testing.T) {
	certPEM := selfSignedPEM(t, "debug-cert", time.Now().Add(24*time.Hour))
	pod := runningPod("default", "debug-pod-xyz", "app")
	objs := []runtime.Object{ns("default"), pod}
	files := map[string][]byte{"/etc/certs/tls.crt": certPEM}

	clientset := fake.NewSimpleClientset(objs...)
	reader := &fakeContainerReader{files: files}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		Certificates: config.CertificateConfig{Paths: []string{"/etc/certs/*"}},
		Containers:   config.ContainerConfig{Mode: "exec"},
		Pods:         config.PodConfig{Exclude: []string{"debug-*"}},
	})
	if results := s.Scan(context.Background()); len(results) != 0 {
		t.Errorf("expected 0 (debug-* pod excluded), got %d", len(results))
	}
}

// --- Init containers ---

// TestScanIncludesInitContainers verifies that IncludeInitContainers=true causes
// init containers to be probed alongside regular containers.
func TestScanIncludesInitContainers(t *testing.T) {
	certPEM := selfSignedPEM(t, "init-cert", time.Now().Add(24*time.Hour))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers:     []corev1.Container{{Name: "app"}},
			InitContainers: []corev1.Container{{Name: "init-certs"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	objs := []runtime.Object{ns("default"), pod}
	files := map[string][]byte{"/etc/certs/tls.crt": certPEM}

	clientset := fake.NewSimpleClientset(objs...)
	reader := &fakeContainerReader{files: files}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		Certificates: config.CertificateConfig{Paths: []string{"/etc/certs/*"}},
		Containers:   config.ContainerConfig{Mode: "exec", IncludeInitContainers: true},
	})
	results := s.Scan(context.Background())
	// app container + init container both have the file → 2 results
	if len(results) != 2 {
		t.Errorf("expected 2 results (app + init), got %d", len(results))
	}
}

// --- Non-running pod skipped ---

// TestScanSkipsPendingPod ensures non-Running pods are never exec'd — they
// have no IP and exec would hang or fail.
func TestScanSkipsPendingPod(t *testing.T) {
	certPEM := selfSignedPEM(t, "pending-cert", time.Now().Add(24*time.Hour))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	objs := []runtime.Object{ns("default"), pod}
	files := map[string][]byte{"/etc/certs/tls.crt": certPEM}

	s, _ := buildTestScanner(objs, files, "exec")
	if results := s.Scan(context.Background()); len(results) != 0 {
		t.Errorf("expected 0 for pending pod, got %d", len(results))
	}
}

// --- Secret scanning: service account token skipped ---

// TestScanSkipsServiceAccountToken ensures the scanner never tries to parse
// JWT tokens as PEM certificates.
func TestScanSkipsServiceAccountToken(t *testing.T) {
	certPEM := selfSignedPEM(t, "sa-cert", time.Now().Add(24*time.Hour))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "sa-token", Namespace: "default"},
		Type:       corev1.SecretTypeServiceAccountToken,
		Data:       map[string][]byte{"tls.crt": certPEM},
	}
	objs := []runtime.Object{ns("default"), secret}
	s, _ := buildTestScanner(objs, nil, "disabled")
	if results := s.Scan(context.Background()); len(results) != 0 {
		t.Errorf("expected 0 (SA token secret skipped), got %d", len(results))
	}
}

// --- Secret key filtering ---

// TestScanSkipsNonCertSecretKeys verifies that only cert-named keys (tls.crt)
// are parsed; tls.key and password must be skipped.
func TestScanSkipsNonCertSecretKeys(t *testing.T) {
	certPEM := selfSignedPEM(t, "cert", time.Now().Add(24*time.Hour))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mixed-secret", Namespace: "default"},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"tls.crt":  certPEM,
			"tls.key":  []byte("private-key-data"),
			"password": []byte("s3cr3t"),
		},
	}
	objs := []runtime.Object{ns("default"), secret}
	s, _ := buildTestScanner(objs, nil, "disabled")
	results := s.Scan(context.Background())
	// Only tls.crt should be parsed; tls.key and password are skipped
	if len(results) != 1 {
		t.Errorf("expected 1 result (only tls.crt), got %d", len(results))
	}
}

// --- Multi-cert chain in a secret ---

// TestScanCertChainInSecret verifies that a PEM bundle stored in a single secret
// key is split into individual CertificateResult entries.
func TestScanCertChainInSecret(t *testing.T) {
	var chain []byte
	chain = append(chain, selfSignedPEM(t, "leaf", time.Now().Add(30*24*time.Hour))...)
	chain = append(chain, selfSignedPEM(t, "intermediate-ca", time.Now().Add(365*24*time.Hour))...)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "chain-secret", Namespace: "default"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"tls.crt": chain},
	}
	objs := []runtime.Object{ns("default"), secret}
	s, _ := buildTestScanner(objs, nil, "disabled")
	results := s.Scan(context.Background())
	if len(results) != 2 {
		t.Errorf("expected 2 results for cert chain, got %d", len(results))
	}
}

// --- ParseCertificates edge cases ---

// TestParseCertificatesInvalidDER confirms an error (not a panic) when a PEM
// block has a valid header but its base64 payload is not valid DER.
func TestParseCertificatesInvalidDER(t *testing.T) {
	// Wrap junk bytes in a valid PEM header but with invalid DER
	badPEM := []byte("-----BEGIN CERTIFICATE-----\naW52YWxpZA==\n-----END CERTIFICATE-----\n")
	_, err := ParseCertificates(badPEM)
	if err == nil {
		t.Error("expected error for invalid DER in PEM block")
	}
}

// TestParseCertificatesSkipsNonCertBlocks confirms that PRIVATE KEY PEM blocks
// are silently skipped and only CERTIFICATE blocks are returned.
func TestParseCertificatesSkipsNonCertBlocks(t *testing.T) {
	// A PEM file with a PRIVATE KEY block before a valid cert
	certPEM := selfSignedPEM(t, "real-cert", time.Now().Add(24*time.Hour))
	keyBlock := []byte("-----BEGIN PRIVATE KEY-----\nZmFrZQ==\n-----END PRIVATE KEY-----\n")
	combined := append(keyBlock, certPEM...)
	certs, err := ParseCertificates(combined)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(certs) != 1 {
		t.Errorf("expected 1 cert (key block skipped), got %d", len(certs))
	}
}

// --- Error injection via fake reactor ---

// TestScanNamespaceListError verifies graceful degradation when the API
// server returns an error listing namespaces.
func TestScanNamespaceListError(t *testing.T) {
	clientset := fake.NewSimpleClientset(ns("default"))
	clientset.Fake.PrependReactor("list", "namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("namespace list error")
		},
	)
	reader := &fakeContainerReader{}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		Certificates: config.CertificateConfig{Paths: []string{"/etc/certs/tls.crt"}},
		Containers:   config.ContainerConfig{Mode: "disabled"},
	})
	// Should return nil because namespace list itself failed
	results := s.Scan(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 results on namespace list error, got %d", len(results))
	}
}

// TestScanContextCancelledBeforeScan confirms that a pre-cancelled context
// causes Scan to return immediately with 0 results.
func TestScanContextCancelledBeforeScan(t *testing.T) {
	certPEM := selfSignedPEM(t, "cert", time.Now().Add(24*time.Hour))
	objs := []runtime.Object{ns("default"), tlsSecret("default", "tls", "tls.crt", certPEM)}
	s, _ := buildTestScanner(objs, nil, "disabled")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	results := s.Scan(ctx)
	if len(results) != 0 {
		t.Errorf("expected 0 results with cancelled context, got %d", len(results))
	}
}

// TestScanSecretListError verifies the scan does not crash when the secret
// list API call fails for a namespace.
func TestScanSecretListError(t *testing.T) {
	clientset := fake.NewSimpleClientset(ns("default"))
	clientset.Fake.PrependReactor("list", "secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("secret list error")
		},
	)
	reader := &fakeContainerReader{}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		Certificates: config.CertificateConfig{Paths: []string{"/etc/certs/tls.crt"}},
		Containers:   config.ContainerConfig{Mode: "disabled"},
	})
	// Scan should complete without panic even when secret list fails
	results := s.Scan(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 results on secret list error, got %d", len(results))
	}
}

// TestScanPodListError verifies the scan does not crash when the pod list
// API call fails for a namespace.
func TestScanPodListError(t *testing.T) {
	clientset := fake.NewSimpleClientset(ns("default"))
	clientset.Fake.PrependReactor("list", "pods",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("pod list error")
		},
	)
	reader := &fakeContainerReader{}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		Certificates: config.CertificateConfig{Paths: []string{"/etc/certs/tls.crt"}},
		Containers:   config.ContainerConfig{Mode: "exec"},
	})
	// Scan should not panic even on pod list failure
	_ = s.Scan(context.Background())
}

// TestScanGlobExpandsMultipleFiles verifies that a single glob pattern like
// "/etc/certs/*" discovers all cert files in that directory rather than only
// an exact filename.
func TestScanGlobExpandsMultipleFiles(t *testing.T) {
	cert1 := selfSignedPEM(t, "service-a", time.Now().Add(30*24*time.Hour))
	cert2 := selfSignedPEM(t, "service-b", time.Now().Add(60*24*time.Hour))
	cert3 := selfSignedPEM(t, "ca-root", time.Now().Add(365*24*time.Hour))

	pod := runningPod("default", "my-pod", "app")
	objs := []runtime.Object{ns("default"), pod}
	// Three distinct files under the same directory — all matched by /etc/certs/*
	files := map[string][]byte{
		"/etc/certs/tls.crt": cert1,
		"/etc/certs/ca.crt":  cert2,
		"/etc/certs/chain.crt": cert3,
	}

	clientset := fake.NewSimpleClientset(objs...)
	reader := &fakeContainerReader{files: files}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		Certificates: config.CertificateConfig{Paths: []string{"/etc/certs/*"}},
		Containers:   config.ContainerConfig{Mode: "exec"},
	})
	results := s.Scan(context.Background())
	if len(results) != 3 {
		t.Errorf("expected 3 results (all files matched by glob), got %d", len(results))
	}
}

// TestScanGlobNoMatch verifies that a pattern matching no files produces zero
// results without an error — most containers won't have most directories.
func TestScanGlobNoMatch(t *testing.T) {
	pod := runningPod("default", "empty-pod", "app")
	objs := []runtime.Object{ns("default"), pod}

	s, _ := buildTestScanner(objs, map[string][]byte{}, "exec")
	results := s.Scan(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty container, got %d", len(results))
	}
}

// TestScanGlobMultiplePatterns verifies that multiple glob patterns cover
// different directories and their results are combined.
func TestScanGlobMultiplePatterns(t *testing.T) {
	certA := selfSignedPEM(t, "tls-cert", time.Now().Add(30*24*time.Hour))
	certB := selfSignedPEM(t, "ca-cert", time.Now().Add(90*24*time.Hour))

	pod := runningPod("default", "my-pod", "app")
	objs := []runtime.Object{ns("default"), pod}
	files := map[string][]byte{
		"/etc/tls/tls.crt":  certA,
		"/etc/certs/ca.crt": certB,
	}

	clientset := fake.NewSimpleClientset(objs...)
	reader := &fakeContainerReader{files: files}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		// Two separate glob patterns covering two different directories.
		Certificates: config.CertificateConfig{Paths: []string{"/etc/tls/*", "/etc/certs/*"}},
		Containers:   config.ContainerConfig{Mode: "exec"},
	})
	results := s.Scan(context.Background())
	if len(results) != 2 {
		t.Errorf("expected 2 results (one from each glob), got %d", len(results))
	}
}

// TestScanExactPathStillWorks confirms backwards compatibility: an exact path
// like "/etc/tls/tls.crt" (no wildcard) still works because ListFiles returns
// it verbatim when filepath.Match matches an exact pattern against itself.
func TestScanExactPathStillWorks(t *testing.T) {
	cert := selfSignedPEM(t, "exact-path", time.Now().Add(30*24*time.Hour))
	pod := runningPod("default", "my-pod", "app")
	objs := []runtime.Object{ns("default"), pod}
	files := map[string][]byte{"/etc/tls/tls.crt": cert}

	clientset := fake.NewSimpleClientset(objs...)
	reader := &fakeContainerReader{files: files}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		Certificates: config.CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}},
		Containers:   config.ContainerConfig{Mode: "exec"},
	})
	results := s.Scan(context.Background())
	if len(results) != 1 {
		t.Errorf("expected 1 result for exact path, got %d", len(results))
	}
}

// --- Container read error (all reads fail) ---

type erroringReader struct {
	err error
}

func (r *erroringReader) ListFiles(_ context.Context, _, _, _, _ string) ([]string, error) {
	return nil, r.err
}

func (r *erroringReader) ReadFile(_ context.Context, _, _, _ string, _ string) ([]byte, error) {
	return nil, r.err
}

// TestScanContainerReadError verifies that exec failures on all paths still
// produce a clean (empty) result rather than an error.
func TestScanContainerReadError(t *testing.T) {
	// All container reads fail; scan should still succeed with 0 results
	pod := runningPod("default", "my-pod", "app", "sidecar")
	objs := []runtime.Object{ns("default"), pod}

	clientset := fake.NewSimpleClientset(objs...)
	reader := &erroringReader{err: errors.New("exec failed: permission denied")}
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := New(clientset, reader, m, config.ScannerConfig{
		Certificates: config.CertificateConfig{Paths: []string{"/etc/certs/tls.crt"}},
		Containers:   config.ContainerConfig{Mode: "exec"},
	})
	results := s.Scan(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 on read errors, got %d", len(results))
	}
}

// TestScanContainerInvalidPEM verifies that a PEM file containing only a
// PRIVATE KEY block (not a CERTIFICATE) produces 0 results.
// scanContainer: invalid PEM (parses but not a certificate)
func TestScanContainerInvalidPEM(t *testing.T) {
	pod := runningPod("default", "my-pod", "app")
	objs := []runtime.Object{ns("default"), pod}
	// Provide a valid PEM block but with a non-CERTIFICATE type
	privateKeyPEM := []byte("-----BEGIN PRIVATE KEY-----\nZmFrZQ==\n-----END PRIVATE KEY-----\n")
	files := map[string][]byte{"/etc/certs/tls.crt": privateKeyPEM}

	s, _ := buildTestScanner(objs, files, "exec")
	results := s.Scan(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 (no cert block in PEM), got %d", len(results))
	}
}

// --- MetricsNew smoke test ---

// TestMetricsNewDoesNotPanic is a smoke test that metrics initialisation
// with an isolated registry does not panic.
func TestMetricsNewDoesNotPanic(t *testing.T) {
	// metrics.New() registers on the global registry; only call once safely
	// by using NewWithRegistry in all other tests. Here we just ensure
	// the package-level function works when called with a fresh registry.
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	if m == nil {
		t.Error("expected non-nil metrics")
	}
}
