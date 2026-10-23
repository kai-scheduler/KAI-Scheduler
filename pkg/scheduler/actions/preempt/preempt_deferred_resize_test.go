// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
	apiconstants "github.com/kai-scheduler/api/constants"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

const resizerPriority = constants.PriorityInteractivePreemptibleNumber

func deferredResizeTask(nodeName string, actualCPUs float64) *tasks_fake.TestTaskBasic {
	return &tasks_fake.TestTaskBasic{
		State:        pod_status.Running,
		NodeName:     nodeName,
		ResizeStatus: &tasks_fake.TestResizeStatus{ActualCPUs: actualCPUs, Deferred: true},
	}
}

func deferredResizeJob(name, nodeName string, desiredCPUs, actualCPUs float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		QueueName:           "queue0",
		Priority:            resizerPriority,
		RequiredCPUsPerTask: desiredCPUs,
		Tasks:               []*tasks_fake.TestTaskBasic{deferredResizeTask(nodeName, actualCPUs)},
	}
}

func runningCPUJob(name, queueName, nodeName string, priority int32, cpus float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		QueueName:           queueName,
		Priority:            priority,
		RequiredCPUsPerTask: cpus,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: nodeName}},
	}
}

func cpuNodes(names ...string) map[string]nodes_fake.TestNodeBasic {
	nodes := map[string]nodes_fake.TestNodeBasic{}
	for _, name := range names {
		nodes[name] = nodes_fake.TestNodeBasic{CPUMillis: 4}
	}
	return nodes
}

func expectStatuses(nodeName string, statuses map[string]pod_status.PodStatus) map[string]test_utils.TestExpectedResultBasic {
	results := map[string]test_utils.TestExpectedResultBasic{}
	for jobName, status := range statuses {
		results[jobName] = test_utils.TestExpectedResultBasic{NodeName: nodeName, Status: status}
	}
	return results
}

func noEvictionMocks() *test_utils.TestMock {
	return &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}}
}

func evictionMocks(evictions int) *test_utils.TestMock {
	return &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: evictions}}
}

func TestPreemptForDeferredResize(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, topology := range allDeferredResizePreemptTopologies() {
		t.Logf("Running Test %d: %s", testNumber, topology.Name)
		ssn := test_utils.BuildSession(topology, controller)
		preempt.New().Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, topology, ssn)
	}
}

func allDeferredResizePreemptTopologies() []test_utils.TestTopologyBasic {
	return append(deferredResizePreemptTopologies(), deferredResizePreemptEdgeTopologies()...)
}

