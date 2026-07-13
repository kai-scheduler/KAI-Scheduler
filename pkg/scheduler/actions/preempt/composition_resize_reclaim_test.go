// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// buildResizeReclaimSession sets up node0 full with a running pod carrying a kubelet-deferred resize
// (desired 2 cores, actual 1 → delta 1, charged at actual 1) in queueA and a running preemptible
// victim from a DIFFERENT queueB. queueB deserves nothing, so its pod is over fair share and
// reclaimable; deservedACPUs (milli-cores) sets queueA's entitlement. A preemptible resizer keeps
// CanReclaimResources (the fair-share gate), not the non-preemptible-quota gate, the sole decider of
// whether reclaim may fire.
//
// The deferred-resize pod is declared in the topology, so BuildSession runs the real
// InjectResizeReservations (as production Snapshot() does) before the session/proportion is built -
// the reservation's pending demand is therefore counted in queueA's fair share.
func buildResizeReclaimSession(t *testing.T, controller *gomock.Controller, deservedACPUs float64, evictions int) *framework.Session {
	t.Helper()
	topology := test_utils.TestTopologyBasic{
		Name: "deferred resize cross-queue reclaim",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "resizer",
				RequiredCPUsPerTask: 2, // desired; kubelet-deferred to an actual 1 core below
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queueA",
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName:             "node0",
					State:                pod_status.Running,
					ResizeDeferredActual: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				}},
			},
			{
				Name:                "victimB",
				RequiredCPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queueB",
				Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node0", State: pod_status.Running}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {CPUMillis: 2, CPUMemory: 4000000000, Labels: map[string]string{v1.LabelHostname: "node0"}},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "queueA", DeservedCPUs: f64ptr(deservedACPUs), GPUOverQuotaWeight: 1},
			{Name: "queueB", DeservedCPUs: f64ptr(0), GPUOverQuotaWeight: 1},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: evictions, NumberOfPipelineActions: evictions},
		},
	}
	return test_utils.BuildSession(topology, controller)
}

// Positive: queueA entitled to the grown size (deserved 2 cores) → the reservation reclaims the
// cross-queue victim (queueB) to free room on node0, and is only pipelined (never bound).
func TestResizeReservationReclaimsCrossQueueWhenEntitled(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	ssn := buildResizeReclaimSession(t, controller, 2000, 1)

	resJob := ssn.ClusterInfo.PodGroupInfos["resizer-resize-reservation"]
	if resJob == nil {
		t.Fatal("no resize reservation injected")
	}
	if !ssn.CanReclaimResources(resJob) {
		t.Fatalf("CanReclaimResources = false, want true (queueA is entitled to the grown size)")
	}

	reclaim.New().Execute(ssn)

	// victimB is in a DIFFERENT queue: Releasing here can only be cross-queue reclaim.
	if got := taskOfJob(ssn, "victimB").Status; got != pod_status.Releasing {
		t.Fatalf("victimB (queueB) status = %v, want Releasing (cross-queue reclaim to free node0)", got)
	}
	if res := taskOfJob(ssn, "resizer-resize-reservation").Status; res == pod_status.Bound || res == pod_status.Binding {
		t.Errorf("reservation status = %v, want never bound (Pipelined at most)", res)
	}
}

// Negative (counterfactual): queueA NOT entitled (deserved only 1 core) → CanReclaimResources gates
// the reservation out, so the cross-queue victim is NOT reclaimed. The resizer is preemptible, so
// this isolates the reclaim fair-share gate (the non-preemptible-quota gate does not apply).
func TestResizeReservationDoesNotReclaimCrossQueueWhenOverShare(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	ssn := buildResizeReclaimSession(t, controller, 1000, 0)

	resJob := ssn.ClusterInfo.PodGroupInfos["resizer-resize-reservation"]
	if resJob == nil {
		t.Fatal("no resize reservation injected")
	}
	if ssn.CanReclaimResources(resJob) {
		t.Fatalf("CanReclaimResources = true, want false (queueA is over its fair share for the growth)")
	}

	reclaim.New().Execute(ssn)

	if got := taskOfJob(ssn, "victimB").Status; got != pod_status.Running {
		t.Fatalf("victimB (queueB) status = %v, want Running (over-share resize must not reclaim cross-queue)", got)
	}
}
