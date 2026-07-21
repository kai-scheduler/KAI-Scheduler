// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestFittingNodeAggregatesCompactReasonCounts(t *testing.T) {
	ssn, job, task := buildDetailedFitErrorSession(t)
	ssn.AddPredicateFn(func(
		task *pod_info.PodInfo,
		_ *podgroup_info.PodGroupInfo,
		node *node_info.NodeInfo,
	) error {
		if node.Name == "node-a" {
			return common_info.NewFitError(task.Name, task.Namespace, node.Name, "NodeAffinityMismatch")
		}
		return common_info.NewFitError(task.Name, task.Namespace, node.Name, "NoStorage")
	})

	for _, nodeName := range []string{"node-a", "node-b"} {
		require.False(t, ssn.FittingNode(task, ssn.ClusterInfo.Nodes[nodeName], true))
	}

	fitErrors := job.TasksFitErrors[task.UID]
	require.NotNil(t, fitErrors)
	require.Equal(t, 1, fitErrors.ReasonCount("NodeAffinityMismatch"))
	require.Equal(t, 1, fitErrors.ReasonCount("NoStorage"))
	require.Equal(t, 2, fitErrors.UniqueReasonCount())
}

func TestRecomputeDetailedFitErrorsRunsPredicatesForEveryNode(t *testing.T) {
	ssn, job, task := buildDetailedFitErrorSession(t)
	ssn.AddPredicateFn(func(
		task *pod_info.PodInfo,
		_ *podgroup_info.PodGroupInfo,
		node *node_info.NodeInfo,
	) error {
		if node.Name == "node-a" {
			return common_info.NewFitErrorWithDetailedMessage(
				task.Name, task.Namespace, node.Name,
				[]string{"NodeAffinityMismatch"}, "node-a affinity details")
		}
		return common_info.NewFitErrorWithDetailedMessage(
			task.Name, task.Namespace, node.Name,
			[]string{"NoStorage"}, "node-b storage details")
	})

	nodeErrors, err := ssn.RecomputeDetailedFitErrors(job, task)
	require.NoError(t, err)
	require.Len(t, nodeErrors, 2)

	detailsByNode := make(map[string][]string, len(nodeErrors))
	for _, fitError := range nodeErrors {
		detailsByNode[fitError.NodeName] = fitError.DetailedReasons
	}
	require.Equal(t, []string{"node-a affinity details"}, detailsByNode["node-a"])
	require.Equal(t, []string{"node-b storage details"}, detailsByNode["node-b"])
}

func TestRecomputeDetailedFitErrorsStopsAtPrePredicate(t *testing.T) {
	ssn, job, task := buildDetailedFitErrorSession(t)
	ssn.AddPrePredicateFn(func(*pod_info.PodInfo, *podgroup_info.PodGroupInfo) error {
		return &common_info.NotFoundError{Name: "missing-pvc"}
	})
	predicateCalls := 0
	ssn.AddPredicateFn(func(*pod_info.PodInfo, *podgroup_info.PodGroupInfo, *node_info.NodeInfo) error {
		predicateCalls++
		return nil
	})

	nodeErrors, err := ssn.RecomputeDetailedFitErrors(job, task)
	require.NoError(t, err)
	require.Empty(t, nodeErrors)
	require.Zero(t, predicateCalls)
}

func TestRecomputeDetailedFitErrorsBuildsCurrentResourceDetails(t *testing.T) {
	ssn, job, task := buildDetailedFitErrorSession(t)
	for _, node := range ssn.ClusterInfo.Nodes {
		node.IdleVector.Set(resource_info.GPUIndex, 0)
		node.UsedVector.Set(resource_info.GPUIndex, 1)
		require.False(t, ssn.FittingNode(task, node, true))

		node.IdleVector.Set(resource_info.GPUIndex, 0.5)
		node.UsedVector.Set(resource_info.GPUIndex, 0.5)
	}

	nodeErrors, err := ssn.RecomputeDetailedFitErrors(job, task)
	require.NoError(t, err)
	require.Len(t, nodeErrors, 2)
	for _, fitError := range nodeErrors {
		require.Len(t, fitError.DetailedReasons, 1)
		require.True(t, strings.Contains(fitError.DetailedReasons[0], "requested: 1"))
		require.True(t, strings.Contains(fitError.DetailedReasons[0], "used: 0.5"))
		require.True(t, strings.Contains(fitError.DetailedReasons[0], "capacity: 1"))
		require.NotContains(t, fitError.DetailedReasons[0], "used: 1")
	}
}

func TestRecomputeDetailedFitErrorsSkipsTypedNilFitError(t *testing.T) {
	ssn, job, task := buildDetailedFitErrorSession(t)
	ssn.AddPredicateFn(func(
		task *pod_info.PodInfo,
		_ *podgroup_info.PodGroupInfo,
		node *node_info.NodeInfo,
	) error {
		if node.Name == "node-a" {
			return common_info.NewFitError(task.Name, task.Namespace, node.Name, "NodeAffinityMismatch")
		}
		var fitError *common_info.TasksFitError
		return fitError
	})

	nodeErrors, err := ssn.RecomputeDetailedFitErrors(job, task)

	require.NoError(t, err)
	require.Len(t, nodeErrors, 1)
	require.Equal(t, "node-a", nodeErrors[0].NodeName)
}

func buildDetailedFitErrorSession(
	t *testing.T,
) (*framework.Session, *podgroup_info.PodGroupInfo, *pod_info.PodInfo) {
	t.Helper()
	test_utils.InitTestingInfrastructure()
	ctrl := gomock.NewController(t)
	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "detailed fit error recomputation",
		Jobs: []*jobs_fake.TestJobBasic{{
			Name:                "pending-job",
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           "queue",
			Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
		}},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-a": {GPUs: 1},
			"node-b": {GPUs: 1},
		},
		Queues: []test_utils.TestQueueBasic{{Name: "queue", DeservedGPUs: 2}},
	}, ctrl)
	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("pending-job")]
	task := job.GetAllPodsMap()[common_info.PodID("pending-job-0")]
	return ssn, job, task
}
