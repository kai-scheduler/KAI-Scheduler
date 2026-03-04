// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package leader_worker_set

import (
	"testing"

	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	lws "sigs.k8s.io/lws/api/leaderworkerset/v1"
)

func lwsOwner(
	name string, startupPolicy string, replicas int64,
	segmentSize *int64, policyType *lws.SubGroupPolicyType, segmentTopology *podgroup.TopologyConstraintMetadata,
) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "LeaderWorkerSet",
			"apiVersion": "leaderworkerset.x-k8s.io/v1",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": "default",
				"uid":       name + "-uid",
			},
			"spec": map[string]interface{}{
				"startupPolicy": startupPolicy,
				"leaderWorkerTemplate": map[string]interface{}{
					"workerTemplate": map[string]interface{}{
						"metadata": map[string]interface{}{},
					},
					"size": replicas,
				},
			},
		},
	}
	if segmentSize != nil {
		lwt := obj.Object["spec"].(map[string]interface{})["leaderWorkerTemplate"].(map[string]interface{})
		lwt["subGroupPolicy"] = map[string]interface{}{
			"subGroupSize": *segmentSize,
		}
		if policyType != nil {
			lwt["subGroupPolicy"].(map[string]interface{})["subGroupPolicyType"] = string(*policyType)
		}
		if segmentTopology != nil {
			lwt["workerTemplate"].(map[string]interface{})["metadata"].(map[string]interface{})["annotations"] = map[string]interface{}{
				constants.SegmentTopologyRequiredPlacementKey:  segmentTopology.RequiredTopologyLevel,
				constants.SegmentTopologyPreferredPlacementKey: segmentTopology.PreferredTopologyLevel,
				constants.TopologyKey:                          segmentTopology.Topology,
			}
		}
	}
	return obj
}

func makeLwsPod(name, workerIndex string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{lwsWorkerIndexLabel: workerIndex},
		},
	}
}

func TestGetPodGroupMetadata_LeaderCreated(t *testing.T) {
	owner := lwsOwner("lws-test", "LeaderCreated", 3, nil, nil, nil)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lws-test-0-1",
			Namespace: "default",
			Labels:    map[string]string{},
		},
	}

	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	podGroupMetadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(3), podGroupMetadata.MinAvailable)
	assert.Equal(t, "LeaderWorkerSet", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "leaderworkerset.x-k8s.io/v1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "lws-test", podGroupMetadata.Owner.Name)
	assert.Equal(t, "lws-test-uid", string(podGroupMetadata.Owner.UID))

	assert.Equal(t, 2, len(podGroupMetadata.SubGroups))
	leaderSubGroup := findSubGroupByName(podGroupMetadata.SubGroups, "leader")
	assert.NotNil(t, leaderSubGroup)
	assert.Equal(t, int32(1), leaderSubGroup.MinAvailable)
	assert.Equal(t, 0, len(leaderSubGroup.PodsReferences))
	workersSubGroup := findSubGroupByName(podGroupMetadata.SubGroups, "workers")
	assert.NotNil(t, workersSubGroup)
	assert.Equal(t, int32(2), workersSubGroup.MinAvailable)
	assert.Equal(t, 1, len(workersSubGroup.PodsReferences))
	assert.Equal(t, "lws-test-0-1", workersSubGroup.PodsReferences[0])
}

func TestGetPodGroupMetadata_LeaderReady_LeaderPod(t *testing.T) {
	owner := lwsOwner("lws-ready", "LeaderReady", 5, nil, nil, nil)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lws-ready-0-0",
			Namespace: "default",
			Labels: map[string]string{
				"leaderworkerset.sigs.k8s.io/worker-index": "0",
			},
		},
		Spec: v1.PodSpec{NodeName: ""},
	}

	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	podGroupMetadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(1), podGroupMetadata.MinAvailable)
	assert.Equal(t, 1, len(podGroupMetadata.SubGroups))
	leaderSubGroup := findSubGroupByName(podGroupMetadata.SubGroups, "leader")
	assert.NotNil(t, leaderSubGroup)
	assert.Equal(t, int32(1), leaderSubGroup.MinAvailable)
	assert.Equal(t, 1, len(leaderSubGroup.PodsReferences))
	assert.Equal(t, "lws-ready-0-0", leaderSubGroup.PodsReferences[0])
}

