// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
	commonconstants "github.com/kai-scheduler/api/constants"
)

func elasticRemainderCycle(tb testing.TB) []string {
	tb.Helper()
	root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	root.AddPodSet(subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil))
	victim := &jobs_fake.TestJobBasic{Name: "elastic-victim", QueueName: "victims", RequiredGPUsPerTask: 1,
		Priority: constants.PriorityTrainNumber, RootSubGroupSet: root}
	pending := &jobs_fake.TestJobBasic{Name: "pending", QueueName: "pending", RequiredGPUsPerTask: 1,
		Priority: constants.PriorityTrainNumber}
	for i := 0; i < 32; i++ {
		victim.Tasks = append(victim.Tasks, &tasks_fake.TestTaskBasic{
			Name: fmt.Sprintf("victim-%02d", i), State: pod_status.Running, NodeName: fmt.Sprintf("node%d", i/8)})
	}
	for i := 0; i < 16; i++ {
		pending.Tasks = append(pending.Tasks, &tasks_fake.TestTaskBasic{
			Name: fmt.Sprintf("pending-%02d", i), State: pod_status.Pending, NodeAffinityNames: []string{fmt.Sprintf("node%d", 2+i/8)}})
	}
	topology := test_utils.TestTopologyBasic{
		Name: "committed elastic victim remainders", Jobs: []*jobs_fake.TestJobBasic{victim, pending},
		Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 8}, "node1": {GPUs: 8}, "node2": {GPUs: 8}, "node3": {GPUs: 8}},
		Queues: []test_utils.TestQueueBasic{{Name: "victims", DeservedGPUs: 16}, {Name: "pending", DeservedGPUs: 16}},
		Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 16, NumberOfPipelineActions: 16}},
	}
	ctrl := gomock.NewController(tb)
	defer ctrl.Finish()
	ssn := test_utils.BuildSession(topology, ctrl)
	ssn.AddTaskOrderFn(func(l, r interface{}) int {
		leftTask, rightTask := l.(*pod_info.PodInfo), r.(*pod_info.PodInfo)
		left, right := leftTask.NodeName+"/"+leftTask.Name, rightTask.NodeName+"/"+rightTask.Name
		if left < right {
			return -1
		}
		if left > right {
			return 1
		}
		return 0
	})
	registrations := ssn.ScenarioGeneratorRegistrations
	ssn.ScenarioGeneratorRegistrations = nil
	for _, registration := range registrations {
		if registration.Name == commonconstants.GeneratorMultiNodeGang {
			ssn.ScenarioGeneratorRegistrations = append(ssn.ScenarioGeneratorRegistrations, registration)
		}
	}
	require.Len(tb, ssn.ScenarioGeneratorRegistrations, 1)
	for _, action := range manySingleGPUJobsSchedulingCycleActions() {
		action.Execute(ssn)
	}
	var outcome []string
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		releasing, pipelined := 0, 0
		for _, task := range job.GetAllPodsMap() {
			if task.Status == pod_status.Releasing {
				releasing++
			}
			if task.Status == pod_status.Pipelined {
				pipelined++
			}
			outcome = append(outcome, fmt.Sprintf("%s/%s:%v:%s", job.Name, task.Name, task.Status, task.NodeName))
			if job.Name == victim.Name {
				want := pod_status.Running
				if task.NodeName >= "node2" {
					want = pod_status.Releasing
				}
				require.Equal(tb, want, task.Status, task.Name)
			} else {
				require.Equal(tb, pod_status.Pipelined, task.Status, task.Name)
			}
		}
		if job.Name == victim.Name {
			require.Equal(tb, 16, releasing)
		} else {
			require.Equal(tb, 16, pipelined)
		}
	}
	sort.Strings(outcome)
	return outcome
}

func TestElasticRemainderCommittedCycle(t *testing.T) {
	defer gock.Off()
	t.Logf("canonical outcome: %v", elasticRemainderCycle(t))
}

func BenchmarkElasticRemainderCommittedCycle(b *testing.B) {
	defer gock.Off()
	b.ReportAllocs()
	for b.Loop() {
		elasticRemainderCycle(b)
	}
}
