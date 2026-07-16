// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobset

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	schedulingv2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

// baseJobSet creates a minimal JobSet unstructured object for testing.
func baseJobSet(name, namespace, uid string, replicatedJobs []map[string]interface{}) *unstructured.Unstructured {
	rjSlice := make([]interface{}, len(replicatedJobs))
	for i, rj := range replicatedJobs {
		rjSlice[i] = rj
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "JobSet",
			"apiVersion": "jobset.x-k8s.io/v1alpha2",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"uid":       uid,
			},
			"spec": map[string]interface{}{
				"replicatedJobs": rjSlice,
			},
		},
	}
}

func replicatedJob(name string, replicas, parallelism int64) map[string]interface{} {
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

// replicatedJobWithTemplateAnnotation adds the batch-min-member annotation to
// the Job template metadata.
func replicatedJobWithTemplateAnnotation(name string, replicas, parallelism int64, annotationValue string) map[string]interface{} {
	rj := replicatedJob(name, replicas, parallelism)
	rj["template"].(map[string]interface{})["metadata"] = map[string]interface{}{
		"annotations": map[string]interface{}{
			constants.MinMemberOverrideKey: annotationValue,
		},
	}
	return rj
}

func setStartupPolicy(js *unstructured.Unstructured, order string) {
	js.Object["spec"].(map[string]interface{})["startupPolicy"] = map[string]interface{}{
		"startupPolicyOrder": order,
	}
}

func podWithJobSetLabels(name, namespace, replicatedJobName, jobIndex string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				jobSetLabelReplicatedJobName: replicatedJobName,
				jobSetLabelJobIndex:          jobIndex,
				queueLabelKey:                "test-queue",
			},
		},
	}
}

func newJobSetGrouper(t *testing.T, existingObjects ...client.Object) *JobSetGrouper {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, schedulingv2.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingObjects...).Build()
	return NewJobSetGrouper(c, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, c))
}

// findSubGroup returns the SubGroup with the given name, or nil.
func findSubGroup(meta *podgroup.Metadata, name string) *podgroup.SubGroupMetadata {
	for _, sg := range meta.SubGroups {
		if sg.Name == name {
			return sg
		}
	}
	return nil
}

func TestSingleReplicatedJobSingleReplica(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 1, 4),
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)

	assert.Equal(t, "pg-js-uid", meta.Name)
	require.NotNil(t, meta.MinSubGroup)
	// AnyOrder is the JobSet default → root MinSubGroup = len(replicatedJobs) = 1.
	assert.Equal(t, int32(1), *meta.MinSubGroup)

	require.Len(t, meta.SubGroups, 2)
	parent := findSubGroup(meta, "worker")
	require.NotNil(t, parent)
	require.NotNil(t, parent.MinSubGroup)
	assert.Equal(t, int32(1), *parent.MinSubGroup)
	assert.Nil(t, parent.Parent)

	leaf := findSubGroup(meta, "worker-replica-0")
	require.NotNil(t, leaf)
	require.NotNil(t, leaf.Parent)
	assert.Equal(t, "worker", *leaf.Parent)
	assert.Equal(t, int32(4), leaf.MinAvailable)
	assert.Contains(t, leaf.PodsReferences, "p")
}

func TestSingleReplicatedJobMultipleReplicas(t *testing.T) {
	replicasCount := 3
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", int64(replicasCount), 4),
	})
	pod := podWithJobSetLabels("p", "default", "worker", "2")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)

	// 1 parent + 3 leaves.
	require.Len(t, meta.SubGroups, 1+replicasCount)
	parent := findSubGroup(meta, "worker")
	require.NotNil(t, parent)
	require.NotNil(t, parent.MinSubGroup)
	assert.Equal(t, int32(replicasCount), *parent.MinSubGroup)

	for i := 0; i < replicasCount; i++ {
		leaf := findSubGroup(meta, fmt.Sprintf(replicasSubgroupNameFormat, "worker", strconv.Itoa(i)))
		require.NotNil(t, leaf, "leaf worker-replica-%d", i)
		assert.Equal(t, int32(4), leaf.MinAvailable)
		require.NotNil(t, leaf.Parent)
		assert.Equal(t, "worker", *leaf.Parent)
	}
	leaf := findSubGroup(meta, "worker-replica-2")
	require.NotNil(t, leaf)
	assert.Contains(t, leaf.PodsReferences, "p")
}