func TestGetPodGroupMetadata_LeaderReady_WorkerPod(t *testing.T) {
	owner := lwsOwner("lws-ready", "LeaderReady", 5, nil, nil, nil)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lws-ready-0-2",
			Namespace: "default",
			Annotations: map[string]string{
				"leaderworkerset.sigs.k8s.io/size": "5",
			},
			Labels: map[string]string{
				"leaderworkerset.sigs.k8s.io/group-index":  "0",
				"leaderworkerset.sigs.k8s.io/worker-index": "2",
			},
		},
		Spec: v1.PodSpec{NodeName: "worker-node"},
	}

	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	podGroupMetadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(5), podGroupMetadata.MinAvailable)
	assert.Equal(t, 2, len(podGroupMetadata.SubGroups))
	workersSubGroup := findSubGroupByName(podGroupMetadata.SubGroups, "workers")
	assert.NotNil(t, workersSubGroup)
	assert.Equal(t, int32(4), workersSubGroup.MinAvailable)
	assert.Equal(t, 1, len(workersSubGroup.PodsReferences))
	assert.Equal(t, "lws-ready-0-2", workersSubGroup.PodsReferences[0])
}

func TestGetPodGroupMetadata_GroupIndex_Label(t *testing.T) {
	owner := lwsOwner("lws-grouped", "LeaderCreated", 2, nil, nil, nil)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"leaderworkerset.sigs.k8s.io/group-index": "1",
			},
		},
	}

	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	podGroupMetadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Contains(t, podGroupMetadata.Name, "-group-1")
}

func TestGetPodGroupMetadata_SubGroups_LeaderPod(t *testing.T) {
	owner := lwsOwner("lws-subgroups", "LeaderCreated", 3, nil, nil, nil)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lws-subgroups-0-0",
			Namespace: "default",
			Labels: map[string]string{
				"leaderworkerset.sigs.k8s.io/worker-index": "0",
			},
		},
	}
	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	metadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)
	assert.Nil(t, err)

	assert.Equal(t, 2, len(metadata.SubGroups))
	leaderSubGroup := findSubGroupByName(metadata.SubGroups, "leader")
	assert.NotNil(t, leaderSubGroup)
	assert.Equal(t, int32(1), leaderSubGroup.MinAvailable)
	assert.Equal(t, 1, len(leaderSubGroup.PodsReferences))
	assert.Equal(t, "lws-subgroups-0-0", leaderSubGroup.PodsReferences[0])

	workersSubGroup := findSubGroupByName(metadata.SubGroups, "workers")
	assert.NotNil(t, workersSubGroup)
	assert.Equal(t, int32(2), workersSubGroup.MinAvailable)
	assert.Equal(t, 0, len(workersSubGroup.PodsReferences))
}

func TestGetPodGroupMetadata_SubGroups_WorkerPod(t *testing.T) {
	owner := lwsOwner("lws-subgroups", "LeaderCreated", 3, nil, nil, nil)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lws-subgroups-0-1",
			Namespace: "default",
			Labels: map[string]string{
				"leaderworkerset.sigs.k8s.io/worker-index": "1",
			},
		},
	}
	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	metadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)
	assert.Nil(t, err)

	assert.Equal(t, 2, len(metadata.SubGroups))
	leaderSubGroup := findSubGroupByName(metadata.SubGroups, "leader")
	assert.NotNil(t, leaderSubGroup)
	assert.Equal(t, int32(1), leaderSubGroup.MinAvailable)
	assert.Equal(t, 0, len(leaderSubGroup.PodsReferences))

	workersSubGroup := findSubGroupByName(metadata.SubGroups, "workers")
	assert.NotNil(t, workersSubGroup)
	assert.Equal(t, int32(2), workersSubGroup.MinAvailable)
	assert.Equal(t, 1, len(workersSubGroup.PodsReferences))
	assert.Equal(t, "lws-subgroups-0-1", workersSubGroup.PodsReferences[0])
}

func TestGetPodGroupMetadata_SubGroups_OnlyLeader(t *testing.T) {
	owner := lwsOwner("lws-single", "LeaderCreated", 1, nil, nil, nil)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lws-single-0-0",
			Namespace: "default",
			Labels: map[string]string{
				"leaderworkerset.sigs.k8s.io/worker-index": "0",
			},
		},
	}
	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	metadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)
	assert.Nil(t, err)

	assert.Equal(t, 1, len(metadata.SubGroups))
	leaderSubGroup := findSubGroupByName(metadata.SubGroups, "leader")
	assert.NotNil(t, leaderSubGroup)
	assert.Equal(t, int32(1), leaderSubGroup.MinAvailable)
	assert.Equal(t, 1, len(leaderSubGroup.PodsReferences))
	assert.Equal(t, "lws-single-0-0", leaderSubGroup.PodsReferences[0])
}