func deferredResizePreemptTopologies() []test_utils.TestTopologyBasic {
	queues := []test_utils.TestQueueBasic{{Name: "queue0"}}
	nonPreemptibleVictim := runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2)
	nonPreemptibleVictim.Preemptibility = enginev2alpha2.NonPreemptible

	return []test_utils.TestTopologyBasic{
		{
			Name: "evicts a lower priority victim on the resizing pod's node",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "victim only on another node",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				runningCPUJob("peer", "queue0", "node0", resizerPriority, 2),
				runningCPUJob("victim", "queue0", "node1", constants.PriorityTrainNumber, 2),
			},
			Nodes:  cpuNodes("node0", "node1"),
			Queues: queues,
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer": {NodeName: "node0", Status: pod_status.Running},
				"peer":    {NodeName: "node0", Status: pod_status.Running},
				"victim":  {NodeName: "node1", Status: pod_status.Running},
			},
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "no room even after evicting all eligible victims",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 4, 1),
				runningCPUJob("peer", "queue0", "node0", resizerPriority, 1),
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 1),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"peer":    pod_status.Running,
				"victim":  pod_status.Running,
			}),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "node already has room for the resize",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 1),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Running,
			}),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "feature disabled",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Running,
			}),
			Mocks: noEvictionMocks(),
		},
		{
			Name: "equal priority victim is not preempted",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				runningCPUJob("victim", "queue0", "node0", resizerPriority, 2),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Running,
			}),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "higher priority victim is not preempted",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				runningCPUJob("victim", "queue0", "node0", constants.PriorityBuildNumber, 2),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Running,
			}),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "non-preemptible victim is not preempted",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				nonPreemptibleVictim,
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Running,
			}),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "gang victim spanning nodes moves to its other node when it fits there",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				{
					Name:                "victim",
					QueueName:           "queue0",
					Priority:            constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 2,
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(2),
					Tasks: []*tasks_fake.TestTaskBasic{
						{State: pod_status.Running, NodeName: "node0"},
						{State: pod_status.Running, NodeName: "node1"},
					},
				},
			},
			Nodes:  cpuNodes("node0", "node1"),
			Queues: queues,
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer": {NodeName: "node0", Status: pod_status.Running},
			},
			TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"victim-0": {NodeName: "node1", Status: pod_status.Pipelined},
				"victim-1": {NodeName: "node1", Status: pod_status.Running},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions: 1, NumberOfPipelineActions: 1,
			}},
			DeferredResizePreemption: true,
		},
		{
			Name: "gang victim spanning nodes is evicted whole when it cannot move",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				{
					Name:                "victim",
					QueueName:           "queue0",
					Priority:            constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 2,
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(2),
					Tasks: []*tasks_fake.TestTaskBasic{
						{State: pod_status.Running, NodeName: "node0"},
						{State: pod_status.Running, NodeName: "node1"},
					},
				},
				runningCPUJob("blocker", "queue0", "node1", resizerPriority, 2),
			},
			Nodes:  cpuNodes("node0", "node1"),
			Queues: queues,
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer": {NodeName: "node0", Status: pod_status.Running},
				"victim":  {Status: pod_status.Releasing},
				"blocker": {NodeName: "node1", Status: pod_status.Running},
			},
			Mocks:                    evictionMocks(2),
			DeferredResizePreemption: true,
		},
		{
			Name: "elastic resizing job",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "resizer",
					QueueName:           "queue0",
					Priority:            resizerPriority,
					RequiredCPUsPerTask: 3,
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						{State: pod_status.Running, NodeName: "node1"},
					},
				},
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  cpuNodes("node0", "node1"),
			Queues: queues,
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer": {Status: pod_status.Running},
				"victim":  {NodeName: "node0", Status: pod_status.Releasing},
			},
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "unschedulable pending pod does not block the resize of a secured gang",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "resizer",
					QueueName:           "queue0",
					Priority:            resizerPriority,
					RequiredCPUsPerTask: 3,
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						{State: pod_status.Pending, NodeAffinityNames: []string{"missing"}},
					},
				},
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"victim": {NodeName: "node0", Status: pod_status.Releasing},
			},
			TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer-0": {NodeName: "node0", Status: pod_status.Running},
				"resizer-1": {Status: pod_status.Pending},
			},
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "gang not yet secured does not evict for a resize",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "resizer",
					QueueName:           "queue0",
					Priority:            resizerPriority,
					RequiredCPUsPerTask: 3,
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(2),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						{State: pod_status.Pending, NodeAffinityNames: []string{"missing"}},
					},
				},
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"victim": {NodeName: "node0", Status: pod_status.Running},
			},
			TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer-0": {NodeName: "node0", Status: pod_status.Running},
				"resizer-1": {Status: pod_status.Pending},
			},
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "pending pod is placed first and the resize needs no extra eviction",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "resizer",
					QueueName:           "queue0",
					Priority:            resizerPriority,
					RequiredCPUsPerTask: 3,
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						{State: pod_status.Pending},
					},
				},
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 3),
				runningCPUJob("victim2", "queue0", "node0", constants.PriorityTrainNumber-10, 3),
			},
			// 1 CPU short for the resize; the pending pod needs both victims, which also cover the resize.
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 8}},
			Queues: queues,
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"victim":  {NodeName: "node0", Status: pod_status.Releasing},
				"victim2": {NodeName: "node0", Status: pod_status.Releasing},
			},
			TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer-0": {NodeName: "node0", Status: pod_status.Running},
				"resizer-1": {NodeName: "node0", Status: pod_status.Pipelined},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions: 2, NumberOfPipelineActions: 1,
			}},
			DeferredResizePreemption: true,
		},
		{
			Name: "two deferred pods of a job on one node resize together",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "resizer",
					QueueName:           "queue0",
					Priority:            resizerPriority,
					RequiredCPUsPerTask: 2,
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(2),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						deferredResizeTask("node0", 1),
					},
				},
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "GPU-sharing resizing pod fits back on its own node",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "resizer",
					QueueName:           "queue0",
					Priority:            resizerPriority,
					RequiredCPUsPerTask: 3,
					RequiredGPUsPerTask: 0.5,
					Tasks: []*tasks_fake.TestTaskBasic{{
						State:        pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
						ResizeStatus: &tasks_fake.TestResizeStatus{ActualCPUs: 1, Deferred: true},
					}},
				},
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4, GPUs: 1}},
			Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 1}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer": {NodeName: "node0", Status: pod_status.Running, GPUsRequired: 0.5, GPUGroups: []string{"0"}},
				"victim":  {NodeName: "node0", Status: pod_status.Releasing},
			},
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "host-port resizing pod fits back on its own node",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "resizer",
					QueueName:           "queue0",
					Priority:            resizerPriority,
					RequiredCPUsPerTask: 3,
					Tasks: []*tasks_fake.TestTaskBasic{{
						State:        pod_status.Running,
						NodeName:     "node0",
						HostPorts:    []int32{8080},
						ResizeStatus: &tasks_fake.TestResizeStatus{ActualCPUs: 1, Deferred: true},
					}},
				},
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
	}
}

