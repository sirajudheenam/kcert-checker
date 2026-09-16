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

func TestListNamespacesEmpty(t *testing.T) {
	c := &Client{Clientset: fake.NewSimpleClientset(), Config: &rest.Config{}}
	namespaces, err := c.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(namespaces) != 0 {
		t.Errorf("expected 0 namespaces, got %d", len(namespaces))
	}
}

func TestListNamespacesWithItems(t *testing.T) {
	ns1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	ns2 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring"}}
	c := &Client{Clientset: fake.NewSimpleClientset(ns1, ns2), Config: &rest.Config{}}

	namespaces, err := c.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(namespaces) != 2 {
		t.Errorf("expected 2 namespaces, got %d", len(namespaces))
	}
}

func TestListNamespacesError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.Fake.PrependReactor("list", "namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("simulated API error")
		},
	)
	c := &Client{Clientset: clientset, Config: &rest.Config{}}
	_, err := c.ListNamespaces(context.Background())
	if err == nil {
		t.Error("expected error from simulated API failure")
	}
}
