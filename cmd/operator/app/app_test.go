// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	policyv1 "k8s.io/api/policy/v1"
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

	expectedScopedObjects := []client.Object{
		&corev1.Pod{},
		&coordinationv1.Lease{},
		&discoveryv1.EndpointSlice{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
		&appsv1.Deployment{},
		&appsv1.DaemonSet{},
		&corev1.Service{},
		&policyv1.PodDisruptionBudget{},
	}

	if len(opts.ByObject) != len(expectedScopedObjects) {
		t.Fatalf("expected %d scoped object types in ByObject, got %d", len(expectedScopedObjects), len(opts.ByObject))
	}

	expectedNamespaces := map[string]cache.Config{namespace: {}}
	for _, target := range expectedScopedObjects {
		byObj, found := lookupByObject(opts.ByObject, target)
		if !found {
			t.Errorf("expected %T to be configured in ByObject", target)
			continue
		}
		if !reflect.DeepEqual(byObj.Namespaces, expectedNamespaces) {
			t.Errorf("expected %T Namespaces to be %v, got %v", target, expectedNamespaces, byObj.Namespaces)
		}
	}

	if _, found := lookupByObject(opts.ByObject, &corev1.ServiceAccount{}); found {
		t.Errorf("expected *corev1.ServiceAccount to remain unscoped in ByObject")
	}
}
