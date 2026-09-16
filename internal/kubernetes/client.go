// Package kubernetes wraps the client-go Kubernetes client with the
// connection modes supported by kcert-checker (in-cluster, kubeconfig, auto).
package kubernetes

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/sirajudheenam/kcert-checker/internal/config"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client holds the Kubernetes clientset and the REST config.
// Both are needed: Clientset for API calls, Config for pod exec streams.
type Client struct {
	Clientset kubernetes.Interface
	Config    *rest.Config
}

// NewClient builds a Kubernetes client using the mode in cfg.
func NewClient(cfg config.KubernetesConfig) (*Client, error) {
	kubeConfig, err := buildConfig(cfg)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf(
			"create Kubernetes clientset: %w",
			err,
		)
	}

	return &Client{
		Clientset: clientset,
		Config:    kubeConfig,
	}, nil
}

// buildConfig dispatches to the appropriate config builder based on mode.
func buildConfig(cfg config.KubernetesConfig) (*rest.Config, error) {
	switch cfg.Mode {

	case "in-cluster":
		return buildInClusterConfig()

	case "kubeconfig":
		return buildKubeconfigConfig(cfg)

	case "auto":
		return buildAutoConfig(cfg)

	default:
		return nil, fmt.Errorf(
			"unsupported Kubernetes mode %q; must be auto, in-cluster, or kubeconfig",
			cfg.Mode,
		)
	}
}

// buildInClusterConfig uses the service account token mounted by Kubernetes
// into every Pod at /var/run/secrets/kubernetes.io/serviceaccount.
func buildInClusterConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf(
			"create in-cluster Kubernetes config: %w",
			err,
		)
	}

	return config, nil
}

// buildKubeconfigConfig reads a kubeconfig file from disk and optionally
// overrides the active context.
func buildKubeconfigConfig(
	cfg config.KubernetesConfig,
) (*rest.Config, error) {

	var kubeconfigPath string

	if v := os.Getenv("KUBECONFIG"); v != "" {
		kubeconfigPath = v
		log.Printf("kubeconfig: using KUBECONFIG env variable %q", kubeconfigPath)
	}

	if kubeconfigPath == "" {
		kubeconfigPath = cfg.Kubeconfig
		if kubeconfigPath != "" {
			log.Printf("kubeconfig: using config.yaml value %q", kubeconfigPath)
		}
	}

	if kubeconfigPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine home directory: %w", err)
		}
		kubeconfigPath = filepath.Join(home, ".kube", "config")
		log.Printf("kubeconfig: using default path %q", kubeconfigPath)
	}

	if _, err := os.Stat(kubeconfigPath); err != nil {
		return nil, fmt.Errorf("kubeconfig %q not accessible: %w", kubeconfigPath, err)
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{
		ExplicitPath: kubeconfigPath,
	}

	overrides := &clientcmd.ConfigOverrides{}

	if cfg.Context != "" {
		overrides.CurrentContext = cfg.Context
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		overrides,
	)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf(
			"load kubeconfig %q with context %q: %w",
			kubeconfigPath,
			cfg.Context,
			err,
		)
	}

	return restConfig, nil
}

// buildAutoConfig tries in-cluster first (detects the standard Kubernetes
// environment variables injected into every Pod), then falls back to
// kubeconfig for local development.
func buildAutoConfig(
	cfg config.KubernetesConfig,
) (*rest.Config, error) {

	if isRunningInKubernetes() {
		log.Println("Kubernetes environment detected: using in-cluster configuration")
		return buildInClusterConfig()
	}
	log.Println("Kubernetes environment not detected: using kubeconfig")
	return buildKubeconfigConfig(cfg)
}

// isRunningInKubernetes checks for the environment variables injected into
// Kubernetes Pods.
func isRunningInKubernetes() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != "" &&
		os.Getenv("KUBERNETES_SERVICE_PORT") != ""
}
