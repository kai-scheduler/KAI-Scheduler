// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"reflect"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func lookupByObject(byObject map[client.Object]cache.ByObject, target client.Object) (cache.ByObject, bool) {
	targetType := reflect.TypeOf(target)
	for obj, cfg := range byObject {
		if reflect.TypeOf(obj) == targetType {
			return cfg, true
		}
	}
	return cache.ByObject{}, false
}

func TestNewCacheOptions(t *testing.T) {
	const namespace = "kai-scheduler"
	opts := NewCacheOptions(namespace)

	if opts.DefaultTransform == nil {
		t.Fatal("expected DefaultTransform to be set")
	}

	podWithManagedFields := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: namespace,
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "kube-controller-manager"},
			},
		},
	}
	transformed, err := opts.DefaultTransform(podWithManagedFields)
	if err != nil {
		t.Fatalf("DefaultTransform returned unexpected error: %v", err)
	}
	transformedPod, ok := transformed.(*corev1.Pod)
	if !ok {
		t.Fatalf("expected DefaultTransform to return *corev1.Pod, got %T", transformed)
	}
	if len(transformedPod.ManagedFields) != 0 {
		t.Fatalf("expected ManagedFields to be stripped, got %v", transformedPod.ManagedFields)
	}

	expectedDefaultNamespaces := map[string]cache.Config{namespace: {}}
	if !reflect.DeepEqual(opts.DefaultNamespaces, expectedDefaultNamespaces) {
		t.Fatalf("expected DefaultNamespaces to be %v, got %v", expectedDefaultNamespaces, opts.DefaultNamespaces)
	}

	if len(opts.ByObject) != 1 {
		t.Fatalf("expected 1 object override in ByObject, got %d", len(opts.ByObject))
	}

	saByObj, found := lookupByObject(opts.ByObject, &corev1.ServiceAccount{})
	if !found {
		t.Fatal("expected *corev1.ServiceAccount to be configured in ByObject")
	}
	expectedAllNamespaces := map[string]cache.Config{cache.AllNamespaces: {}}
	if !reflect.DeepEqual(saByObj.Namespaces, expectedAllNamespaces) {
		t.Fatalf("expected *corev1.ServiceAccount Namespaces to be %v, got %v", expectedAllNamespaces, saByObj.Namespaces)
	}
}

func TestNewCacheOptionsDefaultNamespaceFallback(t *testing.T) {
	opts := NewCacheOptions("")

	expectedDefaultNamespaces := map[string]cache.Config{constants.DefaultKAINamespace: {}}
	if !reflect.DeepEqual(opts.DefaultNamespaces, expectedDefaultNamespaces) {
		t.Fatalf("expected empty namespace to fall back to %v, got %v", expectedDefaultNamespaces, opts.DefaultNamespaces)
	}
}
