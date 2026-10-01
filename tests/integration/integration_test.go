//go:build integration

// Package integration runs end-to-end tests against a live Kubernetes cluster.
//
// Prerequisites:
//   - A running cluster with KUBECONFIG set.
//   - The integration fixtures applied:
//     kubectl apply -f tests/integration/fixtures/
//   - The kcert-fixture pod in namespace kcert-integration must be Ready.
//
// Run with:
//
//	go test -v -count=1 -tags integration -timeout 5m ./tests/integration/...
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/sirajudheenam/kcert-checker/internal/config"
	"github.com/sirajudheenam/kcert-checker/internal/metrics"
	"github.com/sirajudheenam/kcert-checker/internal/result"
	"github.com/sirajudheenam/kcert-checker/internal/scanner"

	"github.com/prometheus/client_golang/prometheus"
)

const integrationNamespace = "kcert-integration"

func newClientset(t *testing.T) kubernetes.Interface {
	t.Helper()
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = home + "/.kube/config"
	}
	// KUBECONFIG may be a colon-separated list; use client-go's loading rules
	// which understand the list format (same as kubectl).
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.Precedence = filepath.SplitList(kubeconfig)
	}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)
	cfg, err := clientCfg.ClientConfig()
	if err != nil {
		t.Fatalf("build kubeconfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create clientset: %v", err)
	}
	return cs
}

func waitForPod(t *testing.T, cs kubernetes.Interface, namespace, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := cs.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			for _, c := range pod.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					return
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("pod %s/%s not ready within %s", namespace, name, timeout)
}

func buildScanner(t *testing.T, cs kubernetes.Interface) *scanner.Scanner {
	t.Helper()
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	return scanner.New(cs, nil, m, config.ScannerConfig{
		Namespaces:   config.NamespaceConfig{},
		Certificates: config.CertificateConfig{Paths: []string{
			"/etc/certs/server.crt",
			"/etc/tls/tls.crt",
		}},
		Containers: config.ContainerConfig{Mode: "disabled"},
	})
}

// TestSecretsDiscovered verifies that all four Secrets in kcert-integration
// are found and classified into the correct status tier.
func TestSecretsDiscovered(t *testing.T) {
	cs := newClientset(t)
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := scanner.New(cs, nil, m, config.ScannerConfig{
		Namespaces:   config.NamespaceConfig{},
		Certificates: config.CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}},
		Containers:   config.ContainerConfig{Mode: "disabled"},
	})
	thresholds := result.Thresholds{WarningDays: 30, CriticalDays: 7}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := s.Scan(ctx)

	// Filter to kcert-integration namespace only
	var ns []result.CertificateResult
	for _, r := range results {
		if r.Namespace == integrationNamespace {
			ns = append(ns, r)
		}
	}

	if len(ns) < 4 {
		t.Fatalf("expected at least 4 certs in %s, got %d", integrationNamespace, len(ns))
	}

	// Build a map of sourceName → status
	statusByName := make(map[string]result.Status)
	for _, r := range ns {
		statusByName[r.SourceName] = thresholds.Classify(r.DaysLeft)
	}

	want := map[string]result.Status{
		"expired-secret/tls.crt":  result.StatusExpired,
		"critical-secret/tls.crt": result.StatusCritical,
		"warning-secret/tls.crt":  result.StatusWarning,
		"healthy-secret/tls.crt":  result.StatusOK,
	}
	for name, expectedStatus := range want {
		if got, ok := statusByName[name]; !ok {
			t.Errorf("secret %q not found in scan results", name)
		} else if got != expectedStatus {
			t.Errorf("secret %q: status = %q, want %q", name, got, expectedStatus)
		}
	}
}

// TestPodCertsDiscovered verifies container cert scanning finds certs
// in the kcert-fixture pod containers.
func TestPodCertsDiscovered(t *testing.T) {
	cs := newClientset(t)

	waitForPod(t, cs, integrationNamespace, "kcert-fixture", 2*time.Minute)

	// Build a ContainerReader backed by the real cluster
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = home + "/.kube/config"
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.Precedence = filepath.SplitList(kubeconfig)
	}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		t.Fatalf("build rest config: %v", err)
	}
	reader := scanner.NewContainerReader(cs, restCfg)
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := scanner.New(cs, reader, m, config.ScannerConfig{
		Namespaces:   config.NamespaceConfig{},
		Certificates: config.CertificateConfig{Paths: []string{
			"/etc/certs/server.crt",
			"/etc/tls/tls.crt",
		}},
		Containers: config.ContainerConfig{Mode: "exec"},
	})
	thresholds := result.Thresholds{WarningDays: 30, CriticalDays: 7}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	results := s.Scan(ctx)

	var ns []result.CertificateResult
	for _, r := range results {
		if r.Namespace == integrationNamespace && r.SourceType == result.SourceContainer {
			ns = append(ns, r)
		}
	}

	if len(ns) == 0 {
		t.Fatalf("expected container certs in %s, got 0", integrationNamespace)
	}

	// Verify at least one expired and one healthy cert is present
	statusCounts := map[result.Status]int{}
	for _, r := range ns {
		statusCounts[thresholds.Classify(r.DaysLeft)]++
	}
	t.Logf("container cert status counts: %v", statusCounts)

	if statusCounts[result.StatusExpired] == 0 {
		t.Error("expected at least one EXPIRED container cert")
	}
	if statusCounts[result.StatusOK] == 0 {
		t.Error("expected at least one OK container cert")
	}
}

// TestCertificateFingerprintUnique verifies that two distinct certs have different fingerprints.
func TestCertificateFingerprintUnique(t *testing.T) {
	cs := newClientset(t)
	m := metrics.NewWithRegistry(prometheus.NewRegistry())
	s := scanner.New(cs, nil, m, config.ScannerConfig{
		Namespaces:   config.NamespaceConfig{},
		Certificates: config.CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}},
		Containers:   config.ContainerConfig{Mode: "disabled"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := s.Scan(ctx)

	fps := map[string]int{}
	for _, r := range results {
		if r.Namespace == integrationNamespace && r.Fingerprint != "" {
			fps[r.Fingerprint]++
		}
	}
	if len(fps) < 4 {
		t.Errorf("expected at least 4 unique fingerprints in %s, got %d", integrationNamespace, len(fps))
	}
}
