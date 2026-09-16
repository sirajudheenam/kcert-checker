package kubernetes

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/rest"
)

func TestListPodsEmpty(t *testing.T) {
	c := &Client{Clientset: fake.NewSimpleClientset(), Config: &rest.Config{}}
	pods, err := c.ListPods(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 0 {
		t.Errorf("expected 0 pods, got %d", len(pods))
	}
}

func TestListPodsWithItems(t *testing.T) {
	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "default"}}
	pod2 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "default"}}
	pod3 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "other"}}
	c := &Client{Clientset: fake.NewSimpleClientset(pod1, pod2, pod3), Config: &rest.Config{}}

	pods, err := c.ListPods(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 2 {
		t.Errorf("expected 2 pods in default ns, got %d", len(pods))
	}
}

func TestListPodsError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.Fake.PrependReactor("list", "pods",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("simulated API error")
		},
	)
	c := &Client{Clientset: clientset, Config: &rest.Config{}}
	_, err := c.ListPods(context.Background(), "default")
	if err == nil {
		t.Error("expected error from simulated API failure")
	}
}
