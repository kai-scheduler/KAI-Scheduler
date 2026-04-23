// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package job

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schedulingv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"

	"github.com/stretchr/testify/assert"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestGetPodGroupMetadata_MultiPodJobSharesPodGroup(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1234-5678",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"parallelism": int64(2),
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: "test_name-4kgrb",
			Labels: map[string]string{
				"random": "abcd",
			},
		},
	}
	pod2 := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: "test_name-77t4m",
			Labels: map[string]string{
				"random": "abcd",
			},
		},
	}

	scheme := runtime.NewScheme()
	err := schedulingv2.AddToScheme(scheme)
	if err != nil {
		t.Fail()
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects().Build()

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobGrouper := NewK8sJobGrouper(client, defaultGrouper, false)

	podGroupMetadata, err := jobGrouper.GetPodGroupMetadata(owner, pod)
	podGroupMetadata2, err2 := jobGrouper.GetPodGroupMetadata(owner, pod2)

	assert.Nil(t, err)
	assert.Nil(t, err2)
	assert.Equal(t, "pg-test_name-1234-5678", podGroupMetadata.Name)
	assert.Equal(t, podGroupMetadata.Name, podGroupMetadata2.Name)

	assert.Equal(t, "test_kind", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "test_version", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1234-5678", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
}

func TestGetPodGroupMetadata_LegacyPodGroup(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1234-5678",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name:      "test_name-4kgrb",
			Namespace: "test_namespace",
			Labels: map[string]string{
				"random": "abcd",
			},
		},
	}

	var testResources = []runtime.Object{
		&schedulingv2.PodGroup{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PodGroup",
				APIVersion: "scheduling.run.ai/v2alpha2",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        "pg-test_name-4kgrb-1234-5678",
				Namespace:   owner.GetNamespace(),
				UID:         owner.GetUID(),
				Annotations: owner.GetAnnotations(),
				Labels:      owner.GetLabels(),
			},
			Spec:   schedulingv2.PodGroupSpec{},
			Status: schedulingv2.PodGroupStatus{},
		},
	}

	scheme := runtime.NewScheme()
	err := schedulingv2.AddToScheme(scheme)
	if err != nil {
		t.Fail()
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(testResources...).Build()

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobGrouper := NewK8sJobGrouper(client, defaultGrouper, true)

	podGroupMetadata, err := jobGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "pg-test_name-4kgrb-1234-5678", podGroupMetadata.Name)

	assert.Equal(t, "test_kind", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "test_version", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1234-5678", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
}

func TestGetPodGroupMetadata_LegacyDisabledPodGroup(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1234-5678",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name:      "test_name-4kgrb",
			Namespace: "test_namespace",
			Labels: map[string]string{
				"random": "abcd",
			},
		},
	}

	var testResources = []runtime.Object{
		&schedulingv2.PodGroup{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PodGroup",
				APIVersion: "scheduling.run.ai/v2alpha2",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        "pg-test_name-4kgrb-1234-5678",
				Namespace:   owner.GetNamespace(),
				UID:         owner.GetUID(),
				Annotations: owner.GetAnnotations(),
				Labels:      owner.GetLabels(),
			},
			Spec:   schedulingv2.PodGroupSpec{},
			Status: schedulingv2.PodGroupStatus{},
		},
	}

	scheme := runtime.NewScheme()
	err := schedulingv2.AddToScheme(scheme)
	if err != nil {
		t.Fail()
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(testResources...).Build()

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobGrouper := NewK8sJobGrouper(client, defaultGrouper, false)

	podGroupMetadata, err := jobGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "pg-test_name-1234-5678", podGroupMetadata.Name)

	assert.Equal(t, "test_kind", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "test_version", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1234-5678", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
}

func TestGetPodGroupMetadata_LegacyNotFound(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1234-5678",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name:      "test_name-4kgrb",
			Namespace: "test_namespace",
			Labels: map[string]string{
				"random": "abcd",
			},
		},
	}

	var testResources = []runtime.Object{}

	scheme := runtime.NewScheme()
	err := schedulingv2.AddToScheme(scheme)
	if err != nil {
		t.Fail()
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(testResources...).Build()

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobGrouper := NewK8sJobGrouper(client, defaultGrouper, true)

	podGroupMetadata, err := jobGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "pg-test_name-1234-5678", podGroupMetadata.Name)

	assert.Equal(t, "test_kind", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "test_version", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1234-5678", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
}

