// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestReclaimFractionalIntegrationTest(t *testing.T) {
	integration_tests_utils.RunTests(t, getReclaimFractionalTestsMetadata())
}

func TestReclaimCanUseFullyEvictedFractionalGpuForDifferentComputeMode(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	const gpuGroup = "time-slicing-group"
	topology := test_utils.TestTopologyBasic{
		Name: "reclaim all time-slicing fractions before using GPU for sm-sharing",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "running_job0",
				RequiredGPUsPerTask: 0.5,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						NodeName:  "node0",
						GPUGroups: []string{gpuGroup},
						State:     pod_status.Running,
					},
					{
						NodeName:  "node0",
						GPUGroups: []string{gpuGroup},
						State:     pod_status.Running,
					},
				},
			},
			{
				Name:                "pending_job0",
				RequiredGPUsPerTask: 0.5,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue1",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State: pod_status.Pending,
						Annotations: map[string]string{
							commonconstants.GpuComputeSharingMode: string(schedulingv1alpha2.GPUComputeSharingModeSMSharing),
						},
					},
				},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {
				GPUs: 1,
			},
		},
		Queues: []test_utils.TestQueueBasic{
			{
				Name:         "queue0",
				DeservedGPUs: 0,
			},
			{
				Name:         "queue1",
				DeservedGPUs: 1,
			},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions:  2,
				NumberOfPipelineActions: 1,
			},
		},
	}

	ssn := test_utils.BuildSession(topology, controller)
	addReservationPodToNodeForReclaimTest(ssn.ClusterInfo.Nodes["node0"], gpuGroup, schedulingv1alpha2.GPUComputeSharingModeTimeSlicing)

	reclaim.New().Execute(ssn)

	runningTasks := ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()
	for taskID, task := range runningTasks {
		if task.Status != pod_status.Releasing {
			t.Fatalf("expected time-slicing task %s to be releasing, got %s", taskID, task.Status)
		}
	}

	pendingTask := ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()[common_info.PodID("pending_job0-0")]
	if pendingTask.Status != pod_status.Pipelined {
		t.Fatalf("expected sm-sharing task to be pipelined, got status %s", pendingTask.Status)
	}
	if pendingTask.NodeName != "node0" {
		t.Fatalf("expected sm-sharing task on node0, got %s", pendingTask.NodeName)
	}
	gpuGroups := pendingTask.GPUGroupIDs()
	if len(gpuGroups) != 1 || gpuGroups[0] == gpuGroup {
		t.Fatalf("expected sm-sharing task to use a new gpu group, got %v", gpuGroups)
	}
	if pendingTask.FractionalGpuGroups[0].ComputeSharingMode != schedulingv1alpha2.GPUComputeSharingModeSMSharing {
		t.Fatalf("expected sm-sharing mode, got %s", pendingTask.FractionalGpuGroups[0].ComputeSharingMode)
	}
}

func addReservationPodToNodeForReclaimTest(
	node *node_info.NodeInfo, gpuGroup string, mode schedulingv1alpha2.GPUComputeSharingMode,
) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("reservation-" + gpuGroup),
			Name:      commonconstants.GPUReservationPodPrefix + "-node0-test",
			Namespace: commonconstants.DefaultResourceReservationName,
			Labels: map[string]string{
				commonconstants.GPUGroup: gpuGroup,
			},
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: "reservation",
				commonconstants.GpuComputeSharingMode:    string(mode),
			},
		},
		Spec: v1.PodSpec{
			NodeName: node.Name,
			Containers: []v1.Container{
				{Name: "reservation"},
			},
		},
		Status: v1.PodStatus{Phase: v1.PodRunning},
	}
	task := pod_info.NewTaskInfo(pod, resource_info.NewResourceVectorMap())
	task.Status = pod_status.Running
	node.PodInfos[task.UID] = task
}

func getReclaimFractionalTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim fractional train by whole GPU job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim fractional train by fractional train GPU job - reclaim only part of fractional jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
							},
						},
					}, {
						Name:                "running_job2",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.4,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
					"running_job2": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 0.4,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim fractional train by fractional GPU job, reclaim all fractional jobs of 1 GPU",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"1"},
								State:     pod_status.Running,
							},
						},
					}, {
						Name:                "running_job2",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"1"},
								State:     pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.8,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Pending,
					},
					"running_job2": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 0.8,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim fractional train by fractional GPU job will go over quota - don't reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
							},
						},
					}, {
						Name:                "running_job2",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.8,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
					"running_job2": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 0.8,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  0,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "verify whole gpu job is being pipelined to a gpu causing a fractional not be pipelined to this node (due to a bug we had)",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "releasing_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"0"},
								State:     pod_status.Releasing,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 3,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"releasing_job1": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						GPUsRequired: 0.5,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
	}
}
