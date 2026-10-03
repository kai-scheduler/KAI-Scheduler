// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
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
			if tt.update != nil {
				task.Status, task.IsVirtualStatus = tt.update.status, tt.update.virtual
				assert.NoError(t, ni.UpdateTask(task))
			}
			if tt.remove {
				assert.NoError(t, ni.RemoveTask(task))
			}
		})
	}
}
