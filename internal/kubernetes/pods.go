package kubernetes

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ListPods returns all Pods in the given namespace.
// The ClusterRole grants get/list on pods cluster-wide.
func (c *Client) ListPods(
	ctx context.Context,
	namespace string,
) ([]corev1.Pod, error) {

	result, err := c.Clientset.CoreV1().
		Pods(namespace).
		List(ctx, metav1.ListOptions{})

	if err != nil {
		return nil, err
	}

	return result.Items, nil
}