func TestPreemptForDeferredResize_RestoresResizingPodState(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topology := deferredResizePreemptTopologies()[0]
	ssn := test_utils.BuildSession(topology, controller)
	queue := ssn.ClusterInfo.Queues["queue0"]
	queueCPUBefore := ssn.QueueAllocatedResources(queue).Get(v1.ResourceCPU)

	preempt.New().Execute(ssn)

	node := ssn.ClusterInfo.Nodes["node0"]
	resizer := ssn.ClusterInfo.PodGroupInfos["resizer"].GetAllPodsMap()["resizer-0"]
	assert.Equal(t, pod_status.Running, resizer.Status)
	assert.False(t, resizer.IsVirtualStatus)
	assert.Equal(t, pod_status.Running, node.PodInfos[pod_info.PodKey(resizer.Pod)].Status)
	assert.Equal(t, 5000.0, node.UsedVector.Get(resource_info.CPUIndex))
	assert.Equal(t, 2000.0, node.ReleasingVector.Get(resource_info.CPUIndex))
	assert.Equal(t, -1000.0, node.IdleVector.Get(resource_info.CPUIndex))
	// The victim is released and the resizing pod keeps its desired-size charge.
	assert.Equal(t, queueCPUBefore-2000, ssn.QueueAllocatedResources(queue).Get(v1.ResourceCPU))
}

func resizerWith(mutate func(job *jobs_fake.TestJobBasic)) *jobs_fake.TestJobBasic {
	job := deferredResizeJob("resizer", "node0", 3, 1)
	mutate(job)
	return job
}

func onlyRunning(nodeName string, jobNames ...string) map[string]test_utils.TestExpectedResultBasic {
	statuses := map[string]pod_status.PodStatus{}
	for _, name := range jobNames {
		statuses[name] = pod_status.Running
	}
	return expectStatuses(nodeName, statuses)
}

