// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package kubeflow

import (
	"testing"

	"github.com/stretchr/testify/assert"

	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestGetPodGroupMetadata(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"xReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{
						"replicas": int64(2),
					},
					"Worker": map[string]interface{}{
						"replicas": int64(4),
					},
				},
			},
		},
	}
	pod := &v1.Pod{}

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	grouper := NewKubeflowDistributedGrouper(defaultGrouper)
	podGroupMetadata, err := grouper.GetPodGroupMetadata(owner, pod,
		"xReplicaSpecs", []string{"Master"})

	assert.Nil(t, err)
	assert.Equal(t, "test_kind", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "test_version", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(6), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadataOnQueueFromOwner(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label":  "test_value",
					queueLabelKey: "my-queue",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"xReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{
						"replicas": int64(2),
					},
					"Worker": map[string]interface{}{
						"replicas": int64(4),
					},
				},
			},
		},
	}
	pod := &v1.Pod{}

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	grouper := NewKubeflowDistributedGrouper(defaultGrouper)
	podGroupMetadata, err := grouper.GetPodGroupMetadata(owner, pod,
		"xReplicaSpecs", []string{"Master"})

	assert.Nil(t, err)
	assert.Equal(t, "my-queue", podGroupMetadata.Queue)
	assert.Equal(t, int32(6), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadataRunPolicy(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
					"project":    "my-proj",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"runPolicy": map[string]interface{}{
					"schedulingPolicy": map[string]interface{}{
						"minAvailable": int64(5),
					},
				},
				"xReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{
						"replicas": int64(0),
					},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Labels: map[string]string{
				"project": "my-proj",
			},
		},
	}

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	grouper := NewKubeflowDistributedGrouper(defaultGrouper)
	podGroupMetadata, err := grouper.GetPodGroupMetadata(owner, pod, "xReplicaSpecs", []string{"Master"})

	assert.Nil(t, err)
	assert.Equal(t, int32(5), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_NoPods(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
					"project":    "my-proj",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"xReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{
						"replicas": int64(0),
					},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Labels: map[string]string{
				"project": "my-proj",
			},
		},
	}

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	grouper := NewKubeflowDistributedGrouper(defaultGrouper)
	_, err := grouper.GetPodGroupMetadata(owner, pod, "xReplicaSpecs", []string{"Master"})

	assert.NotNil(t, err)
}

func TestGetPodGroupMetadata_NoMastersAllowed(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
					"project":    "my-proj",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"xReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{
						"replicas": int64(2),
					},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Labels: map[string]string{
				"project": "my-proj",
			},
		},
	}

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	grouper := NewKubeflowDistributedGrouper(defaultGrouper)
	podGroupMetadata, err := grouper.GetPodGroupMetadata(owner, pod,
		"xReplicaSpecs", []string{})

	assert.Nil(t, err)
	assert.Equal(t, int32(2), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_NoMastersForbiden(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "test_kind",
			"apiVersion": "test_version",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
					"project":    "my-proj",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"xReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{
						"replicas": int64(2),
					},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Labels: map[string]string{
				"project": "my-proj",
			},
		},
	}

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	grouper := NewKubeflowDistributedGrouper(defaultGrouper)
	_, err := grouper.GetPodGroupMetadata(owner, pod,
		"xReplicaSpecs", []string{"Master"})

	assert.NotNil(t, err)
}
