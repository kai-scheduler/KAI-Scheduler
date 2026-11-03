// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestReclaimEvictsOnePodGroupAcrossSchedulingCycles(t *testing.T) {
	for _, gpus := range []int{4, 2} {
		t.Run(fmt.Sprintf("reclaimer-%d-gpus", gpus), func(t *testing.T) {
			testReclaimEvictsOnePodGroupAcrossSchedulingCycles(t, gpus)
		})
	}
}

func testReclaimEvictsOnePodGroupAcrossSchedulingCycles(t *testing.T, gpus int) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	topology := releasingAntiAffinityTopology()
	topology.Jobs[0].RequiredGPUsPerTask = float64(gpus)
	evictedGroups := map[common_info.PodGroupID]int{}

	runCycle := func(canBind bool) *framework.Session {
		ssn := test_utils.BuildSession(topology, controller)
		require.Len(t, ssn.ClusterInfo.PodGroupInfos["reclaimer"].PodStatusIndex[pod_status.Pending], 1)
		cacheMock := ssn.Cache.(*cache.MockCache)
		cacheMock.EXPECT().Evict(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ *v1.Pod, job *podgroup_info.PodGroupInfo, _ eviction_info.EvictionMetadata, _ string) error {
				evictedGroups[job.UID]++
				return nil
			}).AnyTimes()
		cacheMock.EXPECT().TaskPipelined(gomock.Any(), gomock.Any()).AnyTimes()
		if canBind {
			cacheMock.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		}
		allocate.New().Execute(ssn)
		reclaim.New().Execute(ssn)
		return ssn
	}

	ssn := runCycle(false)
	require.Len(t, evictedGroups, 1, "one PodGroup must suffice for the reclaimer")
	var victim *jobs_fake.TestJobBasic
	for _, job := range topology.Jobs {
		if count, evicted := evictedGroups[common_info.PodGroupID(job.Name)]; evicted {
			require.Equal(t, 2, count, "the whole two-pod gang must be evicted")
			victim = job
			require.Len(t, ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(job.Name)].PodStatusIndex[pod_status.Releasing], 2)
			for _, task := range victim.Tasks {
				task.State = pod_status.Releasing
			}
		}
	}
	require.NotNil(t, victim)
	victimNode := victim.Tasks[0].NodeName
	reclaimerID := common_info.PodGroupID("reclaimer")
	require.Len(t, ssn.ClusterInfo.PodGroupInfos[reclaimerID].PodStatusIndex[pod_status.Pipelined], 1)

	// Rebuild from cluster-visible state: pipelining is lost, and victims are no longer virtual.
	for cycle := 0; cycle < 5; cycle++ {
		ssn = runCycle(false)
		require.Len(t, evictedGroups, 1, "cycle %d evicted another PodGroup", cycle+2)
		require.Equal(t, 2, evictedGroups[common_info.PodGroupID(victim.Name)])
		for _, task := range ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(victim.Name)].GetAllPodsMap() {
			require.Equal(t, pod_status.Releasing, task.Status)
			require.False(t, task.IsVirtualStatus)
		}
		require.Len(t, ssn.ClusterInfo.PodGroupInfos[reclaimerID].PodStatusIndex[pod_status.Pipelined], 1)
	}

	// Even with spare capacity, anti-affinity must wait for the remaining victim.
	victim.Tasks = victim.Tasks[:1]
	ssn = runCycle(false)
	require.Len(t, evictedGroups, 1, "partial termination must not trigger another eviction")
	require.Len(t, ssn.ClusterInfo.PodGroupInfos[reclaimerID].PodStatusIndex[pod_status.Pipelined], 1)

	for i, job := range topology.Jobs {
		if job == victim {
			topology.Jobs = append(topology.Jobs[:i], topology.Jobs[i+1:]...)
			break
		}
	}
	ssn = runCycle(true)
	require.Len(t, evictedGroups, 1)
	require.Equal(t, 2, evictedGroups[common_info.PodGroupID(victim.Name)])
	for _, task := range ssn.ClusterInfo.PodGroupInfos[reclaimerID].GetAllPodsMap() {
		require.Equal(t, pod_status.Binding, task.Status)
		require.Equal(t, victimNode, task.NodeName)
	}
	for id, job := range ssn.ClusterInfo.PodGroupInfos {
		if id != reclaimerID {
			require.Len(t, job.PodStatusIndex[pod_status.Running], 2, "the other PodGroup must remain running")
		}
	}
}

func releasingAntiAffinityTopology() test_utils.TestTopologyBasic {
	const hostname = "kubernetes.io/hostname"
	victimLabels := map[string]string{"tier": "victim"}
	topology := test_utils.TestTopologyBasic{
		Name: "one victim PodGroup across scheduling cycles with required anti-affinity",
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 4, Labels: map[string]string{hostname: "node0"}},
			"node1": {GPUs: 4, Labels: map[string]string{hostname: "node1"}},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "victims", DeservedGPUs: 0, GPUOverQuotaWeight: 1},
			{Name: "reclaimer", DeservedGPUs: 4, GPUOverQuotaWeight: 1},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
		Jobs: []*jobs_fake.TestJobBasic{{
			Name: "reclaimer", RequiredGPUsPerTask: 4,
			Priority: constants.PriorityTrainNumber, QueueName: "reclaimer",
			Tasks: []*tasks_fake.TestTaskBasic{{
				State: pod_status.Pending, PodAntiAffinitySelector: victimLabels, PodAntiAffinityTopologyKey: hostname,
			}},
		}},
	}
	for i := 0; i < 2; i++ {
		job := &jobs_fake.TestJobBasic{
			Name: fmt.Sprintf("victim%d", i), RequiredGPUsPerTask: 2,
			Priority: constants.PriorityTrainNumber, QueueName: "victims",
		}
		for range 2 {
			job.Tasks = append(job.Tasks, &tasks_fake.TestTaskBasic{
				State: pod_status.Running, NodeName: fmt.Sprintf("node%d", i), PodAffinityLabels: victimLabels,
			})
		}
		topology.Jobs = append(topology.Jobs, job)
	}
	return topology
}
