// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpujoborder

import (
	"fmt"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/elastic"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
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

func TestVictimOrderFn_SamePriority_EvictLargerFirst_Default(t *testing.T) {
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

func TestVictimOrderFn_EvictSmallerFirstMode(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)
	rp := newPlugin(t, "evict-smaller-first")
	if got := rp.VictimOrderFn(small, large); got != -1 {
		t.Errorf("expected -1 (smaller job preferred as victim in evict-smaller-first mode), got %d", got)
	}
	if got := rp.VictimOrderFn(large, small); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestNew_UnrecognizedMode_FallsBackToEvictLargerFirst(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)
	rp := newPlugin(t, "totally-not-a-real-mode")
	if got := rp.VictimOrderFn(large, small); got != -1 {
		t.Errorf("expected fallback to evict-larger-first behavior, got %d", got)
	}
}

func TestGpujoborder_DoesNotAffect_PendingJobOrdering(t *testing.T) {
	rp := newPlugin(t, "")

	vm := resource_info.NewResourceVectorMap()
	small := makeGPUPodGroup("small", 10, 1, vm)
	large := makeGPUPodGroup("large", 10, 2, vm)

	ssnWithout := &framework.Session{}
	baselineResult := ssnWithout.JobOrderFn(small, large)

	ssnWith := &framework.Session{}
	rp.OnSessionOpen(ssnWith)
	withPluginResult := ssnWith.JobOrderFn(small, large)

	if len(ssnWith.JobOrderFns) != 0 {
		t.Errorf("expected gpujoborder to register 0 JobOrderFns, got %d", len(ssnWith.JobOrderFns))
	}
	if len(ssnWith.VictimOrderFns) != 1 {
		t.Errorf("expected gpujoborder to register exactly 1 VictimOrderFn, got %d", len(ssnWith.VictimOrderFns))
	}
	if baselineResult != withPluginResult {
		t.Errorf("pending-job ordering changed after registering gpujoborder: without=%v, with=%v",
			baselineResult, withPluginResult)
	}
}

func makeElasticJob(uid string, priority int32, minAvailable int32, allocatedTasks int,
	gpuPerTask float64, vm *resource_info.ResourceVectorMap) *podgroup_info.PodGroupInfo {

	pg := podgroup_info.NewPodGroupInfoWithVectorMap(common_info.PodGroupID(uid), vm)
	pg.Priority = priority

	root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	root.AddPodSet(subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, minAvailable, nil))
	pg.RootSubGroupSet = root
	pg.PodSets = root.GetDescendantPodSets()

	for i := 0; i < allocatedTasks; i++ {
		task := &pod_info.PodInfo{
			UID:          common_info.PodID(fmt.Sprintf("%s-task-%d", uid, i)),
			ResReqVector: resource_info.NewResourceVectorWithValues(0, 0, gpuPerTask, vm),
			Status:       pod_status.Running,
		}
		pg.AddTaskInfo(task)
	}
	return pg
}

func TestElasticProtection_OutranksGpujoborder(t *testing.T) {
	rp := newPlugin(t, "")

	vm := resource_info.NewResourceVectorMap()
	atMinLarge := makeElasticJob("at-min-large", 10, 2, 2, 1.0, vm)
	aboveMinSmall := makeElasticJob("above-min-small", 10, 1, 2, 0.5, vm)

	ssn := &framework.Session{}
	ssn.AddJobOrderFn(elastic.JobOrderFn)
	ssn.AddVictimOrderFn(rp.VictimOrderFn)

	victimLessFn := func(l, r interface{}) bool {
		return ssn.VictimOrderFn(l, r)
	}
	pq := scheduler_util.NewPriorityQueue(victimLessFn, scheduler_util.QueueCapacityInfinite)
	pq.Push(atMinLarge)
	pq.Push(aboveMinSmall)

	popped := pq.Pop().(*podgroup_info.PodGroupInfo)

	t.Logf("Popped as victim: %q (GPUs=%v)", popped.UID, popped.GetAliveTasksRequestedGPUs())

	if popped.UID != aboveMinSmall.UID {
		t.Errorf("expected elastic's above-min job to be evicted first (protecting the at-min job), "+
			"but got %q evicted instead -- gpujoborder's raw GPU-size comparison incorrectly "+
			"outranked elastic's protection", popped.UID)
	}
}
