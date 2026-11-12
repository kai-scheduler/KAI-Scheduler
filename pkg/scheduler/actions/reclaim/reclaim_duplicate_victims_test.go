// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// The pending gang needs two victims from the same elastic job. The solver first probes one pending
// task and records one victim, then probes both tasks with that victim recorded and a second one as
// potential; each victim pod must still be evicted, and charged back to its queue, only once.
func TestReclaimEvictsRecordedElasticVictimOnce(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topology := test_utils.TestTopologyBasic{
		Name: "Reclaim two tasks from one elastic job for a two-task gang",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "elastic-job",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
				Tasks: []*tasks_fake.TestTaskBasic{
					{NodeName: "node0", State: pod_status.Running},
					{NodeName: "node0", State: pod_status.Running},
					{NodeName: "node0", State: pod_status.Running},
					{NodeName: "node0", State: pod_status.Running},
				},
			},
			{
				Name:                "pending-job",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue1",
				RootSubGroupSet:     jobs_fake.DefaultSubGroup(2),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 4},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue0", DeservedGPUs: 1, GPUOverQuotaWeight: 1},
			{Name: "queue1", DeservedGPUs: 2, GPUOverQuotaWeight: 1},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds:      2,
				NumberOfPipelineActions: 2,
			},
		},
	}

	ssn := test_utils.BuildSession(topology, controller)
	evictionsByPod := map[string]int{}
	ssn.Cache.(*cache.MockCache).EXPECT().Evict(Any(), Any(), Any(), Any()).DoAndReturn(
		func(pod *v1.Pod, _ *podgroup_info.PodGroupInfo, _ eviction_info.EvictionMetadata, _ string) error {
			evictionsByPod[pod.Name]++
			return nil
		}).AnyTimes()

	reclaim.New().Execute(ssn)

	if len(evictionsByPod) == 0 {
		t.Fatalf("reclaim evicted nothing; the scenario did not run")
	}
	for podName, count := range evictionsByPod {
		if count != 1 {
			t.Errorf("pod %s was evicted %d times in one reclaim action, want 1", podName, count)
		}
	}

	queue := ssn.ClusterInfo.Queues["queue0"]
	var runningGPUs, runningMilliCPU, runningMemory float64
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if job.Queue != queue.UID {
			continue
		}
		for _, task := range job.GetAllPodsMap() {
			if task.Status != pod_status.Running {
				continue
			}
			runningGPUs += task.AcceptedResourceVector.TotalGPUs(task.VectorMap)
			runningMilliCPU += task.AcceptedResourceVector.Get(resource_info.CPUIndex)
			runningMemory += task.AcceptedResourceVector.Get(resource_info.MemoryIndex)
		}
	}
	allocated := ssn.QueueAllocatedResources(queue)
	if allocated.GPUs() != runningGPUs || allocated.Cpu() != runningMilliCPU || allocated.Memory() != runningMemory {
		t.Errorf("queue %s allocated <gpus %v, cpu %vm, memory %v>, its running pods use <gpus %v, cpu %vm, memory %v>",
			queue.Name, allocated.GPUs(), allocated.Cpu(), allocated.Memory(),
			runningGPUs, runningMilliCPU, runningMemory)
	}
}
