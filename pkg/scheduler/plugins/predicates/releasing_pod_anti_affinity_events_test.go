// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestReleasingTaskSetsTrackStatementChanges(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	const hostname = "kubernetes.io/hostname"
	for _, initialStatus := range []pod_status.PodStatus{pod_status.Running, pod_status.Releasing} {
		for _, symmetric := range []bool{false, true} {
			for _, destination := range []string{"node0", "node1"} {
				t.Run(fmt.Sprintf("%s-symmetric-%t-pipeline-%s", initialStatus, symmetric, destination), func(t *testing.T) {
					victim := &tasks_fake.TestTaskBasic{State: initialStatus, NodeName: "node0", PodAffinityLabels: map[string]string{"tier": "victim"}}
					incoming := &tasks_fake.TestTaskBasic{State: pod_status.Pending, PodAffinityLabels: map[string]string{"tier": "train"}, PodAntiAffinitySelector: map[string]string{"tier": "victim"}, PodAntiAffinityTopologyKey: hostname}
					if symmetric {
						incoming.PodAntiAffinitySelector = nil
						victim.PodAntiAffinitySelector = map[string]string{"tier": "train"}
						victim.PodAntiAffinityTopologyKey = hostname
					}
					ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
						Nodes: map[string]nodes_fake.TestNodeBasic{
							"node0": {GPUs: 4, Labels: map[string]string{hostname: "node0"}},
							"node1": {GPUs: 4, Labels: map[string]string{hostname: "node1"}},
						},
						Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 4, GPUOverQuotaWeight: 1}},
						Jobs: []*jobs_fake.TestJobBasic{
							{Name: "victim", QueueName: "queue0", Priority: constants.PriorityTrainNumber, RequiredGPUsPerTask: 1, Tasks: []*tasks_fake.TestTaskBasic{victim}},
							{Name: "incoming", QueueName: "queue0", Priority: constants.PriorityTrainNumber, RequiredGPUsPerTask: 1, Tasks: []*tasks_fake.TestTaskBasic{incoming}},
						},
						Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
					}, gomock.NewController(t))
					var victimTask, incomingTask *pod_info.PodInfo
					for _, task := range ssn.ClusterInfo.PodGroupInfos["victim"].GetAllPodsMap() {
						victimTask = task
					}
					for _, task := range ssn.ClusterInfo.PodGroupInfos["incoming"].GetAllPodsMap() {
						incomingTask = task
					}
					assertReady := func(expected bool) {
						t.Helper()
						ready, err := ssn.IsTaskReadyForBinding(incomingTask, ssn.ClusterInfo.Nodes["node0"])
						require.NoError(t, err)
						require.Equal(t, expected, ready)
					}
					assertReady(initialStatus != pod_status.Releasing)
					stmt := ssn.Statement()
					beforeEvict := stmt.Checkpoint()
					// Eviction followed by rollback restores the initial readiness.
					require.NoError(t, stmt.Evict(victimTask, "", eviction_info.EvictionMetadata{}))
					assertReady(false)
					beforePipeline := stmt.Checkpoint()
					require.NoError(t, stmt.Pipeline(victimTask, destination, true))
					assertReady(destination == "node0")
					require.NoError(t, stmt.Rollback(beforePipeline))
					assertReady(destination == "node0")
					require.NoError(t, stmt.Rollback(beforeEvict))
					assertReady(initialStatus != pod_status.Releasing)
				})
			}
		}
	}
}
