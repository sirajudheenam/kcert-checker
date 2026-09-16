// Package config loads and validates the kcert-checker YAML configuration file.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration structure.
type Config struct {
	Kubernetes KubernetesConfig `yaml:"kubernetes"`
	Scanner    ScannerConfig    `yaml:"scanner"`
	Metrics    MetricsConfig    `yaml:"metrics"`
	Scan       ScanConfig       `yaml:"scan"`
}

// KubernetesConfig controls how the Kubernetes client is initialised.
type KubernetesConfig struct {
	// Mode is "auto" (default), "in-cluster", or "kubeconfig".
	// "auto" detects in-cluster by checking for KUBERNETES_SERVICE_HOST env var.
	Mode       string `yaml:"mode"`
	Kubeconfig string `yaml:"kubeconfig"`
	Context    string `yaml:"context"`
}

// ScannerConfig controls what the scanner looks for.
type ScannerConfig struct {
	Certificates CertificateConfig `yaml:"certificates"`
	Containers   ContainerConfig   `yaml:"containers"`
	Namespaces   NamespaceConfig   `yaml:"namespaces"`
	Secrets      SecretConfig      `yaml:"secrets"`
	Pods         PodConfig         `yaml:"pods"`
}

// CertificateConfig lists the file paths checked inside each container.
type CertificateConfig struct {
	Paths []string `yaml:"paths"`
}

// ContainerConfig controls which container types are included in the scan.
type ContainerConfig struct {
	IncludeInitContainers bool     `yaml:"include_init_containers"`
	Exclude               []string `yaml:"exclude"`
	// Mode is "exec" (default) to scan via pod/exec, or "disabled" to skip
	// container file scanning entirely (use when pods/exec RBAC is not allowed).
	Mode string `yaml:"mode"`
}

// NamespaceConfig controls which namespaces are scanned.
type NamespaceConfig struct {
	Exclude []string `yaml:"exclude"`
}

// SecretConfig controls which Secrets are scanned.
type SecretConfig struct {
	Exclude []string `yaml:"exclude"`
}

// PodConfig controls which Pods are scanned.
type PodConfig struct {
	Exclude []string `yaml:"exclude"`
}

// MetricsConfig controls the Prometheus metrics HTTP endpoint.
type MetricsConfig struct {
	Enabled       bool   `yaml:"enabled"`
	ListenAddress string `yaml:"listen_address"`
	Path          string `yaml:"path"`
}

// ScanConfig controls the periodic scan schedule and status thresholds.
type ScanConfig struct {
	// IntervalSeconds must be > 0; zero is replaced by the default (3600).
	// Values that are too small will hammer the API server with exec calls.
	IntervalSeconds int `yaml:"interval_seconds"`
	WarningDays     int `yaml:"warning_days"`
	// CriticalDays must be < WarningDays; zero is replaced by the default (7).
	// The scanner uses DaysLeft < CriticalDays as the CRITICAL threshold,
	// so a zero value would make everything CRITICAL.
	CriticalDays int `yaml:"critical_days"`
}

// Load reads and parses the YAML config file at path, then validates it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate applies defaults and checks required fields.
func (c *Config) Validate() error {
	if c.Kubernetes.Mode == "" {
		c.Kubernetes.Mode = "auto"
	}

	switch c.Kubernetes.Mode {
	case "auto", "in-cluster", "kubeconfig":
	default:
		return fmt.Errorf(
			"invalid kubernetes.mode %q: must be auto, in-cluster, or kubeconfig",
			c.Kubernetes.Mode,
		)
	}

	if c.Kubernetes.Mode == "kubeconfig" && c.Kubernetes.Kubeconfig == "" {
		return fmt.Errorf("kubernetes.kubeconfig is required when mode is kubeconfig")
	}

	if c.Metrics.Enabled {
		if c.Metrics.ListenAddress == "" {
			c.Metrics.ListenAddress = "0.0.0.0:8080"
		}
		if c.Metrics.Path == "" {
			c.Metrics.Path = "/metrics"
		}
	}

	// Zero or negative values for scan timing are treated as "not set" and
	// replaced with safe defaults to prevent accidental hammering or silent gaps.
	if c.Scan.IntervalSeconds <= 0 {
		c.Scan.IntervalSeconds = 3600
	}
	if c.Scan.WarningDays <= 0 {
		c.Scan.WarningDays = 30
	}
	if c.Scan.CriticalDays <= 0 {
		c.Scan.CriticalDays = 7
	}

	switch c.Scanner.Containers.Mode {
	case "", "exec":
		c.Scanner.Containers.Mode = "exec"
	case "disabled":
	default:
		return fmt.Errorf(
			"invalid scanner.containers.mode %q: must be exec or disabled",
			c.Scanner.Containers.Mode,
		)
	}

	if len(c.Scanner.Certificates.Paths) == 0 {
		return fmt.Errorf("scanner.certificates.paths must contain at least one path")
	}

	return nil
}
