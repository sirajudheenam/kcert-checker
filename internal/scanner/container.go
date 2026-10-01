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

// ListFiles expands a glob pattern inside the container by running
// `ls -1 <pattern>` and returns the matched paths, one per line.
// Returns an empty slice (not an error) when the pattern matches nothing —
// most containers won't have most configured directories.
func (r *ContainerReader) ListFiles(
	ctx context.Context,
	namespace, pod, container, pattern string,
) ([]string, error) {
	data, err := r.exec(ctx, namespace, pod, container, []string{"ls", "-1", pattern})
	if err != nil {
		// ls exits non-zero when nothing matches; treat that as "no files".
		return nil, nil
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
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
	return r.exec(ctx, namespace, pod, container, []string{"cat", path})
}

// exec runs an arbitrary command inside a container via the Kubernetes pod/exec
// SPDY subresource and returns combined stdout bytes.
func (r *ContainerReader) exec(
	ctx context.Context,
	namespace, pod, container string,
	command []string,
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
			Command:   command,
			Stdout:    true,
			Stderr:    true,
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
			"exec %s/%s/%s %v: %w: %s",
			namespace, pod, container, command,
			err, strings.TrimSpace(stderr.String()),
		)
	}

	return stdout.Bytes(), nil
}