func findSubGroupByName(subGroups []*podgroup.SubGroupMetadata, name string) *podgroup.SubGroupMetadata {
	for _, sg := range subGroups {
		if sg.Name == name {
			return sg
		}
	}
	return nil
}

// TestBuildSubGroups_Segmentation_Divisible_LeaderPod covers the case where
// isReplicasSizeDivisibleBySubGroupSize is true (replicasSize=5, segmentSize=2 → (5-1)%2=0)
// and the pod is the leader (worker-index=0).
// The first segment is expanded: MinAvailable = segmentSize+1 = 3.
func TestBuildSubGroups_Segmentation_Divisible_LeaderPod(t *testing.T) {
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 5, ptr.To(int64(2)), nil, nil)
	pod := makeLwsPod("lws-seg-0-0", "0")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 5)
	assert.NoError(t, err)

	assert.Len(t, subGroups, 4) // segment-0, segment-1, leader, workers

	seg0 := findSubGroupByName(subGroups, "segment-0")
	assert.NotNil(t, seg0)
	assert.Equal(t, int32(3), seg0.MinAvailable) // segmentSize(2) + leader(1)
	assert.Empty(t, seg0.PodsReferences)

	seg1 := findSubGroupByName(subGroups, "segment-1")
	assert.NotNil(t, seg1)
	assert.Equal(t, int32(2), seg1.MinAvailable)
	assert.Empty(t, seg1.PodsReferences)

	leaderSG := findSubGroupByName(subGroups, leaderSubGroupName)
	assert.NotNil(t, leaderSG)
	assert.Equal(t, int32(1), leaderSG.MinAvailable)
	assert.Equal(t, []string{"lws-seg-0-0"}, leaderSG.PodsReferences)

	workersSG := findSubGroupByName(subGroups, workersSubGroupName)
	assert.NotNil(t, workersSG)
	assert.Equal(t, int32(2), workersSG.MinAvailable) // 3 - 1
	assert.Empty(t, workersSG.PodsReferences)
}

// TestBuildSubGroups_Segmentation_Divisible_WorkerInFirstSegment covers the case where
// isReplicasSizeDivisibleBySubGroupSize is true and a worker pod lands in segment 0
// (worker-index=1 → (1-1)/2=0). The pod reference goes into the workers child subgroup.
// Pods: segment-0={leader,w1,w2}=3, segment-1={w3,w4}=2. Total=5.
func TestBuildSubGroups_Segmentation_Divisible_WorkerInFirstSegment(t *testing.T) {
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 5, ptr.To(int64(2)), nil, nil)
	pod := makeLwsPod("lws-seg-0-1", "1")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 5)
	assert.NoError(t, err)

	assert.Len(t, subGroups, 4)

	seg0 := findSubGroupByName(subGroups, "segment-0")
	assert.Equal(t, int32(3), seg0.MinAvailable) // segmentSize(2) + leader(1)
	assert.Empty(t, seg0.PodsReferences)

	seg1 := findSubGroupByName(subGroups, "segment-1")
	assert.Equal(t, int32(2), seg1.MinAvailable)
	assert.Empty(t, seg1.PodsReferences)

	leaderSG := findSubGroupByName(subGroups, leaderSubGroupName)
	assert.Equal(t, int32(1), leaderSG.MinAvailable)
	assert.Empty(t, leaderSG.PodsReferences)

	workersSG := findSubGroupByName(subGroups, workersSubGroupName)
	assert.Equal(t, int32(2), workersSG.MinAvailable) // 3 - 1
	assert.Equal(t, []string{"lws-seg-0-1"}, workersSG.PodsReferences)
}

