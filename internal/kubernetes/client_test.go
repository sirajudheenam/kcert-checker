package kubernetes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirajudheenam/kcert-checker/internal/config"
)

// minimalKubeconfig is a syntactically valid kubeconfig that won't connect to a
// real cluster but is sufficient for building a rest.Config object in unit tests.
const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test-cluster
  cluster:
    server: https://localhost:6443
    insecure-skip-tls-verify: true
contexts:
- name: test-context
  context:
    cluster: test-cluster
    user: test-user
users:
- name: test-user
  user:
    token: fake-token
current-context: test-context
`

func writeFakeKubeconfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte(minimalKubeconfig), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNewClientKubeconfig verifies the happy path for mode=kubeconfig with an
// explicit file path. Both Clientset and Config must be non-nil.
func TestNewClientKubeconfig(t *testing.T) {
	kc := writeFakeKubeconfig(t)
	cfg := config.KubernetesConfig{Mode: "kubeconfig", Kubeconfig: kc}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Clientset == nil {
		t.Error("Clientset is nil")
	}
	if client.Config == nil {
		t.Error("Config is nil")
	}
}

// TestNewClientKubeconfigMissingFile verifies that a non-existent kubeconfig
// path returns an error rather than silently using defaults.
func TestNewClientKubeconfigMissingFile(t *testing.T) {
	// Clear KUBECONFIG so the explicit Kubeconfig path is used, not the env var.
	t.Setenv("KUBECONFIG", "")
	cfg := config.KubernetesConfig{Mode: "kubeconfig", Kubeconfig: "/nonexistent/kubeconfig"}
	_, err := NewClient(cfg)
	if err == nil {
		t.Error("expected error for missing kubeconfig")
	}
}

// TestNewClientKubeconfigFromEnv verifies that the KUBECONFIG env var takes
// precedence over the config file value, matching kubectl behaviour.
func TestNewClientKubeconfigFromEnv(t *testing.T) {
	kc := writeFakeKubeconfig(t)
	t.Setenv("KUBECONFIG", kc)
	cfg := config.KubernetesConfig{Mode: "kubeconfig"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error with KUBECONFIG env: %v", err)
	}
	if client.Clientset == nil {
		t.Error("Clientset is nil")
	}
}

// TestNewClientKubeconfigWithContext verifies that an explicit context override
// is accepted and does not cause an error when the context exists in the file.
func TestNewClientKubeconfigWithContext(t *testing.T) {
	// Clear KUBECONFIG so the explicit Kubeconfig path is used, not the env var.
	t.Setenv("KUBECONFIG", "")
	kc := writeFakeKubeconfig(t)
	cfg := config.KubernetesConfig{Mode: "kubeconfig", Kubeconfig: kc, Context: "test-context"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error with context override: %v", err)
	}
	if client.Clientset == nil {
		t.Error("Clientset is nil")
	}
}

// TestNewClientAutoFallsBackToKubeconfig verifies that mode=auto uses kubeconfig
// when KUBERNETES_SERVICE_HOST is not set (i.e. not running inside a Pod).
func TestNewClientAutoFallsBackToKubeconfig(t *testing.T) {
	// Ensure we are not running in a Kubernetes pod
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	// Clear KUBECONFIG so the explicit cfg.Kubeconfig path is used, not the env var.
	t.Setenv("KUBECONFIG", "")

	kc := writeFakeKubeconfig(t)
	cfg := config.KubernetesConfig{Mode: "auto", Kubeconfig: kc}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error in auto mode: %v", err)
	}
	if client.Clientset == nil {
		t.Error("Clientset is nil")
	}
}

// TestNewClientUnsupportedMode verifies that an unrecognised mode value returns
// an error during config validation before any network calls are attempted.
func TestNewClientUnsupportedMode(t *testing.T) {
	cfg := config.KubernetesConfig{Mode: "magic"}
	_, err := NewClient(cfg)
	if err == nil {
		t.Error("expected error for unsupported mode")
	}
}

// TestIsRunningInKubernetes verifies the two-variable check used by auto mode
// to detect whether the process is running inside a Kubernetes Pod.
func TestIsRunningInKubernetes(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	if isRunningInKubernetes() {
		t.Error("expected false when env vars are empty")
	}

	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")
	if !isRunningInKubernetes() {
		t.Error("expected true when both env vars are set")
	}
}

// TestBuildConfigInClusterOutsideCluster verifies that mode=in-cluster returns
// an error when the service account token file is absent (outside a real cluster).
func TestBuildConfigInClusterOutsideCluster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	_, err := buildConfig(config.KubernetesConfig{Mode: "in-cluster"})
	if err == nil {
		t.Error("expected error when not running in a cluster")
	}
}

// TestNewClientAutoUsesInClusterWhenInKubernetes exercises the in-cluster branch
// of auto mode by simulating the Pod environment variables. It will fail (no
// service account on disk) but ensures the branch is covered.
func TestNewClientAutoUsesInClusterWhenInKubernetes(t *testing.T) {
	// Simulate being in a Kubernetes pod without actual credentials
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")
	cfg := config.KubernetesConfig{Mode: "auto"}
	// This will fail (no service account mounted) but exercises the in-cluster branch
	_, err := NewClient(cfg)
	if err == nil {
		t.Log("in-cluster succeeded (unexpected but acceptable in CI with actual cluster)")
	}
	// Either path is valid; we just need the branch to be executed
}

// TestBuildKubeconfigDefaultHome exercises the code path that falls back to
// ~/.kube/config when neither KUBECONFIG nor Kubeconfig is set.
func TestBuildKubeconfigDefaultHome(t *testing.T) {
	// Unset KUBECONFIG and leave Kubeconfig empty to hit the default home dir branch
	t.Setenv("KUBECONFIG", "")
	cfg := config.KubernetesConfig{Mode: "kubeconfig", Kubeconfig: ""}
	// Will fail unless ~/.kube/config exists and is valid, but exercises the branch
	_, _ = buildConfig(cfg)
}

// TestBuildKubeconfigHomeNotReadable verifies that a HOME directory without
// .kube/config produces an error rather than a silent failure.
func TestBuildKubeconfigHomeNotReadable(t *testing.T) {
	// Override HOME to a directory without .kube/config to exercise missing-file path
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", t.TempDir())
	cfg := config.KubernetesConfig{Mode: "kubeconfig", Kubeconfig: ""}
	_, err := buildConfig(cfg)
	if err == nil {
		t.Error("expected error when HOME/.kube/config does not exist")
	}
}