func TestMultipleReplicatedJobsAnyOrder(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("leader", 1, 1),
		replicatedJob("worker", 2, 4),
	})
	setStartupPolicy(js, startupPolicyOrderAnyOrder)
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)

	require.NotNil(t, meta.MinSubGroup)
	assert.Equal(t, int32(2), *meta.MinSubGroup, "root MinSubGroup = len(replicatedJobs) for AnyOrder")
}

func TestStartupPolicyInOrderRootMinSubGroup(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("leader", 1, 1),
		replicatedJob("worker", 2, 4),
	})
	setStartupPolicy(js, startupPolicyOrderInOrder)
	pod := podWithJobSetLabels("p", "default", "leader", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	require.NotNil(t, meta.MinSubGroup)
	assert.Equal(t, int32(1), *meta.MinSubGroup)
}

func TestJobSetAnnotationOverridesRoot(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("a", 1, 1),
		replicatedJob("b", 1, 1),
		replicatedJob("c", 1, 1),
	})
	setStartupPolicy(js, startupPolicyOrderAnyOrder)
	js.SetAnnotations(map[string]string{constants.MinMemberOverrideKey: "2"})
	pod := podWithJobSetLabels("p", "default", "a", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	require.NotNil(t, meta.MinSubGroup)
	assert.Equal(t, int32(2), *meta.MinSubGroup)
}

func TestJobSetAnnotationClampedToReplicatedJobCount(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("a", 1, 1),
		replicatedJob("b", 1, 1),
	})
	js.SetAnnotations(map[string]string{constants.MinMemberOverrideKey: "9"})
	pod := podWithJobSetLabels("p", "default", "a", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	require.NotNil(t, meta.MinSubGroup)
	assert.Equal(t, int32(2), *meta.MinSubGroup, "clamped to len(replicatedJobs)")
}

func TestJobSetAnnotationWinsOverInOrder(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("a", 1, 1),
		replicatedJob("b", 1, 1),
	})
	setStartupPolicy(js, startupPolicyOrderInOrder)
	js.SetAnnotations(map[string]string{constants.MinMemberOverrideKey: "2"})
	pod := podWithJobSetLabels("p", "default", "a", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	require.NotNil(t, meta.MinSubGroup)
	assert.Equal(t, int32(2), *meta.MinSubGroup)
}

func TestJobSetAnnotationUnparseable(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("a", 1, 1),
	})
	js.SetAnnotations(map[string]string{constants.MinMemberOverrideKey: "not-an-int"})
	pod := podWithJobSetLabels("p", "default", "a", "0")

	_, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), constants.MinMemberOverrideKey)
}

func TestJobSetAnnotationBelowOne(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("a", 1, 1),
	})
	js.SetAnnotations(map[string]string{constants.MinMemberOverrideKey: "0"})
	pod := podWithJobSetLabels("p", "default", "a", "0")

	_, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ">= 1")
}

func TestTemplateAnnotationOverridesLeafMinMember(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJobWithTemplateAnnotation("worker", 2, 8, "5"),
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)

	leaf := findSubGroup(meta, "worker-replica-0")
	require.NotNil(t, leaf)
	assert.Equal(t, int32(5), leaf.MinAvailable)
}

func TestTemplateAnnotationGreaterThanParallelismAccepted(t *testing.T) {
	// Greater than parallelism is logged and applied.
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJobWithTemplateAnnotation("worker", 1, 2, "10"),
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	leaf := findSubGroup(meta, "worker-replica-0")
	require.NotNil(t, leaf)
	assert.Equal(t, int32(10), leaf.MinAvailable)
}

func TestTemplateAnnotationUnparseable(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJobWithTemplateAnnotation("worker", 1, 2, "abc"),
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	_, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.Error(t, err)
}

func TestTemplateAnnotationBelowOne(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJobWithTemplateAnnotation("worker", 1, 2, "0"),
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	_, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.Error(t, err)
}

func TestPodMissingReplicatedJobLabel(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 1, 2),
	})
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p",
			Namespace: "default",
			Labels:    map[string]string{jobSetLabelJobIndex: "0"},
		},
	}
	_, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), jobSetLabelReplicatedJobName)
}

