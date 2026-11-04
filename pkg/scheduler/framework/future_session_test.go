// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// A releasing pod holds a host port that two pending pods need. The first fits once the releasing
// pod is gone; checking it leaves the live session as it was. Once the first is pipelined, the copy
// is rebuilt and the second no longer fits.
func TestFittingNodeOnFutureCopy(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	ctrl := gomock.NewController(t)
	portJob := func(name string, state pod_status.PodStatus, nodeName string) *jobs_fake.TestJobBasic {
		return &jobs_fake.TestJobBasic{
			Name: name, RequiredGPUsPerTask: 1, Priority: constants.PriorityTrainNumber, QueueName: "queue",
			Tasks: []*tasks_fake.TestTaskBasic{{State: state, NodeName: nodeName, HostPorts: []int32{8080}}},
		}
	}
	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "future copy",
		Jobs: []*jobs_fake.TestJobBasic{
			portJob("releasing", pod_status.Releasing, "node0"),
			portJob("pending-a", pod_status.Pending, ""),
			portJob("pending-b", pod_status.Pending, ""),
		},
		Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 3}},
		Queues: []test_utils.TestQueueBasic{{Name: "queue", DeservedGPUs: 3}},
		Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}, ctrl)
	node := ssn.ClusterInfo.Nodes["node0"]
	livePods := func() int {
		return len(node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo.Pods)
	}
	task := func(job string) *pod_info.PodInfo {
		return ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(job)].
			GetAllPodsMap()[common_info.PodID(job+"-0")]
	}
	podsBefore := livePods()

	taskA := task("pending-a")
	require.NoError(t, ssn.PrePredicateFn(taskA, ssn.ClusterInfo.PodGroupInfos["pending-a"]))
	fits, mustPipeline := ssn.FittingNode(taskA, node, false)
	assert.True(t, fits)
	assert.True(t, mustPipeline)
	assert.Equal(t, podsBefore, livePods(), "the check must not change the live node")
	assert.Equal(t, pod_status.Releasing, task("releasing").Status)

	require.NoError(t, ssn.Statement().Pipeline(taskA, node.Name, false))

	taskB := task("pending-b")
	require.NoError(t, ssn.PrePredicateFn(taskB, ssn.ClusterInfo.PodGroupInfos["pending-b"]))
	fits, _ = ssn.FittingNode(taskB, node, false)
	assert.False(t, fits, "the pipelined pod holds the port in the future")
}
