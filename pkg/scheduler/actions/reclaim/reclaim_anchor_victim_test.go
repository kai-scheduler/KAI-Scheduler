// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// Queue hierarchy under test:
//
//	root
//	├── org-parent (priority 110)
//	│   ├── reclaimer-queue  (under its deserved quota -> starved)
//	│   └── batch-queue      (over its deserved quota of 0 -> reclaimable; sibling of reclaimer-queue)
//	└── protected-queue      (priority 100, under its deserved quota -> NOT reclaimable / protected)
//
// A correct reclaim should evict a batch-queue pod (the reclaimer is starved and
// batch-queue is over its deserved quota) and pipeline the reclaimer.
//
// Bug: both protected-queue and org-parent are under their fair share, so the victim
// queue ordering falls to the priority tiebreak. The lower-priority subtree is popped
// first as a victim, and protected-queue (priority 100) sorts before org-parent
// (priority 110), so the protected-queue subtree is popped FIRST. Its node anchors
// every candidate prefix the sub-scenario emitter produces; the reclaimer always
// ends up evicting a protected-queue pod, and the scenario validator (which counts
// the actually-evicted victims) only ever sees protected-queue -> rejected. A
// batch-queue-only victim set is never offered to the validator, so the reclaim
// fails entirely even though batch-queue is reclaimable.
//
// This test asserts the CORRECT behavior and currently FAILS on that bug
// (reclaimer-job stays Pending instead of Pipelined). See the control test below,
// which is identical except protected-queue is removed and the reclaim succeeds.
func TestReclaimAnchorVictim(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	topology := test_utils.TestTopologyBasic{
		Name: "reclaimable sibling never offered because lower-priority protected queue is popped first",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "protected-job",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "protected-queue",
				Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node-protected", State: pod_status.Running}},
			},
			{
				Name:                "batch-job-a",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "batch-queue",
				Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node-batch-1", State: pod_status.Running}},
			},
			{
				Name:                "batch-job-b",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "batch-queue",
				Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node-batch-2", State: pod_status.Running}},
			},
			{
				Name:                "reclaimer-job",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "reclaimer-queue",
				Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-protected": {GPUs: 2},
			"node-batch-1":   {GPUs: 2},
			"node-batch-2":   {GPUs: 2},
			// Tiny spare so both protected-queue and org-parent are under their fair
			// share; too small to host the 2-GPU reclaimer or rehome a 2-GPU victim.
			"node-spare": {GPUs: 1},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "org-parent", DeservedGPUs: 6, GPUOverQuotaWeight: 1, ParentQueue: "root", Priority: ptr.To(110)},
			{Name: "reclaimer-queue", DeservedGPUs: 4, GPUOverQuotaWeight: 1, ParentQueue: "org-parent", Priority: ptr.To(75)},
			{Name: "batch-queue", DeservedGPUs: 0, GPUOverQuotaWeight: 1, ParentQueue: "org-parent", Priority: ptr.To(25)},
			{Name: "protected-queue", DeservedGPUs: 4, GPUOverQuotaWeight: 1, ParentQueue: "root", Priority: ptr.To(100)},
		},
		Departments: []test_utils.TestDepartmentBasic{
			{Name: "root", DeservedGPUs: 10},
		},
		JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"reclaimer-job": {
				GPUsRequired:         2,
				Status:               pod_status.Pipelined,
				DontValidateGPUGroup: true,
			},
			"protected-job": {
				NodeName:             "node-protected",
				GPUsRequired:         2,
				Status:               pod_status.Running, // must never be reclaimed
				DontValidateGPUGroup: true,
			},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds:      5,
				NumberOfCacheEvictions:  2,
				NumberOfPipelineActions: 2,
			},
		},
	}

	ssn := test_utils.BuildSession(topology, controller)
	reclaim.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, topology, ssn)
}

// Control for TestReclaimAnchorVictim: the exact same reclaimer and batch-queue
// configuration, but with the protected queue removed. Here the reclaimer correctly
// reclaims a batch-queue pod and pipelines. The only difference from the failing
// test is the presence of the lower-priority protected queue, which isolates the
// defect to the victim-ordering / scenario-anchoring behavior.
func TestReclaimAnchorVictim_ControlNoProtectedQueue(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	topology := test_utils.TestTopologyBasic{
		Name: "control: without the protected queue the reclaimable sibling is reclaimed",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "batch-job-a",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "batch-queue",
				Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node-batch-1", State: pod_status.Running}},
			},
			{
				Name:                "batch-job-b",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "batch-queue",
				Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node-batch-2", State: pod_status.Running}},
			},
			{
				Name:                "reclaimer-job",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "reclaimer-queue",
				Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-batch-1": {GPUs: 2},
			"node-batch-2": {GPUs: 2},
			"node-spare":   {GPUs: 1},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "org-parent", DeservedGPUs: 6, GPUOverQuotaWeight: 1, ParentQueue: "root", Priority: ptr.To(110)},
			{Name: "reclaimer-queue", DeservedGPUs: 4, GPUOverQuotaWeight: 1, ParentQueue: "org-parent", Priority: ptr.To(75)},
			{Name: "batch-queue", DeservedGPUs: 0, GPUOverQuotaWeight: 1, ParentQueue: "org-parent", Priority: ptr.To(25)},
		},
		Departments: []test_utils.TestDepartmentBasic{
			{Name: "root", DeservedGPUs: 10},
		},
		JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"reclaimer-job": {
				GPUsRequired:         2,
				Status:               pod_status.Pipelined,
				DontValidateGPUGroup: true,
			},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds:      5,
				NumberOfCacheEvictions:  2,
				NumberOfPipelineActions: 2,
			},
		},
	}

	ssn := test_utils.BuildSession(topology, controller)
	reclaim.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, topology, ssn)
}