func TestGetPodGroupMetadata_MinMemberOverride(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Job",
			"apiVersion": "batch/v1",
			"metadata": map[string]interface{}{
				"name":      "test_job",
				"namespace": "test_namespace",
				"uid":       "1234-5678",
				"labels":    map[string]interface{}{},
				"annotations": map[string]interface{}{
					"kai.scheduler/batch-min-member": "3",
				},
			},
			"spec": map[string]interface{}{
				"parallelism": int64(5),
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name:      "test_job-abc",
			Namespace: "test_namespace",
		},
	}

	scheme := runtime.NewScheme()
	err := schedulingv2.AddToScheme(scheme)
	assert.NoError(t, err)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobGrouper := NewK8sJobGrouper(client, defaultGrouper, false)

	podGroupMetadata, err := jobGrouper.GetPodGroupMetadata(owner, pod)
	assert.NoError(t, err)
	assert.Equal(t, int32(3), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_MinMemberOverrideInvalid(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
	}{
		{"non-numeric", "abc"},
		{"non-int", "1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "Job",
					"apiVersion": "batch/v1",
					"metadata": map[string]interface{}{
						"name":      "test_job",
						"namespace": "test_namespace",
						"uid":       "1234-5678",
						"labels":    map[string]interface{}{},
						"annotations": map[string]interface{}{
							"kai.scheduler/batch-min-member": "invalid",
						},
					},
					"spec": map[string]interface{}{},
				},
			}
			pod := &v1.Pod{
				ObjectMeta: v12.ObjectMeta{
					Name:      "test_job-abc",
					Namespace: "test_namespace",
				},
			}

			scheme := runtime.NewScheme()
			err := schedulingv2.AddToScheme(scheme)
			assert.NoError(t, err)

			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
			jobGrouper := NewK8sJobGrouper(client, defaultGrouper, false)

			podGroupMetadata, err := jobGrouper.GetPodGroupMetadata(owner, pod)
			assert.Error(t, err)
			assert.Nil(t, podGroupMetadata)
		})
	}
}

func TestGetPodGroupMetadata_MixedUpgradeOnlyOnePodHasLegacy(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Job",
			"apiVersion": "batch/v1",
			"metadata": map[string]interface{}{
				"name":      "test_job",
				"namespace": "test_namespace",
				"uid":       "1234-5678",
				"labels":    map[string]interface{}{},
			},
			"spec": map[string]interface{}{
				"parallelism": int64(2),
			},
		},
	}
	podWithLegacy := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{Name: "test_job-aaaaa", Namespace: "test_namespace"},
	}
	podWithoutLegacy := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{Name: "test_job-bbbbb", Namespace: "test_namespace"},
	}

	testResources := []runtime.Object{
		&schedulingv2.PodGroup{
			TypeMeta:   metav1.TypeMeta{Kind: "PodGroup", APIVersion: "scheduling.run.ai/v2alpha2"},
			ObjectMeta: metav1.ObjectMeta{Name: "pg-test_job-aaaaa-1234-5678", Namespace: "test_namespace"},
		},
	}

	scheme := runtime.NewScheme()
	assert.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(testResources...).Build()

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobGrouper := NewK8sJobGrouper(client, defaultGrouper, true)

	legacyMeta, err := jobGrouper.GetPodGroupMetadata(owner, podWithLegacy)
	assert.NoError(t, err)
	assert.Equal(t, "pg-test_job-aaaaa-1234-5678", legacyMeta.Name)

	newMeta, err := jobGrouper.GetPodGroupMetadata(owner, podWithoutLegacy)
	assert.NoError(t, err)
	assert.Equal(t, "pg-test_job-1234-5678", newMeta.Name)
}

func TestGetPodGroupMetadata_LegacySearchDisabledIgnoresLegacyPodGroup(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Job",
			"apiVersion": "batch/v1",
			"metadata": map[string]interface{}{
				"name":      "test_job",
				"namespace": "test_namespace",
				"uid":       "1234-5678",
				"labels":    map[string]interface{}{},
			},
			"spec": map[string]interface{}{
				"parallelism": int64(2),
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{Name: "test_job-aaaaa", Namespace: "test_namespace"},
	}

	testResources := []runtime.Object{
		&schedulingv2.PodGroup{
			TypeMeta:   metav1.TypeMeta{Kind: "PodGroup", APIVersion: "scheduling.run.ai/v2alpha2"},
			ObjectMeta: metav1.ObjectMeta{Name: "pg-test_job-aaaaa-1234-5678", Namespace: "test_namespace"},
		},
	}

	scheme := runtime.NewScheme()
	assert.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(testResources...).Build()

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobGrouper := NewK8sJobGrouper(client, defaultGrouper, false)

	meta, err := jobGrouper.GetPodGroupMetadata(owner, pod)
	assert.NoError(t, err)
	assert.Equal(t, "pg-test_job-1234-5678", meta.Name)
}

func TestGetPodGroupMetadata_RegularPodGroup(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1234-5678",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name:      "test_name-4kgrb",
			Namespace: "test_namespace",
			Labels: map[string]string{
				"random": "abcd",
			},
		},
	}

	scheme := runtime.NewScheme()
	err := schedulingv2.AddToScheme(scheme)
	if err != nil {
		t.Fail()
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects().Build()

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobGrouper := NewK8sJobGrouper(client, defaultGrouper, false)

	podGroupMetadata, err := jobGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "pg-test_name-1234-5678", podGroupMetadata.Name)

	assert.Equal(t, "test_kind", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "test_version", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1234-5678", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
}
