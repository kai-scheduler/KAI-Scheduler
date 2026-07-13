// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// These tests exercise the whole integrated flow end to end: the deferred-resize pod is declared in
// the topology, BuildSession runs the real cluster_info.InjectResizeReservations (exactly as
// production Snapshot() does), and the normal preempt action then frees room for the resulting
// node-pinned reservation - or refuses to, when the queue is over its fair share.

func f64ptr(f float64) *float64 { return &f }

func taskOfJob(ssn *framework.Session, jobName string) *pod_info.PodInfo {
	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(jobName)]
	if job == nil {
		return nil
	}
	for _, task := range job.GetAllPodsMap() {
		return task
	}
	return nil
}

// resizeCompositionTopology builds a full node0 holding a running pod with a kubelet-deferred resize
// (desired 2 cores, actual 1 → delta 1, charged at actual 1) in queue0 and a lower-priority
// same-queue preemptible victim. deservedCPUs sets queue0's fair share (milli-cores): the
// non-preemptible resize may only evict when the queue is entitled to the grown (desired) size, so
// deserved >= 2000 (2 cores) permits it and deserved < 2000 blocks it (quota gate: deserved <
// allocatedNonPreemptible + requested).
func resizeCompositionTopology(name string, deservedCPUs float64, evictions int) test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name: name,
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "resizer",
				RequiredCPUsPerTask: 2, // desired; kubelet-deferred to an actual 1 core below
				Priority:            constants.PriorityBuildNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName:             "node0",
					State:                pod_status.Running,
					ResizeDeferredActual: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				}},
			},
			{
				Name:                "victim",
				RequiredCPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber, // lower priority, preemptible
				QueueName:           "queue0",
				Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node0", State: pod_status.Running}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {
				CPUMillis: 2, CPUMemory: 4000000000,
				Labels: map[string]string{v1.LabelHostname: "node0"}, // reservation pins here via hostname affinity
			},
		},
		Queues: []test_utils.TestQueueBasic{
			// MaxAllowed (limit) set high in milli-cores so the queue's hard cap is never the binding
			// constraint; the deserved-quota gate (deservedCPUs) is what this test exercises.
			{Name: "queue0", DeservedCPUs: f64ptr(deservedCPUs), MaxAllowedCPUs: f64ptr(1000000), MaxAllowedMemory: f64ptr(1e11)},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions: evictions,
				// On success the reservation is pipelined onto the freed node (never bound); on the
				// blocked path neither happens. Both track the single successful resize.
				NumberOfPipelineActions: evictions,
			},
		},
	}
}

// Test A: within quota, the deferred resize's reservation drives preempt to evict the
// lower-priority same-queue victim on its node, and the reservation itself is only pipelined -
// never bound (it has no real pod).
func TestResizeReservationFreesRoomWithinQuota(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(
		resizeCompositionTopology("deferred resize frees room when queue is within quota", 2000, 1), controller)

	preempt.New().Execute(ssn)

	if got := taskOfJob(ssn, "victim").Status; got != pod_status.Releasing {
		t.Fatalf("victim status = %v, want Releasing (reservation should drive preempt to free node0)", got)
	}
	reservation := taskOfJob(ssn, "resizer-resize-reservation")
	if reservation == nil {
		t.Fatal("no resize reservation present in the session")
	}
	if reservation.Status != pod_status.Pipelined {
		t.Errorf("reservation status = %v, want Pipelined (held on node0, never bound)", reservation.Status)
	}
}

// Test B: the reservation carries the resizing pod's NON-preemptible priority, so it inherits
// KAI's existing rule that a queue's non-preemptible (unreclaimable) usage may not exceed its
// deserved share - the same rule any non-preemptible pending pod hits (not cross-queue reclaim,
// not resize-specific). With the queue's deserved below the grown size, KAI frees nothing. A
// preemptible resize is not subject to this cap and would preempt lower-priority same-queue work
// regardless, exactly like any preemptible pod.
func TestResizeReservationRespectsNonPreemptibleQuota(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(
		resizeCompositionTopology("deferred resize blocked when its non-preemptible growth exceeds the queue's deserved share", 1000, 0), controller)

	preempt.New().Execute(ssn)

	if got := taskOfJob(ssn, "victim").Status; got != pod_status.Running {
		t.Fatalf("victim status = %v, want Running (over-quota resize must not evict)", got)
	}
}
