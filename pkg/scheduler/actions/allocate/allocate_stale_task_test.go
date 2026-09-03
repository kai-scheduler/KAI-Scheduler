// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// TestAllocateTerminatesWithStaleTaskInCache reproduces the scheduler wedge seen when a job's
// cached tasks-to-allocate list names a task whose UID is not in the job's pod map, while the job
// still has a pending task.
//
// The only node's GPU is held by a Releasing pod, so the pending job can only be pipelined. With
// the stale task, Statement.Pipeline cannot update the task's status in the job. If that failure
// is swallowed, the attempt looks successful, the job still has a pending task, and the allocate
// action requeues it and repeats the same attempt for the rest of the session: the scheduler spins
// at full CPU and schedules nothing else. The action must terminate instead, leaving the job
// untouched for this session.
func TestAllocateTerminatesWithStaleTaskInCache(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topology := test_utils.TestTopologyBasic{
		Name: "stale task in tasks-to-allocate cache",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "releasing_job",
				RequiredGPUsPerTask: 1,
				QueueName:           "queue0",
				Priority:            constants.PriorityTrainNumber,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Releasing, NodeName: "node0"},
				},
			},
			{
				Name:                "pending_job",
				RequiredGPUsPerTask: 1,
				QueueName:           "queue0",
				Priority:            constants.PriorityTrainNumber,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 1},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue0", DeservedGPUs: 1},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: 0,
				// Deliberately permissive: on the broken code every loop iteration commits a
				// pipeline, and a tight mock limit would stop the action before the hang is
				// visible. The test must observe the loop itself; side effects are asserted below.
				NumberOfPipelineActions: 1 << 20,
			},
		},
	}
	ssn := test_utils.BuildSession(topology, controller)

	job := ssn.ClusterInfo.PodGroupInfos["pending_job"]
	require.NotNil(t, job)
	trackedTask := job.GetAllPodsMap()["pending_job-0"]
	require.NotNil(t, trackedTask)

	// Populate the job's tasks-to-allocate cache, then make it point at a task the job does not
	// track: a clone of the pending task under a UID that is not in the job's pod map. The tracked
	// task itself stays pending, so the job still looks like it has work to do.
	cached := podgroup_info.GetTasksToAllocate(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, true)
	require.Len(t, cached, 1)
	staleTask := trackedTask.Clone()
	staleTask.UID = common_info.PodID("pending_job-0-stale")
	cached[0] = staleTask
	require.Same(t, staleTask, podgroup_info.GetTasksToAllocate(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, true)[0],
		"test setup: the job's cache must now hold the stale task")

	done := make(chan struct{})
	go func() {
		defer close(done)
		allocate.New().Execute(ssn)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("allocate action did not terminate: a stale task in the tasks-to-allocate cache must not requeue the job forever")
	}

	assert.Equal(t, pod_status.Pending, trackedTask.Status, "the tracked task must stay pending this session")
	assert.Equal(t, "", trackedTask.NodeName, "the tracked task must not be placed")
	assert.Equal(t, "", staleTask.NodeName, "the stale task must not be placed")
	assert.NotContains(t, ssn.ClusterInfo.Nodes["node0"].PodInfos, staleTask.UID, "stale task must not be added to node0")
}
