// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	resourcehelpers "k8s.io/component-helpers/resource"
)

// These tests cover in-place pod resize (KEP-1287) accounting through
// getPodResourceRequest. Aggregation is delegated to k8s.io/component-helpers;
// what is under test here is KAI's wiring of it: the UseStatusResources option,
// the ResourceRequirements conversion, and the effective-request semantics that
// queue accounting depends on. They also guard against behavioural drift if the
// dependency is bumped.

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

// assertCPUMem checks the aggregated pod request in millicpus and bytes.
func assertCPUMem(t *testing.T, pod *v1.Pod, wantMilliCPU, wantMemBytes float64) {
	t.Helper()
	got := getPodResourceRequest(pod)
	assert.Equal(t, wantMilliCPU, got.Get(v1.ResourceCPU), "cpu (millicpus)")
	assert.Equal(t, wantMemBytes, got.Get(v1.ResourceMemory), "memory (bytes)")
}

func TestEffectiveRequests_NoStatus(t *testing.T) {
	// No resize in progress — fall back to spec.
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("500m", "128Mi")}},
	})
	assertCPUMem(t, pod, 500, 128*1024*1024)
}

func TestEffectiveRequests_NormalResize_UsesMax(t *testing.T) {
	// spec > enacted/allocated → effective = spec
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("2", "1Gi")}},
	})
	withContainerStatus(pod, "c1",
		newResourceList("1", "512Mi"), // enacted (lower)
		newResourceList("1", "512Mi"), // allocated (lower)
	)
	assertCPUMem(t, pod, 2000, 1024*1024*1024)
}

func TestEffectiveRequests_DownsizeInProgress_UsesEnacted(t *testing.T) {
	// spec < enacted (downsize not yet completed) → effective = enacted
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("500m", "256Mi")}},
	})
	withContainerStatus(pod, "c1",
		newResourceList("1", "512Mi"), // enacted (higher, still running at old size)
		newResourceList("1", "512Mi"), // allocated
	)
	assertCPUMem(t, pod, 1000, 512*1024*1024)
}

func TestEffectiveRequests_Infeasible_ExcludesSpec(t *testing.T) {
	// Infeasible: kubelet can't satisfy the spec — charge only what's enacted/allocated.
	pod := podWithContainers([]v1.Container{
		{Name: "c1", Resources: v1.ResourceRequirements{Requests: newResourceList("8", "8Gi")}},
	})
	withContainerStatus(pod, "c1",
		newResourceList("1", "1Gi"), // enacted
		newResourceList("1", "1Gi"), // allocated
	)
	withInfeasibleCondition(pod)

	assertCPUMem(t, pod, 1000, 1024*1024*1024)
}

func TestEffectiveRequests_MultipleContainers(t *testing.T) {
	// Each container uses its own effective request; pod total is the sum.
	pod := podWithContainers([]v1.Container{
		{Name: "a", Resources: v1.ResourceRequirements{Requests: newResourceList("1", "")}},
		{Name: "b", Resources: v1.ResourceRequirements{Requests: newResourceList("2", "")}},
	})
	// a: max(spec=1, enacted=500m, alloc=500m) = 1;  b: no status → spec = 2
	withContainerStatus(pod, "a", newResourceList("500m", ""), newResourceList("500m", ""))

	assertCPUMem(t, pod, 3000, 0)
}

func TestEffectiveRequests_Sidecar_DownsizeInProgress(t *testing.T) {
	// Restartable init container (sidecar) mid-downsize must be charged at enacted.
	restartAlways := v1.ContainerRestartPolicyAlways
	pod := podWithContainers([]v1.Container{
		{Name: "main", Resources: v1.ResourceRequirements{Requests: newResourceList("1", "")}},
	})
	pod.Spec.InitContainers = []v1.Container{
		{
			Name:          "sidecar",
			RestartPolicy: &restartAlways,
			Resources:     v1.ResourceRequirements{Requests: newResourceList("500m", "")},
		},
	}
	pod.Status.InitContainerStatuses = []v1.ContainerStatus{
		{
			Name:               "sidecar",
			Resources:          &v1.ResourceRequirements{Requests: newResourceList("2", "")},
			AllocatedResources: newResourceList("2", ""),
		},
	}
	// main(1) + sidecar effective max(500m, 2, 2)=2 → 3000m
	assertCPUMem(t, pod, 3000, 0)
}

// TestIsPodResizeInfeasible characterises upstream's IsPodResizeInfeasible.
//
// Upstream keys off Reason alone and ignores condition.Status, so a PodResizePending
// condition carrying Reason=Infeasible is treated as infeasible even when Status is
// False. In practice the kubelet deletes the condition rather than setting it False,
// so this is not expected to be reachable — but the case is pinned here deliberately:
// if upstream ever tightens the check, this test fails and the behaviour change
// surfaces instead of passing silently.
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
			{Type: v1.PodResizePending, Status: v1.ConditionTrue, Reason: v1.PodReasonInfeasible},
		}, true},
		{"Infeasible reason with Status False is still infeasible upstream", []v1.PodCondition{
			{Type: v1.PodResizePending, Status: v1.ConditionFalse, Reason: v1.PodReasonInfeasible},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{Status: v1.PodStatus{Conditions: tt.conditions}}
			assert.Equal(t, tt.want, resourcehelpers.IsPodResizeInfeasible(pod))
		})
	}
}
