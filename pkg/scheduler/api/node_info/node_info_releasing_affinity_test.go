// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	commonconstants "github.com/kai-scheduler/api/constants"
)

func TestNodeInfoReleasingPodAffinity(t *testing.T) {
	type step struct {
		status  pod_status.PodStatus
		virtual bool
	}
	tests := []struct {
		name            string
		add             step
		update          *step
		remove          bool
		expectAddPod    int
		expectRemovePod int
	}{
		{name: "running task is indexed and un-indexed", add: step{pod_status.Running, false}, remove: true, expectAddPod: 1, expectRemovePod: 1},
		{name: "releasing task from an earlier cycle is not indexed", add: step{pod_status.Releasing, false}, remove: true, expectAddPod: 0, expectRemovePod: 0},
		{name: "session-evicted task (Releasing, virtual) is never indexed", add: step{pod_status.Releasing, true}, remove: true, expectAddPod: 0, expectRemovePod: 0},
		{name: "evict: running -> releasing+virtual un-indexes without re-indexing", add: step{pod_status.Running, false}, update: &step{pod_status.Releasing, true}, expectAddPod: 1, expectRemovePod: 1},
		{name: "unevict: releasing+virtual -> running re-indexes without un-indexing", add: step{pod_status.Releasing, true}, update: &step{pod_status.Running, false}, expectAddPod: 1, expectRemovePod: 0},
		{name: "running -> releasing in a later cycle leaves the index", add: step{pod_status.Running, false}, update: &step{pod_status.Releasing, false}, expectAddPod: 1, expectRemovePod: 1},
		{name: "releasing -> stuck-in-releasing restores the index", add: step{pod_status.Releasing, false}, update: &step{pod_status.StuckInReleasing, false}, remove: true, expectAddPod: 1, expectRemovePod: 1},
		{name: "running -> stuck-in-releasing stays indexed", add: step{pod_status.Running, false}, update: &step{pod_status.StuckInReleasing, true}, expectAddPod: 2, expectRemovePod: 1},
		{name: "evicted victim pipelined elsewhere is indexed again", add: step{pod_status.Releasing, true}, update: &step{pod_status.Pipelined, true}, expectAddPod: 1, expectRemovePod: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := NewController(t)
			affinity := pod_affinity.NewMockNodePodAffinityInfo(ctrl)
			affinity.EXPECT().AddPod(Any()).Times(tt.expectAddPod)
			affinity.EXPECT().RemovePod(Any()).Return(nil).Times(tt.expectRemovePod)

			node := common_info.BuildNode("n1", common_info.BuildResourceList("8000m", "10G"))
			vectorMap := resource_info.NewResourceVectorMap()
			vectorMap.AddResourceList(node.Status.Allocatable)
			ni := NewNodeInfo(node, affinity, vectorMap)

			pod := common_info.BuildPod("ns", "p1", "n1", v1.PodRunning,
				common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{},
				map[string]string{}, map[string]string{
					pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
					commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
				})
			task := pod_info.NewTaskInfo(pod, vectorMap)
			task.Status, task.IsVirtualStatus = tt.add.status, tt.add.virtual
			assert.NoError(t, ni.AddTask(task))
			assertReleasingPods(t, ni, task, tt.add.status)
			if tt.update != nil {
				task.Status, task.IsVirtualStatus = tt.update.status, tt.update.virtual
				assertReleasingPods(t, ni, task, tt.add.status)
				assert.NoError(t, ni.UpdateTask(task))
				assertReleasingPods(t, ni, task, tt.update.status)
			}
			if tt.remove {
				assert.NoError(t, ni.RemoveTask(task))
				assert.Empty(t, ni.ReleasingPods)
			}
		})
	}
}

func TestReleasingPodsWithZeroTrackedResources(t *testing.T) {
	affinity := pod_affinity.NewMockNodePodAffinityInfo(NewController(t))
	node := common_info.BuildNode("n1", common_info.BuildResourceList("8000m", "10G"))
	delete(node.Status.Allocatable, v1.ResourcePods)
	vectorMap := resource_info.NewResourceVectorMap()
	vectorMap.AddResourceList(node.Status.Allocatable)
	ni := NewNodeInfo(node, affinity, vectorMap)

	var tasks []*pod_info.PodInfo
	for _, name := range []string{"first", "second"} {
		pod := common_info.BuildPod("ns", name, "n1", v1.PodRunning, v1.ResourceList{}, nil, nil, nil)
		task := pod_info.NewTaskInfo(pod, vectorMap)
		task.Status = pod_status.Releasing
		require.NoError(t, ni.AddTask(task))
		tasks = append(tasks, task)
	}
	require.Len(t, ni.ReleasingPods, 2)
	require.Equal(t, uint64(2), ni.ReleasingPodsRevision)
	require.True(t, ni.ReleasingVector.IsZero())
	stored := ni.ReleasingPods[pod_info.PodKey(tasks[0].Pod)]
	require.Error(t, ni.AddTask(tasks[0]))
	require.Same(t, stored, ni.ReleasingPods[pod_info.PodKey(tasks[0].Pod)])
	require.Equal(t, uint64(2), ni.ReleasingPodsRevision)

	tasks[0].Status = pod_status.Running
	require.NoError(t, ni.RemoveTask(tasks[0]))
	require.Len(t, ni.ReleasingPods, 1)
	require.Equal(t, uint64(3), ni.ReleasingPodsRevision)
	require.Error(t, ni.RemoveTask(tasks[0]))
	require.Len(t, ni.ReleasingPods, 1)
	require.Equal(t, uint64(3), ni.ReleasingPodsRevision)
	require.NoError(t, ni.RemoveTask(tasks[1]))
	require.Empty(t, ni.ReleasingPods)
	require.Equal(t, uint64(4), ni.ReleasingPodsRevision)
}

func assertReleasingPods(t *testing.T, node *NodeInfo, task *pod_info.PodInfo, status pod_status.PodStatus) {
	t.Helper()
	if status != pod_status.Releasing {
		assert.Empty(t, node.ReleasingPods)
		return
	}
	key := pod_info.PodKey(task.Pod)
	assert.Len(t, node.ReleasingPods, 1)
	assert.Same(t, node.PodInfos[key], node.ReleasingPods[key])
	assert.NotSame(t, task, node.ReleasingPods[key])
	assert.Equal(t, status, node.ReleasingPods[key].Status)
}
