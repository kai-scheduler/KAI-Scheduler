// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package ray

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schedulingv2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

var (
	autoScalingRayCluster = &unstructured.Unstructured{
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
				"enableInTreeAutoscaling": true,
				"headGroupSpec":           map[string]interface{}{},
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

	nonAutoScalingRayCluster = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "RayCluster",
			"apiVersion": "ray.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "test_ray_cluster",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label":                 "test_value",
					"ray.io/priority-class-name": "ray_train_priority_class",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"headGroupSpec": map[string]interface{}{},
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

	rayClusterWithSuspendedWorkers = &unstructured.Unstructured{
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
				"headGroupSpec": map[string]interface{}{},
				"workerGroupSpecs": []interface{}{
					map[string]interface{}{
						"replicas":    int64(3),
						"minReplicas": int64(2),
					},
					map[string]interface{}{
						"suspended":   true,
						"replicas":    int64(3),
						"minReplicas": int64(1),
					},
				},
			},
		},
	}

	rayClusterWithNumOfHosts = &unstructured.Unstructured{
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
				"headGroupSpec": map[string]interface{}{},
				"workerGroupSpecs": []interface{}{
					map[string]interface{}{
						"numOfHosts":  int64(2),
						"replicas":    int64(3),
						"minReplicas": int64(2),
					},
					map[string]interface{}{
						"numOfHosts":  int64(3),
						"replicas":    int64(3),
						"minReplicas": int64(1),
					},
				},
			},
		},
	}

	rayClusterWithNamedWorkerGroups = &unstructured.Unstructured{
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
				"headGroupSpec": map[string]interface{}{},
				"workerGroupSpecs": []interface{}{
					map[string]interface{}{
						"groupName":   "gpu-workers",
						"replicas":    int64(3),
						"minReplicas": int64(2),
					},
					map[string]interface{}{
						"groupName":   "cpu-workers",
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

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(autoScalingRayCluster).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(autoScalingRayCluster, pod)

	assert.Nil(t, err)
	assert.Equal(t, "RayCluster", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "ray.io/v1alpha1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, autoScalingRayCluster.GetName(), podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	// MinAvailable: 1 (head) + 2 (group0 minReplicas) + 1 (group1 minReplicas) = 4
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_RayCluster_NonAutoScaling(t *testing.T) {
	pod := &v1.Pod{}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(nonAutoScalingRayCluster).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(nonAutoScalingRayCluster, pod)

	assert.Nil(t, err)
	assert.Equal(t, "RayCluster", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "ray.io/v1alpha1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, nonAutoScalingRayCluster.GetName(), podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 2, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "ray_train_priority_class", podGroupMetadata.PriorityClassName)
	// MinAvailable: 1 (head) + 2 (group0 minReplicas) + 1 (group1 minReplicas) = 4
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_RayCluster_SuspendedWorkers(t *testing.T) {
	pod := &v1.Pod{}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(rayClusterWithSuspendedWorkers).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(rayClusterWithSuspendedWorkers, pod)

	assert.Nil(t, err)
	assert.Equal(t, "RayCluster", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "ray.io/v1alpha1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, rayClusterWithSuspendedWorkers.GetName(), podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	// MinAvailable: 1 (head) + 2 (group0 minReplicas) + 0 (group1 suspended) = 3
	assert.Equal(t, int32(3), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_RayCluster_NumOfHosts(t *testing.T) {
	pod := &v1.Pod{}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(rayClusterWithNumOfHosts).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(rayClusterWithNumOfHosts, pod)

	assert.Nil(t, err)
	assert.Equal(t, "RayCluster", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "ray.io/v1alpha1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, rayClusterWithNumOfHosts.GetName(), podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 1, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(8), podGroupMetadata.MinAvailable)
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

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(autoScalingRayCluster).Build()
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
	// MinAvailable: 1 (head) + 2 (group0 minReplicas) + 1 (group1 minReplicas) = 4
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_RayJob_v1(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "RayJob",
			"apiVersion": "ray.io/v1",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": autoScalingRayCluster.GetNamespace(),
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"status": map[string]interface{}{
				"rayClusterName": autoScalingRayCluster.GetName(),
			},
		},
	}

	pod := &v1.Pod{}

	rayClusterCopy := autoScalingRayCluster.DeepCopy()
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
	// MinAvailable: 1 (head) + 2 (group0 minReplicas) + 1 (group1 minReplicas) = 4
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)
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

	rayClusterCopy := autoScalingRayCluster.DeepCopy()
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
	// MinAvailable: 1 (head) + 2 (group0 minReplicas) + 1 (group1 minReplicas) = 4
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_RayCluster_SubGroups_Default(t *testing.T) {
	pod := &v1.Pod{}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(autoScalingRayCluster).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(autoScalingRayCluster, pod)

	assert.Nil(t, err)
	// MinAvailable: 1 (head) + 2 (group0 minReplicas) + 1 (group1 minReplicas) = 4
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)

	// Verify subgroups: head + 2 worker groups
	assert.Equal(t, 3, len(podGroupMetadata.SubGroups))
	assert.Equal(t, "headgroup", podGroupMetadata.SubGroups[0].Name)
	assert.Equal(t, int32(1), podGroupMetadata.SubGroups[0].MinAvailable)
	assert.Equal(t, "worker-group-0", podGroupMetadata.SubGroups[1].Name)
	assert.Equal(t, int32(2), podGroupMetadata.SubGroups[1].MinAvailable)
	assert.Equal(t, "worker-group-1", podGroupMetadata.SubGroups[2].Name)
	assert.Equal(t, int32(1), podGroupMetadata.SubGroups[2].MinAvailable)
}

func TestGetPodGroupMetadata_RayCluster_SubGroups_NamedWorkerGroups(t *testing.T) {
	pod := &v1.Pod{}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(rayClusterWithNamedWorkerGroups).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(rayClusterWithNamedWorkerGroups, pod)

	assert.Nil(t, err)
	// MinAvailable: 1 (head) + 2 (gpu-workers minReplicas) + 1 (cpu-workers minReplicas) = 4
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)

	// Verify subgroups use custom names
	assert.Equal(t, 3, len(podGroupMetadata.SubGroups))
	assert.Equal(t, "headgroup", podGroupMetadata.SubGroups[0].Name)
	assert.Equal(t, int32(1), podGroupMetadata.SubGroups[0].MinAvailable)
	assert.Equal(t, "gpu-workers", podGroupMetadata.SubGroups[1].Name)
	assert.Equal(t, int32(2), podGroupMetadata.SubGroups[1].MinAvailable)
	assert.Equal(t, "cpu-workers", podGroupMetadata.SubGroups[2].Name)
	assert.Equal(t, int32(1), podGroupMetadata.SubGroups[2].MinAvailable)
}

func TestGetPodGroupMetadata_RayCluster_SuspendedWorkers_SubGroups(t *testing.T) {
	pod := &v1.Pod{}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(rayClusterWithSuspendedWorkers).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(rayClusterWithSuspendedWorkers, pod)

	assert.Nil(t, err)
	// MinAvailable: 1 (head) + 2 (group0 minReplicas) + 0 (group1 suspended) = 3
	assert.Equal(t, int32(3), podGroupMetadata.MinAvailable)

	// Suspended worker group should not create a subgroup
	// Only head + first (non-suspended) worker group
	assert.Equal(t, 2, len(podGroupMetadata.SubGroups))
	assert.Equal(t, "headgroup", podGroupMetadata.SubGroups[0].Name)
	assert.Equal(t, int32(1), podGroupMetadata.SubGroups[0].MinAvailable)
	assert.Equal(t, "worker-group-0", podGroupMetadata.SubGroups[1].Name)
	assert.Equal(t, int32(2), podGroupMetadata.SubGroups[1].MinAvailable)
}

// Tests for backwards compatibility with existing PodGroups

func buildSchemeWithPodGroup() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = schedulingv2alpha2.AddToScheme(s)
	return s
}

func TestGetPodGroupMetadata_BackwardsCompatibility_NoPodGroupExists(t *testing.T) {
	// When no PodGroup exists, should use new subgroup logic
	pod := &v1.Pod{}
	s := buildSchemeWithPodGroup()

	client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(autoScalingRayCluster).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(autoScalingRayCluster, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)
	// Should have subgroups
	assert.Equal(t, 3, len(podGroupMetadata.SubGroups))
}

func TestGetPodGroupMetadata_BackwardsCompatibility_ExistingPodGroupWithoutSubGroups(t *testing.T) {
	// When PodGroup exists without SubGroups (legacy workload), should use flat logic
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: autoScalingRayCluster.GetNamespace(),
		},
	}
	s := buildSchemeWithPodGroup()

	// Calculate the expected PodGroup name using the same logic as the defaultgrouper
	expectedPGName := "pg-" + autoScalingRayCluster.GetName() + "-" + string(autoScalingRayCluster.GetUID())

	// Create an existing PodGroup without SubGroups (legacy format)
	existingPodGroup := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expectedPGName,
			Namespace: autoScalingRayCluster.GetNamespace(),
		},
		Spec: schedulingv2alpha2.PodGroupSpec{
			MinMember: 4,
			// No SubGroups - legacy format
		},
	}

	client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(autoScalingRayCluster, existingPodGroup).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	// Debug: check if shouldUseSubGroups returns the expected value
	shouldUse := rayGrouper.shouldUseSubGroups(autoScalingRayCluster.GetNamespace(), expectedPGName)
	assert.False(t, shouldUse, "shouldUseSubGroups should return false for legacy PodGroup without SubGroups")

	podGroupMetadata, err := grouper.GetPodGroupMetadata(autoScalingRayCluster, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)
	// Should NOT have subgroups - backwards compatibility
	assert.Equal(t, 0, len(podGroupMetadata.SubGroups), "Expected no subgroups for legacy workload, PodGroup name: %s", expectedPGName)
}

func TestGetPodGroupMetadata_BackwardsCompatibility_ExistingPodGroupWithSubGroups(t *testing.T) {
	// When PodGroup exists with SubGroups (already migrated), should use new logic
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: autoScalingRayCluster.GetNamespace(),
		},
	}
	s := buildSchemeWithPodGroup()

	// Calculate the expected PodGroup name using the same logic as the defaultgrouper
	expectedPGName := "pg-" + autoScalingRayCluster.GetName() + "-" + string(autoScalingRayCluster.GetUID())

	// Create an existing PodGroup with SubGroups (already migrated)
	existingPodGroup := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expectedPGName,
			Namespace: autoScalingRayCluster.GetNamespace(),
		},
		Spec: schedulingv2alpha2.PodGroupSpec{
			MinMember: 4,
			SubGroups: []schedulingv2alpha2.SubGroup{
				{Name: "headgroup", MinMember: 1},
				{Name: "worker-group-0", MinMember: 2},
				{Name: "worker-group-1", MinMember: 1},
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(autoScalingRayCluster, existingPodGroup).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	grouper := NewRayClusterGrouper(rayGrouper)

	podGroupMetadata, err := grouper.GetPodGroupMetadata(autoScalingRayCluster, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(4), podGroupMetadata.MinAvailable)
	// Should have subgroups - already migrated
	assert.Equal(t, 3, len(podGroupMetadata.SubGroups))
}

func TestShouldUseSubGroups_NoPodGroupExists(t *testing.T) {
	s := buildSchemeWithPodGroup()
	client := fake.NewClientBuilder().WithScheme(s).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))

	result := rayGrouper.shouldUseSubGroups("test-ns", "non-existent-pg")

	assert.True(t, result)
}

func TestShouldUseSubGroups_PodGroupExistsWithoutSubGroups(t *testing.T) {
	s := buildSchemeWithPodGroup()
	existingPodGroup := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pg",
			Namespace: "test-ns",
		},
		Spec: schedulingv2alpha2.PodGroupSpec{
			MinMember: 2,
		},
	}
	client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(existingPodGroup).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))

	result := rayGrouper.shouldUseSubGroups("test-ns", "test-pg")

	assert.False(t, result)
}

func TestShouldUseSubGroups_PodGroupExistsWithSubGroups(t *testing.T) {
	s := buildSchemeWithPodGroup()
	existingPodGroup := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pg",
			Namespace: "test-ns",
		},
		Spec: schedulingv2alpha2.PodGroupSpec{
			MinMember: 2,
			SubGroups: []schedulingv2alpha2.SubGroup{
				{Name: "headgroup", MinMember: 1},
			},
		},
	}
	client := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(existingPodGroup).Build()
	rayGrouper := NewRayGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))

	result := rayGrouper.shouldUseSubGroups("test-ns", "test-pg")

	assert.True(t, result)
}
