package config

import (
	"os"
	"path/filepath"
	"testing"
)

const validYAML = `
kubernetes:
  mode: "kubeconfig"
  kubeconfig: "/tmp/fake.kubeconfig"
scanner:
  certificates:
    paths:
      - /etc/tls/tls.crt
metrics:
  enabled: false
scan:
  interval_seconds: 60
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	// Create fake kubeconfig so Validate does not fail on Stat
	_ = os.WriteFile("/tmp/fake.kubeconfig", []byte("apiVersion: v1\nkind: Config\n"), 0600)
	return f.Name()
}

// TestLoadValidConfig verifies the happy path: a well-formed YAML file with all
// required fields loads without error and produces the expected field values.
func TestLoadValidConfig(t *testing.T) {
	path := writeTemp(t, validYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Kubernetes.Mode != "kubeconfig" {
		t.Errorf("mode = %q, want kubeconfig", cfg.Kubernetes.Mode)
	}
	if len(cfg.Scanner.Certificates.Paths) != 1 {
		t.Errorf("paths len = %d, want 1", len(cfg.Scanner.Certificates.Paths))
	}
}

// TestLoadMissingFile verifies Load returns an error when the file does not exist.
func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// TestLoadBadYAML verifies Load returns an error for structurally invalid YAML.
func TestLoadBadYAML(t *testing.T) {
	path := writeTemp(t, "{ this is: not: valid: yaml :")
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

// TestValidateDefaultMode verifies that an empty kubernetes.mode is defaulted to "auto".
func TestValidateDefaultMode(t *testing.T) {
	cfg := &Config{
		Scanner: ScannerConfig{
			Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Kubernetes.Mode != "auto" {
		t.Errorf("default mode = %q, want auto", cfg.Kubernetes.Mode)
	}
}

// TestValidateInvalidKubernetesMode verifies that an unrecognised mode value is rejected.
func TestValidateInvalidKubernetesMode(t *testing.T) {
	cfg := &Config{
		Kubernetes: KubernetesConfig{Mode: "magic"},
		Scanner:    ScannerConfig{Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid kubernetes.mode")
	}
}

// TestValidateKubeconfigModeRequiresPath verifies that mode=kubeconfig without
// a kubeconfig path is rejected. The path is required to avoid silently falling
// back to ~/.kube/config when the operator intended an explicit file.
func TestValidateKubeconfigModeRequiresPath(t *testing.T) {
	cfg := &Config{
		Kubernetes: KubernetesConfig{Mode: "kubeconfig"},
		Scanner:    ScannerConfig{Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when kubeconfig path is empty")
	}
}

// TestValidateMissingCertPaths verifies that a config with no certificate paths is rejected.
// An empty list would cause the scanner to never exec into any container.
func TestValidateMissingCertPaths(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when certificate paths are empty")
	}
}

// TestValidateDefaults verifies that zero-valued scan thresholds and container mode
// are replaced with sensible defaults so a minimal config is operational.
func TestValidateDefaults(t *testing.T) {
	cfg := &Config{
		Scanner: ScannerConfig{
			Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}},
		},
	}
	_ = cfg.Validate()
	if cfg.Scan.IntervalSeconds != 3600 {
		t.Errorf("IntervalSeconds = %d, want 3600", cfg.Scan.IntervalSeconds)
	}
	if cfg.Scan.WarningDays != 30 {
		t.Errorf("WarningDays = %d, want 30", cfg.Scan.WarningDays)
	}
	if cfg.Scan.CriticalDays != 7 {
		t.Errorf("CriticalDays = %d, want 7", cfg.Scan.CriticalDays)
	}
	if cfg.Scanner.Containers.Mode != "exec" {
		t.Errorf("containers.mode = %q, want exec", cfg.Scanner.Containers.Mode)
	}
}

// TestValidateContainerModeDisabled verifies that mode=disabled is explicitly accepted.
func TestValidateContainerModeDisabled(t *testing.T) {
	cfg := &Config{
		Scanner: ScannerConfig{
			Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}},
			Containers:   ContainerConfig{Mode: "disabled"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateContainerModeInvalid verifies that an unrecognised container mode is rejected.
func TestValidateContainerModeInvalid(t *testing.T) {
	cfg := &Config{
		Scanner: ScannerConfig{
			Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}},
			Containers:   ContainerConfig{Mode: "magic"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid containers.mode")
	}
}

// TestLoadBadUnmarshal verifies that YAML with correct syntax but wrong types
// (e.g. a list where a string is expected) causes an unmarshal error.
func TestLoadBadUnmarshal(t *testing.T) {
	// Valid YAML but wrong types cause yaml.v3 to return an error
	path := writeTemp(t, "kubernetes:\n  mode: [1, 2, 3]\n")
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for unmarshal failure")
	}
}

// TestValidateMetricsDefaults verifies that an enabled metrics block with empty
// listen address and path receives the standard defaults.
func TestValidateMetricsDefaults(t *testing.T) {
	cfg := &Config{
		Kubernetes: KubernetesConfig{},
		Metrics:    MetricsConfig{Enabled: true, ListenAddress: "", Path: ""},
		Scanner:    ScannerConfig{Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Metrics.ListenAddress != "0.0.0.0:8080" {
		t.Errorf("ListenAddress = %q, want 0.0.0.0:8080", cfg.Metrics.ListenAddress)
	}
	if cfg.Metrics.Path != "/metrics" {
		t.Errorf("Path = %q, want /metrics", cfg.Metrics.Path)
	}
}

// TestValidateMetricsCustomPath verifies that explicit listen address and path are preserved.
func TestValidateMetricsCustomPath(t *testing.T) {
	cfg := &Config{
		Metrics: MetricsConfig{Enabled: true, ListenAddress: ":9090", Path: "/prom"},
		Scanner: ScannerConfig{Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Metrics.ListenAddress != ":9090" {
		t.Errorf("ListenAddress = %q, want :9090", cfg.Metrics.ListenAddress)
	}
}

// TestValidateInClusterMode verifies that in-cluster mode is accepted without any kubeconfig path.
func TestValidateInClusterMode(t *testing.T) {
	cfg := &Config{
		Kubernetes: KubernetesConfig{Mode: "in-cluster"},
		Scanner:    ScannerConfig{Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error for in-cluster mode: %v", err)
	}
}

// TestValidateScanDefaults verifies that zero-value scan thresholds are replaced with defaults.
// This guards against a config that specifies the struct but leaves all numeric fields at zero.
func TestValidateScanDefaults(t *testing.T) {
	cfg := &Config{
		Scanner: ScannerConfig{
			Certificates: CertificateConfig{Paths: []string{"/etc/tls/tls.crt"}},
		},
		Scan: ScanConfig{IntervalSeconds: 0, WarningDays: 0, CriticalDays: 0},
	}
	_ = cfg.Validate()
	if cfg.Scan.IntervalSeconds != 3600 {
		t.Errorf("IntervalSeconds default = %d", cfg.Scan.IntervalSeconds)
	}
	if cfg.Scan.WarningDays != 30 {
		t.Errorf("WarningDays default = %d", cfg.Scan.WarningDays)
	}
	if cfg.Scan.CriticalDays != 7 {
		t.Errorf("CriticalDays default = %d", cfg.Scan.CriticalDays)
	}
}

// TestLoadValidationError verifies that Load propagates Validate errors (not just parse errors).
func TestLoadValidationError(t *testing.T) {
	// Valid YAML that passes unmarshal but fails Validate (no cert paths)
	path := writeTemp(t, "kubernetes:\n  mode: auto\n")
	_, err := Load(path)
	if err == nil {
		t.Error("expected validation error when cert paths are missing")
	}
}
