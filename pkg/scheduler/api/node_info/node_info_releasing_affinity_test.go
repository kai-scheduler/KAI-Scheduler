// Copyright 2025 NVIDIA CORPORATION
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

func statusPtr(s pod_status.PodStatus) *pod_status.PodStatus { return &s }

// A Releasing task must be invisible to the inter-pod affinity index, and the index
// bookkeeping must stay symmetric across evict / unevict / stuck-in-releasing
// transitions, otherwise UpdateTask fails on the k8s "pod not found" error.
func TestNodeInfoReleasingPodAffinity(t *testing.T) {
	tests := []struct {
		name            string
		addStatus       pod_status.PodStatus
		updateStatus    *pod_status.PodStatus
		remove          bool
		expectAddPod    int
		expectRemovePod int
	}{
		{
			name:            "running task is indexed and un-indexed",
			addStatus:       pod_status.Running,
			remove:          true,
			expectAddPod:    1,
			expectRemovePod: 1,
		},
		{
			name:            "releasing task is never indexed and removal does not un-index",
			addStatus:       pod_status.Releasing,
			remove:          true,
			expectAddPod:    0,
			expectRemovePod: 0,
		},
		{
			name:            "evict: running -> releasing un-indexes without re-indexing",
			addStatus:       pod_status.Running,
			updateStatus:    statusPtr(pod_status.Releasing),
			expectAddPod:    1,
			expectRemovePod: 1,
		},
		{
			name:            "unevict: releasing -> running re-indexes without un-indexing",
			addStatus:       pod_status.Releasing,
			updateStatus:    statusPtr(pod_status.Running),
			expectAddPod:    1,
			expectRemovePod: 0,
		},
		{
			name:            "running -> stuck-in-releasing stays indexed",
			addStatus:       pod_status.Running,
			updateStatus:    statusPtr(pod_status.StuckInReleasing),
			expectAddPod:    2,
			expectRemovePod: 1,
		},
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
			task.Status = tt.addStatus
			assert.NoError(t, ni.AddTask(task))

			if tt.updateStatus != nil {
				task.Status = *tt.updateStatus
				assert.NoError(t, ni.UpdateTask(task))
			}
			if tt.remove {
				assert.NoError(t, ni.RemoveTask(task))
			}
		})
	}
}
