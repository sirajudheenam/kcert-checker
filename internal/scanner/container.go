// Package scanner implements certificate discovery across Pod containers
// and Kubernetes Secrets.
package scanner

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ContainerReader reads files from inside running containers using the
// Kubernetes pod/exec subresource (equivalent to `kubectl exec -- cat <path>`).
// Requires the pods/exec create verb in the ClusterRole.
type ContainerReader struct {
	Clientset kubernetes.Interface
	Config    *rest.Config
}

// NewContainerReader creates a ContainerReader backed by the given clientset
// and REST config. The REST config is needed to open the SPDY exec stream.
func NewContainerReader(
	clientset kubernetes.Interface,
	config *rest.Config,
) *ContainerReader {
	return &ContainerReader{
		Clientset: clientset,
		Config:    config,
	}
}

// ReadFile runs `cat <path>` inside the specified container and returns the
// raw bytes. Returns an error if the file does not exist or the exec fails —
// callers treat a missing file as normal (most containers won't have most
// configured cert paths).
func (r *ContainerReader) ReadFile(
	ctx context.Context,
	namespace string,
	pod string,
	container string,
	path string,
) ([]byte, error) {

	req := r.Clientset.
		CoreV1().
		RESTClient().
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(pod).
		SubResource("exec")

	req.VersionedParams(
		&corev1.PodExecOptions{
			Container: container,
			Command: []string{
				"cat",
				path,
			},
			Stdout: true,
			Stderr: true,
		},
		scheme.ParameterCodec,
	)

	// SPDY is the WebSocket-like multiplexed protocol used by kubectl exec.
	executor, err := remotecommand.NewSPDYExecutor(
		r.Config,
		"POST",
		req.URL(),
	)
	if err != nil {
		return nil, fmt.Errorf("create pod executor: %w", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err = executor.StreamWithContext(
		ctx,
		remotecommand.StreamOptions{
			Stdout: &stdout,
			Stderr: &stderr,
		},
	)

	if err != nil {
		return nil, fmt.Errorf(
			"exec %s/%s/%s path %s: %w: %s",
			namespace,
			pod,
			container,
			path,
			err,
			strings.TrimSpace(stderr.String()),
		)
	}

	return stdout.Bytes(), nil
}
