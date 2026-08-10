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

func TestVictimOrderFn_PriorityDiffers_Defers(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	a := makeGPUPodGroup("a", 50, 1, vm)
	b := makeGPUPodGroup("b", 10, 1, vm)
	rp := newPlugin(t, "")
	if got := rp.VictimOrderFn(a, b); got != 0 {
		t.Errorf("expected 0 when priorities differ, got %d", got)
	}
}

func TestVictimOrderFn_SamePriority_PrefersLarger_Default(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)
	rp := newPlugin(t, "")
	if got := rp.VictimOrderFn(large, small); got != -1 {
		t.Errorf("expected -1 (larger job preferred as victim), got %d", got)
	}
	if got := rp.VictimOrderFn(small, large); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestVictimOrderFn_SamePriority_SameGPU_FallsThrough(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	a := makeGPUPodGroup("a", 10, 1, vm)
	b := makeGPUPodGroup("b", 10, 1, vm)
	rp := newPlugin(t, "")
	if got := rp.VictimOrderFn(a, b); got != 0 {
		t.Errorf("expected 0 (equal GPU falls through), got %d", got)
	}
}

func TestVictimOrderFn_PreferSmallerMode(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)
	rp := newPlugin(t, "prefer-smaller")
	if got := rp.VictimOrderFn(small, large); got != -1 {
		t.Errorf("expected -1 (smaller job preferred as victim in prefer-smaller mode), got %d", got)
	}
	if got := rp.VictimOrderFn(large, small); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestNew_UnrecognizedMode_FallsBackToPreferLarger(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)
	rp := newPlugin(t, "totally-not-a-real-mode")
	if got := rp.VictimOrderFn(large, small); got != -1 {
		t.Errorf("expected fallback to prefer-larger behavior, got %d", got)
	}
}

// TestGpujoborder_DoesNotAffect_PendingJobOrdering is the specific test
// gshaibi asked for: confirms that registering gpujoborder's comparator
// has NO EFFECT on ssn.JobOrderFn (pending-job allocation ordering),
// since the plugin now registers exclusively via AddVictimOrderFn.
func TestGpujoborder_DoesNotAffect_PendingJobOrdering(t *testing.T) {
	rp := newPlugin(t, "")

	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)

	// Session WITHOUT gpujoborder registered: falls through to the
	// existing CreationTimestamp/UID fallback for JobOrderFn.
	ssnWithout := &framework.Session{}
	baselineResult := ssnWithout.JobOrderFn(small, large)

	// Session WITH gpujoborder registered via OnSessionOpen (the real
	// registration path), same two jobs, same JobOrderFn call.
	ssnWith := &framework.Session{}
	rp.OnSessionOpen(ssnWith)
	withPluginResult := ssnWith.JobOrderFn(small, large)

	if len(ssnWith.JobOrderFns) != 0 {
		t.Errorf("expected gpujoborder to register 0 JobOrderFns (it should only "+
			"register via AddVictimOrderFn now), got %d", len(ssnWith.JobOrderFns))
	}
	if len(ssnWith.VictimOrderFns) != 1 {
		t.Errorf("expected gpujoborder to register exactly 1 VictimOrderFn, got %d",
			len(ssnWith.VictimOrderFns))
	}
	if baselineResult != withPluginResult {
		t.Errorf("pending-job ordering changed after registering gpujoborder: "+
			"without=%v, with=%v -- expected identical, since gpujoborder must not "+
			"affect JobOrderFn at all", baselineResult, withPluginResult)
	}
}