func deferredResizePreemptEdgeTopologies() []test_utils.TestTopologyBasic {
	queues := []test_utils.TestQueueBasic{{Name: "queue0"}}
	victim := func() *jobs_fake.TestJobBasic {
		return runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2)
	}
	notReady := []v1.NodeCondition{{Type: v1.NodeReady, Status: v1.ConditionFalse}}

	return []test_utils.TestTopologyBasic{
		{
			Name: "memory-only deferred resize evicts a victim",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "resizer", QueueName: "queue0", Priority: resizerPriority,
					RequiredCPUsPerTask: 1, RequiredMemoryPerTask: 3e9,
					Tasks: []*tasks_fake.TestTaskBasic{{
						State: pod_status.Running, NodeName: "node0",
						ResizeStatus: &tasks_fake.TestResizeStatus{ActualMemory: 1e9, Deferred: true},
					}},
				},
				{
					Name: "victim", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 1, RequiredMemoryPerTask: 2e9,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node0"}},
				},
			},
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 8, CPUMemory: 4e9}},
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "stale Deferred condition after a downsize",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 1, 3),
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:                    map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4}},
			Queues:                   queues,
			JobExpectedResults:       onlyRunning("node0", "resizer", "victim"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "Infeasible resize does not evict",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) {
					job.Tasks[0].ResizeStatus = &tasks_fake.TestResizeStatus{ActualCPUs: 1, Infeasible: true}
				}),
				victim(),
			},
			Nodes:                    cpuNodes("node0"),
			Queues:                   queues,
			JobExpectedResults:       onlyRunning("node0", "resizer", "victim"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "terminating resizing pod does not evict",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) { job.Tasks[0].State = pod_status.Releasing }),
				victim(),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Releasing,
				"victim":  pod_status.Running,
			}),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "resizing job within its preemption delay does not evict",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) {
					job.PreemptionDelay = &metav1.Duration{Duration: time.Hour}
				}),
				victim(),
			},
			Nodes:                    cpuNodes("node0"),
			Queues:                   queues,
			JobExpectedResults:       onlyRunning("node0", "resizer", "victim"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "resizing job past its preemption delay evicts",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) {
					job.PreemptionDelay = &metav1.Duration{Duration: time.Second}
					job.JobAgeInMinutes = 10
				}),
				victim(),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "whole-GPU resizing pod fits back on its own node",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) { job.RequiredGPUsPerTask = 1 }),
				victim(),
			},
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4, GPUs: 1}},
			Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 1}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer": {NodeName: "node0", Status: pod_status.Running, GPUsRequired: 1},
				"victim":  {NodeName: "node0", Status: pod_status.Releasing},
			},
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "node no longer matching the resizing pod's node affinity does not evict",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) { job.Tasks[0].NodeAffinityNames = []string{"other-pool"} }),
				victim(),
			},
			Nodes:                    cpuNodes("node0"),
			Queues:                   queues,
			JobExpectedResults:       onlyRunning("node0", "resizer", "victim"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name:                     "not ready node does not evict",
			Jobs:                     []*jobs_fake.TestJobBasic{deferredResizeJob("resizer", "node0", 3, 1), victim()},
			Nodes:                    map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4, Conditions: &notReady}},
			Queues:                   queues,
			JobExpectedResults:       onlyRunning("node0", "resizer", "victim"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name:                     "cordoned node does not evict",
			Jobs:                     []*jobs_fake.TestJobBasic{deferredResizeJob("resizer", "node0", 3, 1), victim()},
			Nodes:                    map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4, Unschedulable: true}},
			Queues:                   queues,
			JobExpectedResults:       onlyRunning("node0", "resizer", "victim"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "two resizing jobs on a node share the evictions",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				deferredResizeJob("resizer2", "node0", 3, 1),
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
				runningCPUJob("victim2", "queue0", "node0", constants.PriorityTrainNumber, 2),
			},
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 6}},
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer":  pod_status.Running,
				"resizer2": pod_status.Running,
				"victim":   pod_status.Releasing,
				"victim2":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(2),
			DeferredResizePreemption: true,
		},
		{
			Name: "higher-priority resize may evict a lower-priority resizing pod",
			Jobs: []*jobs_fake.TestJobBasic{
				deferredResizeJob("resizer", "node0", 3, 1),
				{
					Name: "low-resizer", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 2, Tasks: []*tasks_fake.TestTaskBasic{deferredResizeTask("node0", 1)},
				},
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer":     pod_status.Running,
				"low-resizer": pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "two resizing pods and a gang victim that moves to its other node",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "resizer", QueueName: "queue0", Priority: resizerPriority, RequiredCPUsPerTask: 2,
					RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						deferredResizeTask("node0", 1),
					},
				},
				{
					Name: "victim", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 2, RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
					Tasks: []*tasks_fake.TestTaskBasic{
						{State: pod_status.Running, NodeName: "node0"},
						{State: pod_status.Running, NodeName: "node1"},
					},
				},
			},
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4}, "node1": {CPUMillis: 4}},
			Queues: queues,
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"resizer": {NodeName: "node0", Status: pod_status.Running},
			},
			TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"victim-0": {NodeName: "node1", Status: pod_status.Pipelined},
				"victim-1": {NodeName: "node1", Status: pod_status.Running},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions: 1, NumberOfPipelineActions: 1,
			}},
			DeferredResizePreemption: true,
		},
		{
			Name: "resizing pod with anti-affinity to its own label fits back on its node",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) {
					job.Tasks[0].PodAffinityLabels = map[string]string{"app": "one-per-node"}
					job.Tasks[0].PodAntiAffinityTopologyKey = v1.LabelHostname
				}),
				victim(),
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {CPUMillis: 4, Labels: map[string]string{v1.LabelHostname: "node0"}},
			},
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "elastic job with two resizing pods above its gang minimum resizes both",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "resizer", QueueName: "queue0", Priority: resizerPriority, RequiredCPUsPerTask: 2,
					RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						deferredResizeTask("node0", 1),
					},
				},
				victim(),
			},
			Nodes:  cpuNodes("node0"),
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "elastic job with three resizing pods needing two victims",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "resizer", QueueName: "queue0", Priority: resizerPriority, RequiredCPUsPerTask: 2,
					RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						deferredResizeTask("node0", 1),
						deferredResizeTask("node0", 1),
					},
				},
				runningCPUJob("victim", "queue0", "node0", constants.PriorityTrainNumber, 2),
				runningCPUJob("victim2", "queue0", "node0", constants.PriorityTrainNumber, 1),
			},
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 6}},
			Queues: queues,
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
				"victim2": pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(2),
			DeferredResizePreemption: true,
		},
		{
			Name: "a resize never evicts tasks of its own job",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "resizer", QueueName: "queue0", Priority: resizerPriority, RequiredCPUsPerTask: 3,
					RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
					Tasks: []*tasks_fake.TestTaskBasic{
						deferredResizeTask("node0", 1),
						{State: pod_status.Running, NodeName: "node0"},
					},
				},
			},
			Nodes:                    map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 5}},
			Queues:                   queues,
			JobExpectedResults:       onlyRunning("node0", "resizer"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name:  "desired size above the queue limit does not evict",
			Jobs:  []*jobs_fake.TestJobBasic{deferredResizeJob("resizer", "node0", 3, 1), victim()},
			Nodes: cpuNodes("node0"),
			Queues: []test_utils.TestQueueBasic{{
				Name: "queue0", MaxAllowedCPUs: test_utils.CreateFloat64Pointer(2000),
			}},
			JobExpectedResults:       onlyRunning("node0", "resizer", "victim"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
		{
			Name: "non-preemptible resizer within deserved quota evicts",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) { job.Priority = constants.PriorityBuildNumber }),
				victim(),
			},
			Nodes: cpuNodes("node0"),
			Queues: []test_utils.TestQueueBasic{{
				Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(3000),
			}},
			JobExpectedResults: expectStatuses("node0", map[string]pod_status.PodStatus{
				"resizer": pod_status.Running,
				"victim":  pod_status.Releasing,
			}),
			Mocks:                    evictionMocks(1),
			DeferredResizePreemption: true,
		},
		{
			Name: "non-preemptible resizer beyond deserved quota does not evict",
			Jobs: []*jobs_fake.TestJobBasic{
				resizerWith(func(job *jobs_fake.TestJobBasic) {
					job.Priority = constants.PriorityBuildNumber
					job.RequiredCPUsPerTask = 4
				}),
				victim(),
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 5}},
			Queues: []test_utils.TestQueueBasic{{
				Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(3000),
			}},
			JobExpectedResults:       onlyRunning("node0", "resizer", "victim"),
			Mocks:                    noEvictionMocks(),
			DeferredResizePreemption: true,
		},
	}
}

