// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobset

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schedulingv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

const (
	queueLabelKey              = "kai.scheduler/queue"
	nodePoolLabelKey           = "kai.scheduler/node-pool"
	jobSetLabelJobIndexForTest = "jobset.sigs.k8s.io/job-index"
)

// baseJobSet creates a minimal JobSet unstructured object for testing.
func baseJobSet(name, namespace, uid string, replicatedJobs []map[string]interface{}) *unstructured.Unstructured {
	// Convert []map[string]interface{} to []interface{} for unstructured.NestedSlice
	rjSlice := make([]interface{}, len(replicatedJobs))
	for i, rj := range replicatedJobs {
		rjSlice[i] = rj
	}

	js := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "JobSet",
			"apiVersion": "jobset.x-k8s.io/v1alpha2",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"uid":       uid,
				"labels": map[string]interface{}{
					"test-label": "test-value",
				},
			},
			"spec": map[string]interface{}{
				"replicatedJobs": rjSlice,
			},
		},
	}
	return js
}

// replicatedJob creates a replicatedJob entry for JobSet spec.
func replicatedJob(name string, replicas int64, parallelism int64) map[string]interface{} {
	return map[string]interface{}{
		"name":     name,
		"replicas": replicas,
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"parallelism": parallelism,
			},
		},
	}
}

// podWithJobSetLabels creates a Pod with JobSet labels.
func podWithJobSetLabels(name, namespace, replicatedJobName, jobIndex string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				jobSetLabelJobSetName:        "my-jobset",
				jobSetLabelReplicatedJobName: replicatedJobName,
				jobSetLabelJobIndexForTest:   jobIndex,
				queueLabelKey:                "test-queue",
			},
		},
	}
}

func TestGetPodGroupMetadata_Basic(t *testing.T) {
	jobSet := baseJobSet("my-jobset", "default", "jobset-uid-123", []map[string]interface{}{
		replicatedJob("worker", 2, 4),
	})
	pod := podWithJobSetLabels("my-jobset-worker-0-pod-abc", "default", "worker", "0")

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)

	require.NoError(t, err)
	// PodGroup name: pg-<jobset-name>-<jobset-uid>-<replicatedjob-name>
	assert.Equal(t, "pg-my-jobset-jobset-uid-123-worker", pgMeta.Name)
	// MinAvailable = replicas * parallelism
	assert.Equal(t, int32(8), pgMeta.MinAvailable)
	// DefaultGrouper fields preserved
	assert.Equal(t, "test-queue", pgMeta.Queue)
	assert.Equal(t, "JobSet", pgMeta.Owner.Kind)
	assert.Equal(t, "jobset.x-k8s.io/v1alpha2", pgMeta.Owner.APIVersion)
	assert.Equal(t, "my-jobset", pgMeta.Owner.Name)
	assert.Equal(t, "jobset-uid-123", string(pgMeta.Owner.UID))
}

func TestGetPodGroupMetadata_MultipleReplicatedJobs(t *testing.T) {
	jobSet := baseJobSet("multi-role-jobset", "default", "uid-456", []map[string]interface{}{
		replicatedJob("leader", 1, 1),
		replicatedJob("worker", 3, 8),
	})

	// Test leader pod
	leaderPod := podWithJobSetLabels("leader-pod", "default", "leader", "0")
	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	leaderMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, leaderPod)
	require.NoError(t, err)
	assert.Equal(t, "pg-multi-role-jobset-uid-456-leader", leaderMeta.Name)
	assert.Equal(t, int32(1), leaderMeta.MinAvailable)

	// Test worker pod (different replicatedJob)
	workerPod := podWithJobSetLabels("worker-pod", "default", "worker", "1")
	workerMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, workerPod)
	require.NoError(t, err)
	assert.Equal(t, "pg-multi-role-jobset-uid-456-worker", workerMeta.Name)
	assert.Equal(t, int32(24), workerMeta.MinAvailable)

	// Different job-index should map to the same PodGroup (one per replicatedJob)
	workerPod2 := podWithJobSetLabels("worker-pod-2", "default", "worker", "2")
	workerMeta2, err := jobSetGrouper.GetPodGroupMetadata(jobSet, workerPod2)
	require.NoError(t, err)
	assert.Equal(t, "pg-multi-role-jobset-uid-456-worker", workerMeta2.Name)
	assert.Equal(t, workerMeta.Name, workerMeta2.Name)
}

func TestGetPodGroupMetadata_ParallelismDefaults(t *testing.T) {
	// Test: parallelism not specified -> defaults to 1
	jobSetNoParallelism := baseJobSet("no-parallelism", "default", "uid-789", []map[string]interface{}{
		{
			"name":     "worker",
			"replicas": int64(2),
			"template": map[string]interface{}{
				"spec": map[string]interface{}{}, // no parallelism
			},
		},
	})
	pod := podWithJobSetLabels("pod", "default", "worker", "0")

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSetNoParallelism, pod)
	require.NoError(t, err)
	// replicas=2, parallelism default=1 => minAvailable=2
	assert.Equal(t, int32(2), pgMeta.MinAvailable)
}

