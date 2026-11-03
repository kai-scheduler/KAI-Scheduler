// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func resizeTask(status pod_status.PodStatus, specCPU, enactedCPU, allocatedCPU string,
	conditions ...v1.PodCondition) *PodInfo {
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList(specCPU, "1Gi")}},
	})
	withContainerStatus(pod, "c1", newResourceList(enactedCPU, "1Gi"), newResourceList(allocatedCPU, "1Gi"))
	pod.Status.Conditions = conditions
	return &PodInfo{Pod: pod, Status: status}
}

func resizePendingCondition(reason string) v1.PodCondition {
	return v1.PodCondition{Type: v1.PodResizePending, Status: v1.ConditionTrue, Reason: reason}
}

func TestIsDeferredResizeTask(t *testing.T) {
	deferred := resizePendingCondition(v1.PodReasonDeferred)
	tests := []struct {
		name string
		task *PodInfo
		want bool
	}{
		{
			name: "deferred upsize",
			task: resizeTask(pod_status.Running, "2", "1", "1", deferred),
			want: true,
		},
		{
			name: "no resize condition",
			task: resizeTask(pod_status.Running, "2", "1", "1"),
			want: false,
		},
		{
			name: "infeasible resize",
			task: resizeTask(pod_status.Running, "2", "1", "1", resizePendingCondition(v1.PodReasonInfeasible)),
			want: false,
		},
		{
			name: "stale deferred after downsize",
			task: resizeTask(pod_status.Running, "1", "2", "2", deferred),
			want: false,
		},
		{
			name: "allocated already at desired size",
			task: resizeTask(pod_status.Running, "2", "1", "2", deferred),
			want: false,
		},
		{
			name: "releasing task",
			task: resizeTask(pod_status.Releasing, "2", "1", "1", deferred),
			want: false,
		},
		{
			name: "pending task",
			task: resizeTask(pod_status.Pending, "2", "1", "1", deferred),
			want: false,
		},
		{
			name: "pipelined task",
			task: resizeTask(pod_status.Pipelined, "2", "1", "1", deferred),
			want: false,
		},
		{
			name: "bound task not started yet",
			task: resizeTask(pod_status.Bound, "2", "1", "1", deferred),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsDeferredResizeTask(tt.task))
		})
	}
}

func TestIsDeferredResizeTask_MemoryOnlyUpsize(t *testing.T) {
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("1", "2Gi")}},
	})
	withContainerStatus(pod, "c1", newResourceList("1", "1Gi"), newResourceList("1", "1Gi"))
	pod.Status.Conditions = []v1.PodCondition{resizePendingCondition(v1.PodReasonDeferred)}

	assert.True(t, IsDeferredResizeTask(&PodInfo{Pod: pod, Status: pod_status.Running}))
}

func TestIsDeferredResizeTask_SidecarUpsize(t *testing.T) {
	always := v1.ContainerRestartPolicyAlways
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("1", "1Gi")}},
	})
	pod.Spec.InitContainers = []v1.Container{{
		Name: "sidecar", RestartPolicy: &always,
		Resources: v1.ResourceRequirements{Requests: newResourceList("2", "1Gi")},
	}}
	withContainerStatus(pod, "c1", newResourceList("1", "1Gi"), newResourceList("1", "1Gi"))
	pod.Status.InitContainerStatuses = []v1.ContainerStatus{{
		Name:               "sidecar",
		Resources:          &v1.ResourceRequirements{Requests: newResourceList("1", "1Gi")},
		AllocatedResources: newResourceList("1", "1Gi"),
	}}
	pod.Status.Conditions = []v1.PodCondition{resizePendingCondition(v1.PodReasonDeferred)}

	assert.True(t, IsDeferredResizeTask(&PodInfo{Pod: pod, Status: pod_status.Running}))
}

func TestNewResizeStandIn(t *testing.T) {
	task := NewTaskInfo(resizeTask(pod_status.Running, "2", "1", "1").Pod, resource_info.NewResourceVectorMap())
	task.NodeName, task.Status = "node0", pod_status.Running

	request := NewResizeStandIn(task)

	assert.Equal(t, task.UID, request.UID)
	assert.Equal(t, "node0", request.NodeName)
	assert.Equal(t, pod_status.Pending, request.Status)
	assert.True(t, request.IsResizeStandIn)
	assert.True(t, request.ResReqVector.IsZero())
	assert.True(t, request.GpuRequirement.IsEmpty())
	assert.True(t, request.Clone().IsResizeStandIn)

	assert.Equal(t, pod_status.Running, task.Status)
	assert.False(t, task.IsResizeStandIn)
	assert.False(t, task.ResReqVector.IsZero())
}