func TestPreemptForDeferredResize_ElasticVictimLosesOnlyNeededPods(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topology := test_utils.TestTopologyBasic{
		Name: "elastic victim",
		Jobs: []*jobs_fake.TestJobBasic{
			deferredResizeJob("resizer", "node0", 3, 1),
			{
				Name: "victim", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
				RequiredCPUsPerTask: 1, RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Running, NodeName: "node0"},
					{State: pod_status.Running, NodeName: "node0"},
				},
			},
		},
		Nodes:                    cpuNodes("node0"),
		Queues:                   []test_utils.TestQueueBasic{{Name: "queue0"}},
		Mocks:                    evictionMocks(1),
		DeferredResizePreemption: true,
	}
	ssn := test_utils.BuildSession(topology, controller)
	preempt.New().Execute(ssn)

	statuses := map[pod_status.PodStatus]int{}
	for _, task := range ssn.ClusterInfo.PodGroupInfos["victim"].GetAllPodsMap() {
		statuses[task.Status]++
	}
	assert.Equal(t, map[pod_status.PodStatus]int{pod_status.Running: 1, pod_status.Releasing: 1}, statuses)
	assert.Equal(t, pod_status.Running, ssn.ClusterInfo.PodGroupInfos["resizer"].GetAllPodsMap()["resizer-0"].Status)
}