func TestGetPodGroupMetadata_ParallelismZeroOrNegative(t *testing.T) {
	// Test: parallelism = 0 -> defaults to 1
	jobSetZero := baseJobSet("zero-parallelism", "default", "uid-zero", []map[string]interface{}{
		replicatedJob("worker", 1, 0),
	})
	pod := podWithJobSetLabels("pod", "default", "worker", "0")

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSetZero, pod)
	require.NoError(t, err)
	assert.Equal(t, int32(1), pgMeta.MinAvailable)

	// Test: parallelism = -1 -> defaults to 1
	jobSetNegative := baseJobSet("negative-parallelism", "default", "uid-neg", []map[string]interface{}{
		replicatedJob("worker", 1, -1),
	})
	pgMeta2, err := jobSetGrouper.GetPodGroupMetadata(jobSetNegative, pod)
	require.NoError(t, err)
	assert.Equal(t, int32(1), pgMeta2.MinAvailable)
}

func TestGetPodGroupMetadata_NoReplicatedJobs(t *testing.T) {
	// Test: spec.replicatedJobs missing or empty -> defaults to parallelism 1
	jobSetEmpty := baseJobSet("empty-jobset", "default", "uid-empty", []map[string]interface{}{})
	pod := podWithJobSetLabels("pod", "default", "worker", "0")

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSetEmpty, pod)
	require.NoError(t, err)
	assert.Equal(t, int32(1), pgMeta.MinAvailable)
}

func TestGetPodGroupMetadata_ReplicatedJobNotFound(t *testing.T) {
	// Test: pod has replicatedJobName that doesn't exist in spec -> defaults to 1
	jobSet := baseJobSet("notfound", "default", "uid-nf", []map[string]interface{}{
		replicatedJob("worker", 1, 4),
	})
	pod := podWithJobSetLabels("pod", "default", "nonexistent", "0") // "nonexistent" not in spec

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)
	require.NoError(t, err)
	// Should still create PodGroup name, but MinAvailable defaults to 1
	assert.Equal(t, "pg-notfound-uid-nf-nonexistent", pgMeta.Name)
	assert.Equal(t, int32(1), pgMeta.MinAvailable)
}

func TestGetPodGroupMetadata_MissingJobSetName(t *testing.T) {
	jobSet := baseJobSet("", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 1, 2),
	})
	pod := podWithJobSetLabels("pod", "default", "worker", "0")

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	_, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing name or UID")
}

func TestGetPodGroupMetadata_MissingJobSetUID(t *testing.T) {
	jobSet := baseJobSet("my-jobset", "default", "", []map[string]interface{}{
		replicatedJob("worker", 1, 2),
	})
	pod := podWithJobSetLabels("pod", "default", "worker", "0")

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	_, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing name or UID")
}

func TestGetPodGroupMetadata_MissingReplicatedJobLabel(t *testing.T) {
	jobSet := baseJobSet("my-jobset", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 1, 2),
	})
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "default",
			Labels: map[string]string{
				jobSetLabelJobSetName: "my-jobset",
				// Missing jobSetLabelReplicatedJobName
				jobSetLabelJobIndexForTest: "0",
			},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	_, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), jobSetLabelReplicatedJobName)
}

func TestGetPodGroupMetadata_DefaultGrouperIntegration(t *testing.T) {
	// Verify that DefaultGrouper fields (queue, priority, labels) are preserved
	jobSet := baseJobSet("integrated", "test-ns", "uid-integ", []map[string]interface{}{
		replicatedJob("worker", 1, 3),
	})
	pod := podWithJobSetLabels("pod", "test-ns", "worker", "0")
	pod.Labels[queueLabelKey] = "custom-queue"

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)
	require.NoError(t, err)

	// JobSet-specific fields
	assert.Equal(t, "pg-integrated-uid-integ-worker", pgMeta.Name)
	assert.Equal(t, int32(3), pgMeta.MinAvailable)

	// DefaultGrouper fields should be preserved
	assert.Equal(t, "custom-queue", pgMeta.Queue)
	assert.Equal(t, "test-ns", pgMeta.Namespace)
	assert.Equal(t, "JobSet", pgMeta.Owner.Kind)
	assert.Equal(t, "jobset.x-k8s.io/v1alpha2", pgMeta.Owner.APIVersion)
}

func TestGetPodGroupMetadata_NameUniqueness(t *testing.T) {
	// Verify that PodGroup name is stable across job-index for the same replicatedJob
	jobSet := baseJobSet("unique", "default", "uid-unique", []map[string]interface{}{
		replicatedJob("worker", 2, 4),
	})

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	// Same replicatedJob, different jobIndex
	pod0 := podWithJobSetLabels("pod-0", "default", "worker", "0")
	pod1 := podWithJobSetLabels("pod-1", "default", "worker", "1")

	meta0, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod0)
	require.NoError(t, err)
	meta1, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod1)
	require.NoError(t, err)

	assert.Equal(t, meta0.Name, meta1.Name)
	assert.Equal(t, "pg-unique-uid-unique-worker", meta0.Name)
	assert.Equal(t, "pg-unique-uid-unique-worker", meta1.Name)
	// replicas=2, parallelism=4 => minAvailable=8
	assert.Equal(t, int32(8), meta0.MinAvailable)
	assert.Equal(t, int32(8), meta1.MinAvailable)
}