// TestBuildSubGroups_Segmentation_Divisible_WorkerInLaterSegment covers the case where
// isReplicasSizeDivisibleBySubGroupSize is true and a worker pod lands in segment 1
// (worker-index=3 → (3-1)/2=1). The pod reference goes directly onto segment-1.
// Pods: segment-0={leader,w1,w2}=3, segment-1={w3,w4}=2. Total=5.
func TestBuildSubGroups_Segmentation_Divisible_WorkerInLaterSegment(t *testing.T) {
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 5, ptr.To(int64(2)), nil, nil)
	pod := makeLwsPod("lws-seg-0-3", "3")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 5)
	assert.NoError(t, err)

	assert.Len(t, subGroups, 4)

	seg0 := findSubGroupByName(subGroups, "segment-0")
	assert.NotNil(t, seg0)
	assert.Equal(t, int32(3), seg0.MinAvailable) // segmentSize(2) + leader(1)
	assert.Empty(t, seg0.PodsReferences)

	seg1 := findSubGroupByName(subGroups, "segment-1")
	assert.NotNil(t, seg1)
	assert.Equal(t, int32(2), seg1.MinAvailable)
	assert.Equal(t, []string{"lws-seg-0-3"}, seg1.PodsReferences)

	leaderSG := findSubGroupByName(subGroups, leaderSubGroupName)
	assert.Equal(t, int32(1), leaderSG.MinAvailable)
	assert.Empty(t, leaderSG.PodsReferences)

	workersSG := findSubGroupByName(subGroups, workersSubGroupName)
	assert.Equal(t, int32(2), workersSG.MinAvailable) // 3 - 1
	assert.Empty(t, workersSG.PodsReferences)
}

// TestBuildSubGroups_Segmentation_NotDivisible_LeaderPod covers the case where
// isReplicasSizeDivisibleBySubGroupSize is false (replicasSize=4, segmentSize=2 → (4-1)%2=1)
// and the pod is the leader. The first segment is NOT expanded (MinAvailable stays at 2).
// Pods: segment-0={leader,w1}=2, segment-1={w2,w3}=2. Total=4.
func TestBuildSubGroups_Segmentation_NotDivisible_LeaderPod(t *testing.T) {
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 4, ptr.To(int64(2)), nil, nil)
	pod := makeLwsPod("lws-seg-0-0", "0")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 4)
	assert.NoError(t, err)

	assert.Len(t, subGroups, 4) // segment-0, segment-1, leader, workers

	seg0 := findSubGroupByName(subGroups, "segment-0")
	assert.Equal(t, int32(2), seg0.MinAvailable) // not expanded
	assert.Empty(t, seg0.PodsReferences)

	seg1 := findSubGroupByName(subGroups, "segment-1")
	assert.Equal(t, int32(2), seg1.MinAvailable)
	assert.Empty(t, seg1.PodsReferences)

	leaderSG := findSubGroupByName(subGroups, leaderSubGroupName)
	assert.Equal(t, int32(1), leaderSG.MinAvailable)
	assert.Equal(t, []string{"lws-seg-0-0"}, leaderSG.PodsReferences)

	workersSG := findSubGroupByName(subGroups, workersSubGroupName)
	assert.Equal(t, int32(1), workersSG.MinAvailable) // 2 - 1
	assert.Empty(t, workersSG.PodsReferences)
}

// TestBuildSubGroups_Segmentation_LeaderExcluded_WorkerInFirstSegment covers LeaderExcluded
// policy (divisible required) with a worker in segment 0 (worker-index=1 → segment 0).
// The leader gets its own top-level subgroup; the worker reference lands on segment-0.
// Pods: leader=1, segment-0={w1,w2}=2, segment-1={w3,w4}=2. Total=5.
func TestBuildSubGroups_Segmentation_LeaderExcluded_WorkerInFirstSegment(t *testing.T) {
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 5, ptr.To(int64(2)), ptr.To(lws.SubGroupPolicyTypeLeaderExcluded), nil)
	pod := makeLwsPod("lws-seg-0-1", "1")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 5)
	assert.NoError(t, err)
	// No nil entries on the LeaderExcluded path
	assert.Len(t, subGroups, 3) // leader, segment-0, segment-1

	assert.Equal(t, leaderSubGroupName, subGroups[0].Name)
	assert.Equal(t, int32(1), subGroups[0].MinAvailable)
	assert.Empty(t, subGroups[0].PodsReferences)

	assert.Equal(t, "segment-0", subGroups[1].Name)
	assert.Equal(t, int32(2), subGroups[1].MinAvailable)
	assert.Equal(t, []string{"lws-seg-0-1"}, subGroups[1].PodsReferences)

	assert.Equal(t, "segment-1", subGroups[2].Name)
	assert.Equal(t, int32(2), subGroups[2].MinAvailable)
	assert.Empty(t, subGroups[2].PodsReferences)
}