func TestPreemptForDeferredResize_NoFitErrorsOnRunningJob(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for _, topology := range allDeferredResizePreemptTopologies() {
		ssn := test_utils.BuildSession(topology, controller)
		preempt.New().Execute(ssn)
		for _, job := range ssn.ClusterInfo.PodGroupInfos {
			// Evicted victims get fit errors from re-placement attempts, as in regular preemption.
			if job.GetNumPendingTasks() > 0 || len(job.PodStatusIndex[pod_status.Releasing]) > 0 {
				continue
			}
			assert.Empty(t, job.JobFitErrors, "%s: job %s", topology.Name, job.Name)
			assert.Empty(t, job.TasksFitErrors, "%s: job %s", topology.Name, job.Name)
		}
	}
}

func TestPreemptForDeferredResize_ExhaustedBudgetStopsSafely(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topology := deferredResizePreemptTopologies()[0]
	topology.Mocks = noEvictionMocks()
	ssn := test_utils.BuildSession(topology, controller)
	ssn.Config.ScenarioSearchBudgets = &kaiv1.ScenarioSearchBudgets{
		MaxActionSearchDuration: map[string]metav1.Duration{apiconstants.ActionPreempt: {Duration: time.Nanosecond}},
	}
	before := ssn.ClusterInfo.Nodes["node0"].IdleVector.Clone()

	require.NotPanics(t, func() { preempt.New().Execute(ssn) })
	assert.Equal(t, pod_status.Running, ssn.ClusterInfo.PodGroupInfos["victim"].GetAllPodsMap()["victim-0"].Status)
	assert.Equal(t, pod_status.Running, ssn.ClusterInfo.PodGroupInfos["resizer"].GetAllPodsMap()["resizer-0"].Status)
	assert.Equal(t, before, ssn.ClusterInfo.Nodes["node0"].IdleVector)
}

