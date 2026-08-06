// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func newResourceList(cpu, memory string) v1.ResourceList {
	rl := v1.ResourceList{}
	if cpu != "" {
		rl[v1.ResourceCPU] = resource.MustParse(cpu)
	}
	if memory != "" {
		rl[v1.ResourceMemory] = resource.MustParse(memory)
	}
	return rl
}

func podWithContainers(containers []v1.Container) *v1.Pod {
	return &v1.Pod{Spec: v1.PodSpec{Containers: containers}}
}

func withContainerStatus(pod *v1.Pod, name string, enacted, allocated v1.ResourceList) *v1.Pod {
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, v1.ContainerStatus{
		Name: name,
		Resources: &v1.ResourceRequirements{
			Requests: enacted,
		},
		AllocatedResources: allocated,
	})
	return pod
}

func withInfeasibleCondition(pod *v1.Pod) *v1.Pod {
	pod.Status.Conditions = append(pod.Status.Conditions, v1.PodCondition{
		Type:   v1.PodResizePending,
		Status: v1.ConditionTrue,
		Reason: v1.PodReasonInfeasible,
	})
	return pod
}

func TestEffectivePodContainerRequests_NoStatus(t *testing.T) {
	// No resize in progress — fall back to spec.
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("500m", "128Mi")}},
	})
	got := effectivePodContainerRequests(pod)
	assert.Equal(t, newResourceList("500m", "128Mi"), got["c1"])
}

func TestEffectivePodContainerRequests_NormalResize_UsesMax(t *testing.T) {
	// spec > enacted > allocated → effective = spec
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("2", "1Gi")}},
	})
	withContainerStatus(pod, "c1",
		newResourceList("1", "512Mi"), // enacted (lower)
		newResourceList("1", "512Mi"), // allocated (lower)
	)
	got := effectivePodContainerRequests(pod)
	wantCPU := resource.MustParse("2")
	wantMem := resource.MustParse("1Gi")
	gotCPU := got["c1"][v1.ResourceCPU]
	gotMem := got["c1"][v1.ResourceMemory]
	assert.Equal(t, 0, gotCPU.Cmp(wantCPU))
	assert.Equal(t, 0, gotMem.Cmp(wantMem))
}

func TestEffectivePodContainerRequests_DownsizeInProgress_UsesEnacted(t *testing.T) {
	// spec < enacted (downsize not yet completed) → effective = enacted
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("500m", "256Mi")}},
	})
	withContainerStatus(pod, "c1",
		newResourceList("1", "512Mi"), // enacted (higher, still running at old size)
		newResourceList("1", "512Mi"), // allocated
	)
	got := effectivePodContainerRequests(pod)
	wantCPU := resource.MustParse("1")
	wantMem := resource.MustParse("512Mi")
	gotCPU := got["c1"][v1.ResourceCPU]
	gotMem := got["c1"][v1.ResourceMemory]
	assert.Equal(t, 0, gotCPU.Cmp(wantCPU))
	assert.Equal(t, 0, gotMem.Cmp(wantMem))
}

func TestEffectivePodContainerRequests_Infeasible_ExcludesSpec(t *testing.T) {
	// Infeasible: kubelet can't satisfy the spec — charge only what's enacted/allocated.
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("8", "8Gi")}},
	})
	withContainerStatus(pod, "c1",
		newResourceList("1", "1Gi"), // enacted
		newResourceList("1", "1Gi"), // allocated
	)
	withInfeasibleCondition(pod)

	got := effectivePodContainerRequests(pod)
	wantCPU := resource.MustParse("1")
	wantMem := resource.MustParse("1Gi")
	gotCPU := got["c1"][v1.ResourceCPU]
	gotMem := got["c1"][v1.ResourceMemory]
	assert.Equal(t, 0, gotCPU.Cmp(wantCPU))
	assert.Equal(t, 0, gotMem.Cmp(wantMem))
}

func TestEffectivePodContainerRequests_MultipleContainers(t *testing.T) {
	// Each container uses its own effective request.
	pod := podWithContainers([]v1.Container{
		{Name: "a", Resources: v1.ResourceRequirements{Requests: newResourceList("1", "")}},
		{Name: "b", Resources: v1.ResourceRequirements{Requests: newResourceList("2", "")}},
	})
	withContainerStatus(pod, "a", newResourceList("500m", ""), newResourceList("500m", ""))

	got := effectivePodContainerRequests(pod)
	wantA := resource.MustParse("1")
	wantB := resource.MustParse("2")
	gotA := got["a"][v1.ResourceCPU]
	gotB := got["b"][v1.ResourceCPU]
	// Container "a": max(spec=1, enacted=500m, alloc=500m) = 1
	assert.Equal(t, 0, gotA.Cmp(wantA))
	// Container "b": no status → spec = 2
	assert.Equal(t, 0, gotB.Cmp(wantB))
}

func TestIsPodResizeInfeasible(t *testing.T) {
	tests := []struct {
		name       string
		conditions []v1.PodCondition
		want       bool
	}{
		{"no conditions", nil, false},
		{"Deferred condition", []v1.PodCondition{
			{Type: v1.PodResizePending, Reason: v1.PodReasonDeferred},
		}, false},
		{"Infeasible condition", []v1.PodCondition{
			{Type: v1.PodResizePending, Reason: v1.PodReasonInfeasible},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{Status: v1.PodStatus{Conditions: tt.conditions}}
			assert.Equal(t, tt.want, isPodResizeInfeasible(pod))
		})
	}
}

func TestMaxResourceList(t *testing.T) {
	a := newResourceList("1", "1Gi")
	b := newResourceList("2", "512Mi")
	got := maxResourceList(a, b)
	wantCPU := resource.MustParse("2")
	wantMem := resource.MustParse("1Gi")
	gotCPU := got[v1.ResourceCPU]
	gotMem := got[v1.ResourceMemory]
	assert.Equal(t, 0, gotCPU.Cmp(wantCPU))
	assert.Equal(t, 0, gotMem.Cmp(wantMem))
}