// TestBuildSubGroups_Segmentation_LeaderExcluded_WorkerInLaterSegment covers LeaderExcluded
// policy with a worker in segment 1 (worker-index=3 → (3-1)/2=1).
// Pods: leader=1, segment-0={w1,w2}=2, segment-1={w3,w4}=2. Total=5.
func TestBuildSubGroups_Segmentation_LeaderExcluded_WorkerInLaterSegment(t *testing.T) {
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 5, ptr.To(int64(2)), ptr.To(lws.SubGroupPolicyTypeLeaderExcluded), nil)
	pod := makeLwsPod("lws-seg-0-3", "3")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 5)
	assert.NoError(t, err)
	assert.Len(t, subGroups, 3)

	assert.Equal(t, leaderSubGroupName, subGroups[0].Name)
	assert.Equal(t, int32(1), subGroups[0].MinAvailable)
	assert.Empty(t, subGroups[0].PodsReferences)

	assert.Equal(t, "segment-0", subGroups[1].Name)
	assert.Equal(t, int32(2), subGroups[1].MinAvailable)
	assert.Empty(t, subGroups[1].PodsReferences)

	assert.Equal(t, "segment-1", subGroups[2].Name)
	assert.Equal(t, int32(2), subGroups[2].MinAvailable)
	assert.Equal(t, []string{"lws-seg-0-3"}, subGroups[2].PodsReferences)
}

// TestBuildSubGroups_Segmentation_TopologyPreferredOnly_LeaderIncluded_NotDivisible covers
// LeaderWorker policy with only a preferred topology level set (required is empty),
// not-divisible sizing (replicasSize=5, segmentSize=3 → (5-1)%3=1), and a worker pod in
// segment 0 (worker-index=1 → 1/3=0). All segment subgroups carry the topology constraint;
// the leader/workers child subgroups do not.
// Pods: segment-0={leader,w1,w2}=3, segment-1={w3,w4}=2. Total=5.
func TestBuildSubGroups_Segmentation_TopologyPreferredOnly_LeaderIncluded_NotDivisible(t *testing.T) {
	topology := &podgroup.TopologyConstraintMetadata{
		Topology:               "rack",
		PreferredTopologyLevel: "node",
	}
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 5, ptr.To(int64(3)), nil, topology)
	pod := makeLwsPod("lws-seg-0-1", "1")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 5)
	assert.NoError(t, err)
	assert.Len(t, subGroups, 4) // segment-0, segment-1, leader, workers

	seg0 := findSubGroupByName(subGroups, "segment-0")
	assert.NotNil(t, seg0)
	assert.Equal(t, int32(3), seg0.MinAvailable) // not expanded (not divisible)
	assert.Equal(t, topology, seg0.TopologyConstraints)

	seg1 := findSubGroupByName(subGroups, "segment-1")
	assert.NotNil(t, seg1)
	// last segment might contain less pods then the segment size
	assert.Equal(t, int32(2), seg1.MinAvailable)
	assert.Equal(t, topology, seg1.TopologyConstraints)

	leaderSG := findSubGroupByName(subGroups, leaderSubGroupName)
	assert.NotNil(t, leaderSG)
	assert.Equal(t, int32(1), leaderSG.MinAvailable)
	assert.Nil(t, leaderSG.TopologyConstraints)
	assert.Empty(t, leaderSG.PodsReferences)

	workersSG := findSubGroupByName(subGroups, workersSubGroupName)
	assert.NotNil(t, workersSG)
	assert.Equal(t, int32(2), workersSG.MinAvailable) // 3 - 1
	assert.Nil(t, workersSG.TopologyConstraints)
	assert.Equal(t, []string{"lws-seg-0-1"}, workersSG.PodsReferences)
}

