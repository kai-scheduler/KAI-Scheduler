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

func statusPtr(s pod_status.PodStatus) *pod_status.PodStatus { return &s }

func buildAffinityTestTask(vectorMap *resource_info.ResourceVectorMap, status pod_status.PodStatus) *pod_info.PodInfo {
	pod := common_info.BuildPod("ns", "p1", "n1", v1.PodRunning,
		common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{},
		map[string]string{}, map[string]string{
			pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
			commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
		})
	task := pod_info.NewTaskInfo(pod, vectorMap)
	task.Status = status
	return task
}

func buildAffinityTestNode() (*v1.Node, *resource_info.ResourceVectorMap) {
	node := common_info.BuildNode("n1", common_info.BuildResourceList("8000m", "10G"))
	vectorMap := resource_info.NewResourceVectorMap()
	vectorMap.AddResourceList(node.Status.Allocatable)
	return node, vectorMap
}

// The full view indexes every task regardless of status (upstream kube-scheduler
// semantics). The pipeline view omits Releasing tasks, and its bookkeeping must stay
// symmetric across evict / unevict / stuck-in-releasing transitions: removing a pod that
// was never indexed fails on the k8s "pod not found" error and would break UpdateTask.
func TestNodeInfoPipelineAffinityView(t *testing.T) {
	tests := []struct {
		name           string
		addStatus      pod_status.PodStatus
		updateStatus   *pod_status.PodStatus
		remove         bool
		fullAdd        int
		fullRemove     int
		pipelineAdd    int
		pipelineRemove int
	}{
		{
			name:      "running task is indexed and un-indexed in both views",
			addStatus: pod_status.Running, remove: true,
			fullAdd: 1, fullRemove: 1, pipelineAdd: 1, pipelineRemove: 1,
		},
		{
			name:      "releasing task is indexed in the full view only",
			addStatus: pod_status.Releasing, remove: true,
			fullAdd: 1, fullRemove: 1, pipelineAdd: 0, pipelineRemove: 0,
		},
		{
			name:      "evict: running -> releasing leaves the pipeline view, stays in the full view",
			addStatus: pod_status.Running, updateStatus: statusPtr(pod_status.Releasing),
			fullAdd: 2, fullRemove: 1, pipelineAdd: 1, pipelineRemove: 1,
		},
		{
			name:      "unevict: releasing -> running re-enters the pipeline view without un-indexing",
			addStatus: pod_status.Releasing, updateStatus: statusPtr(pod_status.Running),
			fullAdd: 2, fullRemove: 1, pipelineAdd: 1, pipelineRemove: 0,
		},
		{
			name:      "running -> stuck-in-releasing stays in both views",
			addStatus: pod_status.Running, updateStatus: statusPtr(pod_status.StuckInReleasing),
			fullAdd: 2, fullRemove: 1, pipelineAdd: 2, pipelineRemove: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := NewController(t)
			full := pod_affinity.NewMockNodePodAffinityInfo(ctrl)
			full.EXPECT().AddPod(Any()).Times(tt.fullAdd)
			full.EXPECT().RemovePod(Any()).Return(nil).Times(tt.fullRemove)
			pipeline := pod_affinity.NewMockNodePodAffinityInfo(ctrl)
			pipeline.EXPECT().AddPod(Any()).Times(tt.pipelineAdd)
			pipeline.EXPECT().RemovePod(Any()).Return(nil).Times(tt.pipelineRemove)

			node, vectorMap := buildAffinityTestNode()
			ni := NewNodeInfoWithPipelineAffinity(node, full, pipeline, vectorMap)
			task := buildAffinityTestTask(vectorMap, tt.addStatus)
			assert.NoError(t, ni.AddTask(task))

			if tt.updateStatus != nil {
				// Mirrors Statement.evict / unevict: the job's task changes status first,
				// then the node re-indexes it. The node keeps its own clone, so the removal
				// is decided by the status the pod was indexed under.
				task.Status = *tt.updateStatus
				assert.NoError(t, ni.UpdateTask(task))
			}
			if tt.remove {
				assert.NoError(t, ni.RemoveTask(task))
			}
		})
	}
}

// Without a pipeline view the node behaves exactly as before the view was introduced.
func TestNodeInfoWithoutPipelineAffinityView(t *testing.T) {
	ctrl := NewController(t)
	full := pod_affinity.NewMockNodePodAffinityInfo(ctrl)
	full.EXPECT().AddPod(Any()).Times(2)
	full.EXPECT().RemovePod(Any()).Return(nil).Times(2)

	node, vectorMap := buildAffinityTestNode()
	ni := NewNodeInfo(node, full, vectorMap)
	assert.Nil(t, ni.PipelinePodAffinityInfo)

	task := buildAffinityTestTask(vectorMap, pod_status.Running)
	assert.NoError(t, ni.AddTask(task))
	task.Status = pod_status.Releasing
	assert.NoError(t, ni.UpdateTask(task))
	assert.NoError(t, ni.RemoveTask(task))
}