func TestGetPodGroupMetadata_CompletionsLessThanParallelism(t *testing.T) {
	// Test: completions < parallelism -> minAvailable = replicas * completions
	jobSet := baseJobSet("completions-test", "default", "uid-comp", []map[string]interface{}{
		{
			"name":     "worker",
			"replicas": int64(2),
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"parallelism": int64(4),
					"completions": int64(3), // completions < parallelism
				},
			},
		},
	})
	pod := podWithJobSetLabels("pod", "default", "worker", "0")

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)
	require.NoError(t, err)
	// replicas=2, min(parallelism=4, completions=3)=3 => minAvailable=6
	assert.Equal(t, int32(6), pgMeta.MinAvailable)
}

func TestGetPodGroupMetadata_CompletionsGreaterThanParallelism(t *testing.T) {
	// Test: completions > parallelism -> minAvailable = replicas * parallelism
	jobSet := baseJobSet("completions-gt", "default", "uid-comp-gt", []map[string]interface{}{
		{
			"name":     "worker",
			"replicas": int64(3),
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"parallelism": int64(2),
					"completions": int64(5), // completions > parallelism
				},
			},
		},
	})
	pod := podWithJobSetLabels("pod", "default", "worker", "0")

	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)
	require.NoError(t, err)
	// replicas=3, min(parallelism=2, completions=5)=2 => minAvailable=6
	assert.Equal(t, int32(6), pgMeta.MinAvailable)
}

func TestGetPodGroupMetadata_StartupPolicyOrderInOrder(t *testing.T) {
	jobSet := baseJobSet("inorder-jobset", "default", "uid-inorder", []map[string]interface{}{
		replicatedJob("leader", 1, 1),
		replicatedJob("worker", 2, 4),
	})
	jobSet.Object["spec"].(map[string]interface{})["startupPolicy"] = map[string]interface{}{
		"startupPolicyOrder": startupPolicyOrderInOrder,
	}

	leaderPod := podWithJobSetLabels("leader-pod", "default", "leader", "0")
	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	leaderMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, leaderPod)
	require.NoError(t, err)
	assert.Equal(t, "pg-inorder-jobset-uid-inorder-leader", leaderMeta.Name)
	assert.Equal(t, int32(1), leaderMeta.MinAvailable)

	workerPod := podWithJobSetLabels("worker-pod", "default", "worker", "0")
	workerMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, workerPod)
	require.NoError(t, err)
	assert.Equal(t, "pg-inorder-jobset-uid-inorder-worker", workerMeta.Name)
	assert.Equal(t, int32(8), workerMeta.MinAvailable)
}

func TestGetPodGroupMetadata_StartupPolicyOrderNotInOrder(t *testing.T) {
	jobSet := baseJobSet("anyorder-jobset", "default", "uid-anyorder", []map[string]interface{}{
		replicatedJob("leader", 1, 1),
		replicatedJob("worker", 2, 4),
	})
	jobSet.Object["spec"].(map[string]interface{})["startupPolicy"] = map[string]interface{}{
		"startupPolicyOrder": "AnyOrder",
	}

	leaderPod := podWithJobSetLabels("leader-pod", "default", "leader", "0")
	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	leaderMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, leaderPod)
	require.NoError(t, err)
	assert.Equal(t, "pg-anyorder-jobset-uid-anyorder", leaderMeta.Name)
	assert.Equal(t, int32(9), leaderMeta.MinAvailable)

	workerPod := podWithJobSetLabels("worker-pod", "default", "worker", "0")
	workerMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, workerPod)
	require.NoError(t, err)
	assert.Equal(t, "pg-anyorder-jobset-uid-anyorder", workerMeta.Name)
	assert.Equal(t, leaderMeta.Name, workerMeta.Name)
	assert.Equal(t, int32(9), workerMeta.MinAvailable)
}

func TestGetPodGroupMetadata_StartupPolicyOrderDefault(t *testing.T) {
	jobSet := baseJobSet("default-order-jobset", "default", "uid-default", []map[string]interface{}{
		replicatedJob("worker", 2, 4),
	})

	pod := podWithJobSetLabels("pod", "default", "worker", "0")
	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client)
	jobSetGrouper := NewJobSetGrouper(defaultGrouper)

	pgMeta, err := jobSetGrouper.GetPodGroupMetadata(jobSet, pod)
	require.NoError(t, err)
	assert.Equal(t, "pg-default-order-jobset-uid-default-worker", pgMeta.Name)
	assert.Equal(t, int32(8), pgMeta.MinAvailable)
}
