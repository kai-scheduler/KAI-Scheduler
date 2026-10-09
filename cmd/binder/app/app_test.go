// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newConfigMap(owners ...metav1.OwnerReference) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "settings",
			Namespace:       "team-a",
			Labels:          map[string]string{"app": "trainer"},
			OwnerReferences: owners,
			ManagedFields:   []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
		},
		Data:       map[string]string{"payload": "large"},
		BinaryData: map[string][]byte{"blob": []byte("large")},
	}
}

func TestCompactConfigMapDropsContentOfConfigMapsNotOwnedByPods(t *testing.T) {
	deploymentOwner := metav1.OwnerReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "trainer"}

	for name, configMap := range map[string]*corev1.ConfigMap{
		"no owner":         newConfigMap(),
		"deployment owner": newConfigMap(deploymentOwner),
	} {
		t.Run(name, func(t *testing.T) {
			transformed, err := compactConfigMap(configMap)
			require.NoError(t, err)

			expectedMeta := configMap.ObjectMeta.DeepCopy()
			expectedMeta.ManagedFields = nil
			require.Equal(t, &corev1.ConfigMap{ObjectMeta: *expectedMeta}, transformed)
		})
	}
}

func TestCompactConfigMapKeepsGPUSharingConfigMapsOwnedByPods(t *testing.T) {
	configMap := newConfigMap(metav1.OwnerReference{APIVersion: "v1", Kind: "Pod", Name: "worker-0", UID: "worker-0-uid"})

	transformed, err := compactConfigMap(configMap)
	require.NoError(t, err)
	require.Same(t, configMap, transformed)
}

func TestCompactConfigMapPassesThroughOtherObjects(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker-0"}}

	transformed, err := compactConfigMap(pod)
	require.NoError(t, err)
	require.Same(t, pod, transformed)
}
