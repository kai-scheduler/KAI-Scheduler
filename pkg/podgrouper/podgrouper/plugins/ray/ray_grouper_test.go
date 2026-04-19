// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package ray

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

var (
	rayCluster = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "RayCluster",
			"apiVersion": "ray.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "test_ray_cluster",
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
				"headGroupSpec": map[string]interface{}{
					"replicas":    int64(3),
					"minReplicas": int64(2),
				},
				"workerGroupSpecs": []interface{}{
					map[string]interface{}{
						"replicas":    int64(3),
						"minReplicas": int64(2),
					},
					map[string]interface{}{
						"replicas":    int64(3),
						"minReplicas": int64(1),
					},
				},
			},
		},
	}
)

func TestGetPodGroupMetadata_RayCluster(t *testing.T) {
	pod := &v1.Pod{}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(rayCluster).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(rayCluster, pod)

	assert.Nil(t, err)
	assert.Equal(t, "RayCluster", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "ray.io/v1alpha1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, rayCluster.GetName(), podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(5), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_RayJob(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "RayJob",
			"apiVersion": "ray.io/v1alpha1",
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
			"status": map[string]interface{}{
				"rayClusterName": "test_ray_cluster",
			},
		},
	}

	pod := &v1.Pod{}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(rayCluster).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayJobGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "RayJob", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "ray.io/v1alpha1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(5), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_RayJob_v1(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "RayJob",
			"apiVersion": "ray.io/v1",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": rayCluster.GetNamespace(),
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"status": map[string]interface{}{
				"rayClusterName": rayCluster.GetName(),
			},
		},
	}

	pod := &v1.Pod{}

	rayClusterCopy := rayCluster.DeepCopy()
	rayClusterCopy.SetAPIVersion("ray.io/v1")

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(rayClusterCopy).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayJobGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "RayJob", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "ray.io/v1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(5), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_RayService(t *testing.T) {
	rayService := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "RayService",
			"apiVersion": "ray.io/v1alpha1",
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
			"status": map[string]interface{}{
				"activeServiceStatus": map[string]interface{}{
					"rayClusterName": "test_ray_cluster",
				},
			},
		},
	}

	rayClusterCopy := rayCluster.DeepCopy()
	rayClusterCopy.SetOwnerReferences([]metav1.OwnerReference{
		{
			Kind:       "RayService",
			APIVersion: "ray.io/v1alpha1",
			Name:       rayService.GetName(),
			UID:        rayService.GetUID(),
		},
	})

	pod := &v1.Pod{}
	pod.SetOwnerReferences([]metav1.OwnerReference{
		{
			Kind:       "RayCluster",
			APIVersion: "ray.io/v1alpha1",
			Name:       rayClusterCopy.GetName(),
			UID:        rayClusterCopy.GetUID(),
		},
	})

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(rayClusterCopy).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayServiceGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(rayService, pod)

	assert.Nil(t, err)
	assert.Equal(t, "RayService", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "ray.io/v1alpha1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(5), podGroupMetadata.MinAvailable)
}
