package kubernetes

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ListNamespaces returns all namespaces visible to the service account.
// The ClusterRole grants get/list on namespaces cluster-wide.
func (c *Client) ListNamespaces(ctx context.Context) ([]corev1.Namespace, error) {
	result, err := c.Clientset.CoreV1().
		Namespaces().
		List(ctx, metav1.ListOptions{})

	if err != nil {
		return nil, err
	}

	return result.Items, nil
}
