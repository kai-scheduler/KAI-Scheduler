// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestFittingNodeRebuildsAffinityCountsAfterPrefilterReruns(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	ctrl := gomock.NewController(t)
	preprocessLabels := map[string]string{"tier": "preprocess"}
	rackLabels := func(node, rack string) map[string]string {
		return map[string]string{"kubernetes.io/hostname": node, "rack": rack}
	}
	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "pod allocated between two attempts blocks the pending pod",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:      "releasing",
				Priority:  constants.PriorityTrainNumber,
				QueueName: "queue",
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName: "node1", State: pod_status.Releasing, PodAffinityLabels: preprocessLabels,
				}},
			},
			{
				Name:                "blocker",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue",
				Tasks: []*tasks_fake.TestTaskBasic{{
					State: pod_status.Pending, PodAffinityLabels: preprocessLabels,
				}},
			},
			{
				Name:                "pending",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue",
				Tasks: []*tasks_fake.TestTaskBasic{{
					State: pod_status.Pending, PodAntiAffinitySelector: preprocessLabels, PodAntiAffinityTopologyKey: "rack",
				}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 1, Labels: rackLabels("node0", "r1")},
			"node1": {GPUs: 0, Labels: rackLabels("node1", "r1")},
			"node2": {GPUs: 2, Labels: rackLabels("node2", "r2")},
		},
		Queues: []test_utils.TestQueueBasic{{Name: "queue", DeservedGPUs: 4}},
		Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, ctrl)

	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("pending")]
	task := job.GetAllPodsMap()[common_info.PodID("pending-0")]
	blocker := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("blocker")].
		GetAllPodsMap()[common_info.PodID("blocker-0")]
	node0, node2 := ssn.ClusterInfo.Nodes["node0"], ssn.ClusterInfo.Nodes["node2"]

	require.NoError(t, ssn.PrePredicateFn(task, job))
	fits, mustPipeline := ssn.FittingNode(task, node0, false)
	require.True(t, fits && mustPipeline, "node0 fits once the releasing pod is gone")
	fits, _ = ssn.FittingNode(task, node2, false)
	require.True(t, fits, "node2 fits before the blocker is allocated")

	require.NoError(t, ssn.Statement().Allocate(blocker, "node2"))
	require.NoError(t, ssn.PrePredicateFn(task, job))
	fits, _ = ssn.FittingNode(task, node2, false)
	assert.False(t, fits, "the blocker on rack r2 rejects node2")
	fits, mustPipeline = ssn.FittingNode(task, node0, false)
	assert.True(t, fits && mustPipeline, "node0 still fits once the releasing pod is gone")
}