func elasticJob(name string, priority int32, minAvailable int32, nodeNames ...string) *jobs_fake.TestJobBasic {
	job := &jobs_fake.TestJobBasic{
		Name: name, QueueName: "queue0", Priority: priority, RequiredCPUsPerTask: 1,
		RootSubGroupSet: jobs_fake.DefaultSubGroup(minAvailable),
	}
	for _, nodeName := range nodeNames {
		job.Tasks = append(job.Tasks, &tasks_fake.TestTaskBasic{State: pod_status.Running, NodeName: nodeName})
	}
	return job
}

func runDeferredResizePreempt(t *testing.T, jobs []*jobs_fake.TestJobBasic, nodeNames ...string) *framework.Session {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	topology := test_utils.TestTopologyBasic{
		Name:   t.Name(),
		Jobs:   append([]*jobs_fake.TestJobBasic{deferredResizeJob("resizer", "node0", 3, 1)}, jobs...),
		Nodes:  cpuNodes(nodeNames...),
		Queues: []test_utils.TestQueueBasic{{Name: "queue0"}},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
			NumberOfCacheEvictions: 10, NumberOfPipelineActions: 10,
		}},
		DeferredResizePreemption: true,
	}
	ssn := test_utils.BuildSession(topology, controller)
	preempt.New().Execute(ssn)
	return ssn
}

func requireResizeMadeRoom(t *testing.T, ssn *framework.Session) {
	resizer := ssn.ClusterInfo.PodGroupInfos["resizer"].GetAllPodsMap()["resizer-0"]
	require.Equal(t, pod_status.Running, resizer.Status)
	require.Equal(t, "node0", resizer.NodeName)
	require.GreaterOrEqual(t, ssn.ClusterInfo.Nodes["node0"].NonAllocatedResource(v1.ResourceCPU), 0.0)
}

func activeTasks(ssn *framework.Session, jobName string) int {
	count := 0
	for _, task := range ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(jobName)].GetAllPodsMap() {
		if task.Status == pod_status.Running || task.Status == pod_status.Pipelined {
			count++
		}
	}
	return count
}

// The solver restores a victim job to its minimum on the victims' own nodes, not only on the
// resizing pod's node.
func TestDeferredResizeKeepsMultiNodeVictimAtItsMinimum(t *testing.T) {
	for localIndex := 0; localIndex < 3; localIndex++ {
		t.Run(fmt.Sprintf("local pod at index %d", localIndex), func(t *testing.T) {
			nodeNames := []string{"node1", "node2"}
			nodeNames = append(nodeNames[:localIndex], append([]string{"node0"}, nodeNames[localIndex:]...)...)
			victim := elasticJob("victim", constants.PriorityTrainNumber, 2, nodeNames...)
			victim.RequiredCPUsPerTask = 2

			ssn := runDeferredResizePreempt(t, []*jobs_fake.TestJobBasic{victim}, "node0", "node1", "node2")

			requireResizeMadeRoom(t, ssn)
			assert.GreaterOrEqual(t, activeTasks(ssn, "victim"), 2)
		})
	}
}

// Evicting the elastic job alone is not enough, so "other" goes too, which leaves room for one
// elastic pod. The solver keeps that pod running, possibly on another node, and so must the commit.
func TestDeferredResizeCommitsVictimMoves(t *testing.T) {
	for localIndex := 0; localIndex < 2; localIndex++ {
		t.Run(fmt.Sprintf("local pod at index %d", localIndex), func(t *testing.T) {
			nodeNames := []string{"node1", "node1"}
			nodeNames[localIndex] = "node0"
			elastic := elasticJob("elastic", constants.PriorityTrainNumber, 1, nodeNames...)
			other := runningCPUJob("other", "queue0", "node0", constants.PriorityTrainNumber+1, 2)

			ssn := runDeferredResizePreempt(t, []*jobs_fake.TestJobBasic{elastic, other}, "node0", "node1")

			requireResizeMadeRoom(t, ssn)
			assert.GreaterOrEqual(t, activeTasks(ssn, "elastic"), 1)
		})
	}
}
