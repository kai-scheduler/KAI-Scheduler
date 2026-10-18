// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
)

func TestGPUSharingConfigMapIdentification(t *testing.T) {
	prefixes := map[types.UID]client.ObjectKey{"pod-uid": {Namespace: "workload", Name: "worker-shared-gpu"}}
	for _, test := range []struct {
		name, namespace, ownerKind, ownerUID string
		matches                              bool
	}{
		{"worker-shared-gpu-0", "workload", "Pod", "pod-uid", true},
		{"worker-shared-gpu-12-evar", "workload", "Pod", "pod-uid", true},
		{"worker-shared-gpu-0", "other", "Pod", "pod-uid", false},
		{"worker-shared-gpu-0", "workload", "Pod", "stale-uid", false},
		{"worker-shared-gpu-0", "workload", "Deployment", "pod-uid", false},
		{"worker-shared-gpu-0-copy", "workload", "Pod", "pod-uid", false},
		{"worker-shared-gpu-01", "workload", "Pod", "pod-uid", false},
		{"unrelated-0", "workload", "Pod", "pod-uid", false},
	} {
		t.Run(test.name+test.namespace+test.ownerUID+test.ownerKind, func(t *testing.T) {
			cm := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: test.name, Namespace: test.namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: test.ownerKind, UID: types.UID(test.ownerUID)}}}}
			require.Equal(t, test.matches, isGPUSharingConfigMap(cm, prefixes))
		})
	}
}

func TestFinalMigrationWaitsForOldBinderIncludingTerminatingPods(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	one := int32(1)
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "binder", Namespace: "kai", Generation: 2}, Spec: appsv1.DeploymentSpec{Replicas: &one, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "binder", Image: "new"}}}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}}
	old := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "kai", Labels: map[string]string{"app": "binder"}, Finalizers: []string{"hold"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "binder", Image: "old"}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(deployment).WithObjects(deployment, old).Build()
	require.NoError(t, c.Delete(context.Background(), old))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := WaitForBinderUpgrade(ctx, c, "kai", "new")
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	// A map created after the first scan remains eligible for the final scan.
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "workload", UID: "worker-uid", Annotations: map[string]string{"runai/shared-gpu-configmap": "worker-shared-gpu"}}}
	require.NoError(t, c.Create(context.Background(), pod))
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "worker-shared-gpu-0", Namespace: "workload", UID: "map-uid", Labels: map[string]string{"existing": "label"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID}}}, Data: map[string]string{"value": "preserved"}, BinaryData: map[string][]byte{"binary": []byte("preserved")}}
	require.NoError(t, c.Create(context.Background(), cm))
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(old), old))
	old.Finalizers = nil
	require.NoError(t, c.Update(context.Background(), old))
	require.NoError(t, WaitForBinderUpgrade(context.Background(), c, "kai", "new"))
	for i := 0; i < 2; i++ {
		require.NoError(t, MigrateGPUSharingConfigMaps(context.Background(), c))
	}
	stored := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(cm), stored))
	require.Equal(t, cm.UID, stored.UID)
	require.Equal(t, cm.OwnerReferences, stored.OwnerReferences)
	require.Equal(t, cm.Data, stored.Data)
	require.Equal(t, cm.BinaryData, stored.BinaryData)
	require.Equal(t, "label", stored.Labels["existing"])
	require.Equal(t, "true", stored.Labels[gpusharingconfigmap.GPUSharingConfigMapLabel])
}
