// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package multinodegang_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/multinodegang"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestMultiNodeGangGeneratorEmitsScenariosThenUntypedNil(t *testing.T) {
	defer gock.Off()

	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	defer controller.Finish()
	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "multinodegang generator",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "running-job",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue-0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{NodeName: "node-0", State: pod_status.Running},
					{NodeName: "node-1", State: pod_status.Running},
				},
			},
			{
				Name:                "pending-job",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue-1",
				Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-0": {GPUs: 1},
			"node-1": {GPUs: 1},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue-0", DeservedGPUs: 1},
			{Name: "queue-1", DeservedGPUs: 1},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, controller)

	generator := multinodegang.NewMultiNodeGangGenerator(&solvers.SolveContext{
		Session:           ssn,
		ActionType:        framework.Reclaim,
		PartialPendingJob: findJobByName(t, ssn, "pending-job"),
		GenerateVictimsQueue: func() *utils.JobsOrderByQueues {
			return utils.GetVictimsQueue(ssn, nil)
		},
		FeasibleNodes: ssn.ClusterInfo.Nodes,
	})
	require.NotNil(t, generator)

	emitted := 0
	exhausted := false
	for attempt := 0; attempt < 100; attempt++ {
		sn := generator.Next()
		if sn == nil {
			exhausted = true
			break
		}
		byNodeScenario, ok := sn.(*scenario.ByNodeScenario)
		require.True(t, ok)
		require.NotNil(t, byNodeScenario)
		emitted++
	}

	require.Greater(t, emitted, 0)
	require.True(t, exhausted, "exhaustion must be signaled with an untyped nil interface")
}

func findJobByName(t *testing.T, ssn *framework.Session, name string) *podgroup_info.PodGroupInfo {
	t.Helper()

	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if job.Name == name {
			return job
		}
	}
	t.Fatalf("job %q not found in session", name)
	return nil
}
