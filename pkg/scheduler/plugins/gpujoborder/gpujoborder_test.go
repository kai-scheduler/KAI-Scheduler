// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpujoborder

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func makeGPUPodGroup(uid string, priority int32, gpuCount float64, vm *resource_info.ResourceVectorMap) *podgroup_info.PodGroupInfo {
	task := &pod_info.PodInfo{
		UID:          common_info.PodID(uid + "-task"),
		ResReqVector: resource_info.NewResourceVectorWithValues(0, 0, gpuCount, vm),
		Status:       pod_status.Running,
	}
	pg := podgroup_info.NewPodGroupInfoWithVectorMap(common_info.PodGroupID(uid), vm, task)
	pg.Priority = priority
	return pg
}

func newPlugin(t *testing.T, mode string) *gpuJobOrderPlugin {
	t.Helper()
	args := framework.PluginArguments{}
	if mode != "" {
		args["mode"] = mode
	}
	rp, ok := New(args).(*gpuJobOrderPlugin)
	if !ok {
		t.Fatalf("New() did not return *gpuJobOrderPlugin")
	}
	return rp
}

func TestJobOrderFn_PriorityDiffers_Defers(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	a := makeGPUPodGroup("a", 50, 1, vm)
	b := makeGPUPodGroup("b", 10, 1, vm)
	rp := newPlugin(t, "")
	if got := rp.JobOrderFn(a, b); got != 0 {
		t.Errorf("expected 0 when priorities differ, got %d", got)
	}
}

func TestJobOrderFn_SamePriority_PrefersLarger_Default(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)
	rp := newPlugin(t, "")
	if got := rp.JobOrderFn(large, small); got != -1 {
		t.Errorf("expected -1 (larger job preferred as victim), got %d", got)
	}
	if got := rp.JobOrderFn(small, large); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestJobOrderFn_SamePriority_SameGPU_FallsThrough(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	a := makeGPUPodGroup("a", 10, 1, vm)
	b := makeGPUPodGroup("b", 10, 1, vm)
	rp := newPlugin(t, "")
	if got := rp.JobOrderFn(a, b); got != 0 {
		t.Errorf("expected 0 (equal GPU falls through), got %d", got)
	}
}

func TestJobOrderFn_PreferSmallerMode(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)
	rp := newPlugin(t, "prefer-smaller")
	if got := rp.JobOrderFn(small, large); got != -1 {
		t.Errorf("expected -1 (smaller job preferred as victim in prefer-smaller mode), got %d", got)
	}
	if got := rp.JobOrderFn(large, small); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestNew_UnrecognizedMode_FallsBackToPreferLarger(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)
	rp := newPlugin(t, "totally-not-a-real-mode")
	if got := rp.JobOrderFn(large, small); got != -1 {
		t.Errorf("expected fallback to prefer-larger behavior, got %d", got)
	}
}
