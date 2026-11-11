// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation_test

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestConsolidationCooldown(t *testing.T) {
	test_utils.InitTestingInfrastructure()

	consolidated := map[string]test_utils.TestExpectedResultBasic{
		"running_job0": {NodeName: "node0", GPUsRequired: 2, Status: pod_status.Running},
		"running_job1": {NodeName: "node0", GPUsRequired: 2, Status: pod_status.Pipelined},
		"pending_job":  {NodeName: "node1", GPUsRequired: 3, Status: pod_status.Pipelined},
	}
	untouched := map[string]test_utils.TestExpectedResultBasic{
		"running_job0": {NodeName: "node0", GPUsRequired: 2, Status: pod_status.Running},
		"running_job1": {NodeName: "node1", GPUsRequired: 2, Status: pod_status.Running},
		"pending_job":  {GPUsRequired: 3, Status: pod_status.Pending},
	}

	for _, tc := range []struct {
		name       string
		cooldown   time.Duration
		evictedAgo *time.Duration
		expected   map[string]test_utils.TestExpectedResultBasic
	}{
		{
			name:       "evicted within the cooldown: does not trigger consolidation",
			cooldown:   10 * time.Minute,
			evictedAgo: ptrDuration(time.Minute),
			expected:   untouched,
		},
		{
			name:       "evicted before the cooldown: triggers consolidation",
			cooldown:   10 * time.Minute,
			evictedAgo: ptrDuration(11 * time.Minute),
			expected:   consolidated,
		},
		{
			name:       "cooldown disabled: a just-evicted job triggers consolidation",
			evictedAgo: ptrDuration(time.Minute),
			expected:   consolidated,
		},
		{
			name:     "never evicted: triggers consolidation",
			cooldown: 10 * time.Minute,
			expected: consolidated,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller := NewController(t)
			defer controller.Finish()

			cacheRequirements := &test_utils.CacheMocking{}
			if tc.expected["pending_job"].Status == pod_status.Pipelined {
				cacheRequirements.NumberOfCacheEvictions = 1
				cacheRequirements.NumberOfPipelineActions = 2
			}
			topology := test_utils.TestTopologyBasic{
				Name: tc.name,
				Jobs: []*jobs_fake.TestJobBasic{
					runningJob("running_job0", "node0"),
					runningJob("running_job1", "node1"),
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 4},
					"node1": {GPUs: 4},
				},
				Queues:             []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 2}},
				JobExpectedResults: tc.expected,
				Mocks:              &test_utils.TestMock{CacheRequirements: cacheRequirements},
			}

			ssn := test_utils.BuildSession(topology, controller)
			ssn.SchedulerParams.ConsolidationCooldown = tc.cooldown
			if tc.evictedAgo != nil {
				for _, job := range ssn.ClusterInfo.PodGroupInfos {
					if job.Name == "pending_job" {
						evictedAt := time.Now().Add(-*tc.evictedAgo)
						job.LastEvictionTimestamp = &evictedAt
					}
				}
			}

			consolidation.New().Execute(ssn)
			test_utils.MatchExpectedAndRealTasks(t, 0, topology, ssn)
		})
	}
}

func runningJob(name, node string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: 2,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           "queue0",
		Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: node, State: pod_status.Running}},
	}
}

func ptrDuration(d time.Duration) *time.Duration {
	return &d
}
