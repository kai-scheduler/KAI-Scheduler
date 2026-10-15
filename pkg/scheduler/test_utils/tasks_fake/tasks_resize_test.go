// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package tasks_fake

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
)

func buildResizePod(status *TestResizeStatus) *v1.Pod {
	req := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("3"),
		v1.ResourceMemory: resource.MustParse("2G"),
	}
	return BuildPod("pod", "ns", &TestTaskBasic{NodeName: "node0", ResizeStatus: status},
		v1.PodRunning, req, "", 0, nil, "job")
}

func TestBuildPodResizeStatus(t *testing.T) {
	pod := buildResizePod(&TestResizeStatus{ActualCPUs: 1, Deferred: true, PreemptionDisabled: true})

	require.Len(t, pod.Status.ContainerStatuses, 1)
	status := pod.Status.ContainerStatuses[0]
	assert.Equal(t, pod.Spec.Containers[0].Name, status.Name)
	assert.True(t, status.AllocatedResources.Cpu().Equal(resource.MustParse("1")))
	assert.True(t, status.Resources.Requests.Cpu().Equal(resource.MustParse("1")))
	assert.True(t, status.AllocatedResources.Memory().Equal(resource.MustParse("2G")), "unset actual memory keeps spec")
	assert.True(t, pod.Spec.Containers[0].Resources.Requests.Cpu().Equal(resource.MustParse("3")), "spec stays desired")

	require.Len(t, pod.Status.Conditions, 2)
	assert.Equal(t, v1.PodResizePending, pod.Status.Conditions[0].Type)
	assert.Equal(t, v1.PodReasonDeferred, pod.Status.Conditions[0].Reason)
	assert.Equal(t, v1.ConditionTrue, pod.Status.Conditions[0].Status)
	assert.Equal(t, pod_info.PodResizePreemptionDisabled, pod.Status.Conditions[1].Type)
	assert.Equal(t, v1.ConditionTrue, pod.Status.Conditions[1].Status)
}

func TestBuildPodResizeStatusVariants(t *testing.T) {
	infeasible := buildResizePod(&TestResizeStatus{ActualCPUs: 1, Infeasible: true})
	require.Len(t, infeasible.Status.Conditions, 1)
	assert.Equal(t, v1.PodReasonInfeasible, infeasible.Status.Conditions[0].Reason)

	memory := buildResizePod(&TestResizeStatus{ActualMemory: 1e9, Deferred: true})
	assert.True(t, memory.Status.ContainerStatuses[0].AllocatedResources.Memory().Equal(resource.MustParse("1G")))
	assert.True(t, memory.Status.ContainerStatuses[0].AllocatedResources.Cpu().Equal(resource.MustParse("3")))

	noResize := buildResizePod(nil)
	assert.Empty(t, noResize.Status.ContainerStatuses)
	assert.Empty(t, noResize.Status.Conditions)
}

func TestBuildPodResizeStatusIsDetected(t *testing.T) {
	deferred := &pod_info.PodInfo{Pod: buildResizePod(&TestResizeStatus{ActualCPUs: 1, Deferred: true}), Status: pod_status.Running}
	assert.True(t, pod_info.IsDeferredResizeTask(deferred))

	disabled := &pod_info.PodInfo{
		Pod:    buildResizePod(&TestResizeStatus{ActualCPUs: 1, Deferred: true, PreemptionDisabled: true}),
		Status: pod_status.Running,
	}
	assert.False(t, pod_info.IsDeferredResizeTask(disabled))
}

func TestBuildPodHostPorts(t *testing.T) {
	pod := BuildPod("pod", "ns", &TestTaskBasic{HostPorts: []int32{8080}}, v1.PodRunning, v1.ResourceList{}, "", 0, nil, "job")
	require.Len(t, pod.Spec.Containers[0].Ports, 1)
	assert.Equal(t, int32(8080), pod.Spec.Containers[0].Ports[0].HostPort)
}
