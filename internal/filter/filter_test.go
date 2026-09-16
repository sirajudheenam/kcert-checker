package filter

import "testing"

// TestNamespace verifies exact-match and glob-pattern exclusion for namespace names.
func TestNamespace(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"kube-system", []string{"kube-system"}, true},
		{"kube-public", []string{"kube-system", "kube-public"}, true},
		{"default", []string{"kube-system", "kube-public"}, false},
		{"openshift-etcd", []string{"openshift-*"}, true},
		{"openshift-apiserver", []string{"openshift-*"}, true},
		{"monitoring", []string{"openshift-*"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Namespace(tt.name, tt.patterns)
			if got != tt.want {
				t.Errorf("Namespace(%q, %v) = %v, want %v", tt.name, tt.patterns, got, tt.want)
			}
		})
	}
}

// TestNamespaceEmptyPatterns verifies that an empty exclusion list never skips a namespace.
func TestNamespaceEmptyPatterns(t *testing.T) {
	if Namespace("anything", nil) {
		t.Error("expected false for empty patterns")
	}
}

// TestContainer verifies sidecar and init-container patterns used in typical configs.
func TestContainer(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"istio-proxy", []string{"istio-proxy", "linkerd-proxy"}, true},
		{"linkerd-proxy", []string{"istio-proxy", "linkerd-proxy"}, true},
		{"app", []string{"istio-proxy", "linkerd-proxy"}, false},
		{"init-certs", []string{"init-*"}, true},
		{"init-migrate", []string{"init-*"}, true},
		// "app-init" does not start with "init-", so the prefix glob should not match
		{"app-init", []string{"init-*"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Container(tt.name, tt.patterns)
			if got != tt.want {
				t.Errorf("Container(%q, %v) = %v, want %v", tt.name, tt.patterns, got, tt.want)
			}
		})
	}
}

// TestSecret verifies Helm release secrets (common cluster noise) and exact matches.
func TestSecret(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"sh.helm.release.v1.prometheus.v1", []string{"sh.helm.release.*"}, true},
		{"sh.helm.release.v1.loki.v1", []string{"sh.helm.release.*"}, true},
		{"my-tls-secret", []string{"sh.helm.release.*"}, false},
		{"exact-secret", []string{"exact-secret"}, true},
		{"other-secret", []string{"exact-secret"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Secret(tt.name, tt.patterns)
			if got != tt.want {
				t.Errorf("Secret(%q, %v) = %v, want %v", tt.name, tt.patterns, got, tt.want)
			}
		})
	}
}

// TestPod verifies glob and exact exclusion for pod names.
func TestPod(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"debug-pod", []string{"debug-*"}, true},
		{"debug-app", []string{"debug-*"}, true},
		{"test-runner", []string{"test-*"}, true},
		{"my-app", []string{"debug-*", "test-*"}, false},
		{"broken-app", []string{"broken-app"}, true},
		{"healthy-app", []string{"broken-app"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Pod(tt.name, tt.patterns)
			if got != tt.want {
				t.Errorf("Pod(%q, %v) = %v, want %v", tt.name, tt.patterns, got, tt.want)
			}
		})
	}
}

// TestPodEmptyPatterns verifies that an empty exclusion list never skips a pod.
func TestPodEmptyPatterns(t *testing.T) {
	if Pod("anything", nil) {
		t.Error("expected false for empty patterns")
	}
}