// TestBuildSubGroups_Segmentation_TopologyBothPlacementKeys_LeaderExcluded covers
// LeaderExcluded policy with both preferred and required topology levels set,
// divisible sizing (replicasSize=5, segmentSize=2), and a worker pod in segment 0
// (worker-index=1 → (1-1)/2=0). Segment subgroups carry the full topology constraint;
// the leader top-level subgroup does not.
// Pods: leader=1, segment-0={w1,w2}=2, segment-1={w3,w4}=2. Total=5.
func TestBuildSubGroups_Segmentation_TopologyBothPlacementKeys_LeaderExcluded(t *testing.T) {
	topology := &podgroup.TopologyConstraintMetadata{
		Topology:               "rack",
		PreferredTopologyLevel: "node",
		RequiredTopologyLevel:  "rack-unit",
	}
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 5, ptr.To(int64(2)), ptr.To(lws.SubGroupPolicyTypeLeaderExcluded), topology)
	pod := makeLwsPod("lws-seg-0-1", "1")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 5)
	assert.NoError(t, err)
	assert.Len(t, subGroups, 3) // leader, segment-0, segment-1

	assert.Equal(t, leaderSubGroupName, subGroups[0].Name)
	assert.Equal(t, int32(1), subGroups[0].MinAvailable)
	assert.Nil(t, subGroups[0].TopologyConstraints)
	assert.Empty(t, subGroups[0].PodsReferences)

	assert.Equal(t, "segment-0", subGroups[1].Name)
	assert.Equal(t, int32(2), subGroups[1].MinAvailable)
	assert.Equal(t, topology, subGroups[1].TopologyConstraints)
	assert.Equal(t, []string{"lws-seg-0-1"}, subGroups[1].PodsReferences)

	assert.Equal(t, "segment-1", subGroups[2].Name)
	assert.Equal(t, int32(2), subGroups[2].MinAvailable)
	assert.Equal(t, topology, subGroups[2].TopologyConstraints)
	assert.Empty(t, subGroups[2].PodsReferences)
}

// TestBuildSubGroups_SegmentSizeFromWorkerTemplateAnnotation covers the third fallback
// where segment size is read from the workerTemplate metadata annotations
// (no subGroupPolicy, no LWS-level annotation). Topology constraints are also sourced
// from the same workerTemplate annotations.
// replicasSize=5, segmentSize=2 → divisible ((5-1)%2=0), LeaderWorker policy.
// Worker pod at worker-index=1 lands in segment 0.
// Pods: segment-0={leader,w1,w2}=3, segment-1={w3,w4}=2. Total=5.
func TestBuildSubGroups_SegmentSizeFromWorkerTemplateAnnotation(t *testing.T) {
	lwsJob := lwsOwner("lws-seg", "LeaderCreated", 5, nil, nil, nil)
	lwsJob.SetAnnotations(map[string]string{
		constants.TopologyKey: "rack",
	})
	err := unstructured.SetNestedStringMap(lwsJob.Object, map[string]string{
		constants.SegmentSizeKey:                       "2",
		constants.SegmentTopologyRequiredPlacementKey:  "rack-unit",
		constants.SegmentTopologyPreferredPlacementKey: "node",
	}, "spec", "leaderWorkerTemplate", "workerTemplate", "metadata", "annotations")
	assert.NoError(t, err)
	pod := makeLwsPod("lws-seg-0-1", "1")
	grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))

	subGroups, err := grouper.buildSubGroups(lwsJob, pod, 5)
	assert.NoError(t, err)
	assert.Len(t, subGroups, 4) // segment-0, segment-1, leader, workers

	expectedTopology := &podgroup.TopologyConstraintMetadata{
		Topology:               "rack",
		RequiredTopologyLevel:  "rack-unit",
		PreferredTopologyLevel: "node",
	}

	seg0 := findSubGroupByName(subGroups, "segment-0")
	assert.NotNil(t, seg0)
	assert.Equal(t, int32(3), seg0.MinAvailable) // segmentSize(2) + leader(1)
	assert.Equal(t, expectedTopology, seg0.TopologyConstraints)

	seg1 := findSubGroupByName(subGroups, "segment-1")
	assert.NotNil(t, seg1)
	assert.Equal(t, int32(2), seg1.MinAvailable)
	assert.Equal(t, expectedTopology, seg1.TopologyConstraints)

	leaderSG := findSubGroupByName(subGroups, leaderSubGroupName)
	assert.NotNil(t, leaderSG)
	assert.Equal(t, int32(1), leaderSG.MinAvailable)
	assert.Nil(t, leaderSG.TopologyConstraints)
	assert.Empty(t, leaderSG.PodsReferences)

	workersSG := findSubGroupByName(subGroups, workersSubGroupName)
	assert.NotNil(t, workersSG)
	assert.Equal(t, int32(2), workersSG.MinAvailable) // 3 - 1
	assert.Nil(t, workersSG.TopologyConstraints)
	assert.Equal(t, []string{"lws-seg-0-1"}, workersSG.PodsReferences)
}
