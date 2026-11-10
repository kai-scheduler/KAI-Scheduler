// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
)

func TestAllocateHostPortPrefersBindableNodeOverPipeline(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topology := test_utils.TestTopologyBasic{
		Name: "Allocate onto B instead of pipelining onto preferred node A",
		Jobs: []*jobs_fake.TestJobBasic{
			hostPortJob("holder_job0", pod_status.Releasing, "node-a"),
			hostPortJob("pending_job0", pod_status.Pending, ""),
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node-a": {GPUs: 2},
			"node-b": {GPUs: 2},
		},
		Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 4, GPUOverQuotaWeight: 1}},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
			NumberOfCacheBinds: 1,
		}},
		JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"holder_job0":  {GPUsRequired: 1, Status: pod_status.Releasing, NodeName: "node-a"},
			"pending_job0": {GPUsRequired: 1, Status: pod_status.Binding, NodeName: "node-b"},
		},
	}
	ssn := test_utils.BuildSession(topology, controller)
	job := ssn.ClusterInfo.PodGroupInfos["pending_job0"]
	task := job.GetAllPodsMap()["pending_job0-0"]
	nodeA := ssn.ClusterInfo.Nodes["node-a"]
	nodeB := ssn.ClusterInfo.Nodes["node-b"]

	ssn.NodeOrderFns = []api.NodeOrderFn{func(_ *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		if node.Name == "node-a" {
			return 1, nil
		}
		return 0, nil
	}}
	orderedNodes := ssn.OrderedNodesByTask([]*node_info.NodeInfo{nodeA, nodeB}, task)
	require.Len(t, orderedNodes, 2)
	require.Equal(t, "node-a", orderedNodes[0].Name)

	allocate.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, topology, ssn)
}