func TestPodMissingJobIndexLabel(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 1, 2),
	})
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p",
			Namespace: "default",
			Labels:    map[string]string{jobSetLabelReplicatedJobName: "worker"},
		},
	}
	_, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), jobSetLabelJobIndex)
}

func TestPodRoutingFindsCorrectLeaf(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("leader", 1, 1),
		replicatedJob("worker", 3, 4),
	})
	g := newJobSetGrouper(t)

	leaderPod := podWithJobSetLabels("leader-pod", "default", "leader", "0")
	meta, err := g.GetPodGroupMetadata(js, leaderPod)
	require.NoError(t, err)
	leaderLeaf := findSubGroup(meta, "leader-replica-0")
	require.NotNil(t, leaderLeaf)
	assert.Equal(t, []string{"leader-pod"}, leaderLeaf.PodsReferences)
	assert.Empty(t, findSubGroup(meta, "worker-replica-1").PodsReferences)

	workerPod := podWithJobSetLabels("worker-pod", "default", "worker", "1")
	meta, err = g.GetPodGroupMetadata(js, workerPod)
	require.NoError(t, err)
	workerLeaf := findSubGroup(meta, "worker-replica-1")
	require.NotNil(t, workerLeaf)
	assert.Equal(t, []string{"worker-pod"}, workerLeaf.PodsReferences)
}

func TestEmptyReplicatedJobs(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	_, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no replicatedJobs")
}

func TestRootMinSubGroupNotMinAvailable(t *testing.T) {
	// Sanity: confirm we set MinSubGroup (not MinAvailable) at the root.
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 1, 1),
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")
	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	assert.Equal(t, int32(0), meta.MinAvailable)
	assert.NotNil(t, meta.MinSubGroup)
}

func TestDistinctTopologiesAtJobSetAndReplicatedJobLevels(t *testing.T) {
	// Two replicas so we can confirm the template-level topology is applied to
	// every leaf SubGroup, not just one.
	rj := replicatedJob("worker", 2, 4)
	rj["template"].(map[string]interface{})["metadata"] = map[string]interface{}{
		"annotations": map[string]interface{}{
			constants.TopologyKey:                   "leaf-topology",
			constants.TopologyRequiredPlacementKey:  "rack",
			constants.TopologyPreferredPlacementKey: "zone",
		},
	}
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{rj})
	js.SetAnnotations(map[string]string{
		constants.TopologyKey:                  "root-topology",
		constants.TopologyRequiredPlacementKey: "datacenter",
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)

	// Root PodGroup carries the JobSet-level topology (read by DefaultGrouper).
	assert.Equal(t, "root-topology", meta.Topology)
	assert.Equal(t, "datacenter", meta.RequiredTopologyLevel)
	assert.Empty(t, meta.PreferredTopologyLevel)

	// Parent SubGroup has no topology — only leaves get the template-level
	// constraints.
	parent := findSubGroup(meta, "worker")
	require.NotNil(t, parent)
	assert.Nil(t, parent.TopologyConstraints)

	// Both leaves carry the replicatedJob-template topology, distinct from the
	// root.
	for i := 0; i < 2; i++ {
		leafName := fmt.Sprintf(replicasSubgroupNameFormat, "worker", strconv.Itoa(i))
		leaf := findSubGroup(meta, leafName)
		require.NotNil(t, leaf, "leaf %s", leafName)
		require.NotNil(t, leaf.TopologyConstraints, "leaf %s missing TopologyConstraints", leafName)
		assert.Equal(t, "leaf-topology", leaf.TopologyConstraints.Topology)
		assert.Equal(t, "rack", leaf.TopologyConstraints.RequiredTopologyLevel)
		assert.Equal(t, "zone", leaf.TopologyConstraints.PreferredTopologyLevel)
	}
}

func TestReplicasDefaultsToOne(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		{
			"name": "worker",
			"template": map[string]interface{}{
				"spec": map[string]interface{}{"parallelism": int64(2)},
			},
		},
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	meta, err := newJobSetGrouper(t).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	parent := findSubGroup(meta, "worker")
	require.NotNil(t, parent)
	assert.Equal(t, ptr.To(int32(1)), parent.MinSubGroup)
}
