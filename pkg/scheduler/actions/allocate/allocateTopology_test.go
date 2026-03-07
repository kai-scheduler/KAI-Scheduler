// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"fmt"
	"testing"

	kaiv1alpha1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1alpha1"
	. "go.uber.org/mock/gomock"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/topology_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleTopologyAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getTopologyTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getTopologyTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology level - schedule all tasks on the same rack",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "cluster-topology",
								RequiredLevel: "k8s.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
						},
					},
					"node2": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
						},
					},
					"node3": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     4,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node3",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology level - could not schedule on first domain due to affinity, goes to next",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "cluster-topology",
								RequiredLevel: "k8s.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
								NodeAffinityNames: []string{
									"affinity1",
								},
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
						},
					},
					"node2": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":              "rack2",
							tasks_fake.NodeAffinityKey: "affinity1",
						},
					},
					"node3": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":              "rack2",
							tasks_fake.NodeAffinityKey: "affinity1",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     4,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node3",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology with multiple levels - schedule all tasks on the same spine",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "cluster-topology",
								RequiredLevel: "k8s.io/spine",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     4,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology with multiple levels - schedule all tasks on the same rack",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "cluster-topology",
								RequiredLevel: "k8s.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine2",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack2",
							"k8s.io/spine": "spine1",
						},
					},
					"node3": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack2",
							"k8s.io/spine": "spine2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       8,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     8,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 8,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node3",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						NodeName:             "node3",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology with multiple levels - schedule all tasks on the same partially occupied rack",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "cluster-topology",
								RequiredLevel: "k8s.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       8,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     8,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 8,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology with multiple levels - job remains pending as no available rack can be found",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node2",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "cluster-topology",
								RequiredLevel: "k8s.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine2",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack2",
							"k8s.io/spine": "spine1",
						},
					},
					"node3": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack2",
							"k8s.io/spine": "spine2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       8,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     8,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 8,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired:         4,
						Status:               pod_status.Pending,
						ExpectedErrorMessage: "\nUnable to schedule podgroup.\n<rack1>: node-group rack1 can allocate only 3 of 4 required pods.\n<rack2>: node-group rack2 can allocate only 3 of 4 required pods.\ntopology cluster-topology, requirement k8s.io/rack couldn't be satisfied for job </pending_job0>.",
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running_job1-0": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						GPUsRequired:         1,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						GPUsRequired:         1,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						GPUsRequired:         1,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						GPUsRequired:         1,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 0,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Preferred topology - schedule job on the same rack",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "cluster-topology",
								PreferredLevel: "k8s.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     4,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Preferred topology - schedule job even if preferred constraint can't be met",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "cluster-topology",
								PreferredLevel: "k8s.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     4,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running_job1-0": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Combined preferred and required topology constraints - schedule job although preferred constraint cannot be satisfied",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
							{
								State:    pod_status.Running,
								NodeName: "node2",
							},
							{
								State:    pod_status.Running,
								NodeName: "node3",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "cluster-topology",
								RequiredLevel:  "k8s.io/rack",
								PreferredLevel: "k8s.io/spine",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine2",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack2",
							"k8s.io/spine": "spine1",
						},
					},
					"node3": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack2",
							"k8s.io/spine": "spine2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       8,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     8,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 8,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running_job0-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running_job0-2": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running_job0-3": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running_job0-4": {
						NodeName:             "node3",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node3",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology at multiple subgroup hierarchy levels - subgroup on the same rack and workload in the same zone",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/zone",
								},
							)
							root.AddPodSet(subgroup_info.NewPodSet("sub-a", 1,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							))
							root.AddPodSet(subgroup_info.NewPodSet("sub-b", 1,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-a",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-b",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
							"k8s.io/zone": "zone1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
							"k8s.io/zone": "zone1",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack3",
							"k8s.io/zone": "zone2",
						},
					},
					"node3": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack4",
							"k8s.io/zone": "zone2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       6,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     6,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 6,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node2",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node3",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology at multiple subgroup hierarchy levels - rack, spine and zone",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							replica1 := subgroup_info.NewSubGroupSet("replica1",
								&topology_info.TopologyConstraintInfo{
									RequiredLevel: "k8s.io/spine",
									Topology:      "cluster-topology",
								},
							)
							replica1.AddPodSet(subgroup_info.NewPodSet(
								"rep1-sub-a",
								1,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							))
							replica1.AddPodSet(subgroup_info.NewPodSet(
								"rep1-sub-b",
								1,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							))

							replica2 := subgroup_info.NewSubGroupSet("replica2",
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/spine",
								},
							)
							replica2.AddPodSet(subgroup_info.NewPodSet(
								"rep2-sub-a",
								1,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							))
							replica2.AddPodSet(subgroup_info.NewPodSet(
								"rep2-sub-b",
								1,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							))

							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/zone",
								},
							)
							root.AddSubGroup(replica1)
							root.AddSubGroup(replica2)

							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "rep1-sub-a",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "rep1-sub-b",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "rep2-sub-a",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "rep2-sub-b",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
							"k8s.io/zone":  "zone1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack2",
							"k8s.io/spine": "spine1",
							"k8s.io/zone":  "zone1",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack3",
							"k8s.io/spine": "spine2",
							"k8s.io/zone":  "zone1",
						},
					},
					"node3": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack4",
							"k8s.io/spine": "spine2",
							"k8s.io/zone":  "zone1",
						},
					},
					"node4": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack5",
							"k8s.io/spine": "spine3",
							"k8s.io/zone":  "zone2",
						},
					},
					"node5": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack6",
							"k8s.io/spine": "spine3",
							"k8s.io/zone":  "zone2",
						},
					},
					"node6": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack7",
							"k8s.io/spine": "spine4",
							"k8s.io/zone":  "zone2",
						},
					},
					"node7": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack8",
							"k8s.io/spine": "spine4",
							"k8s.io/zone":  "zone2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       10,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     10,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 10,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node4",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node5",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node6",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						NodeName:             "node7",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required topology constraint - prefer packed domain",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-a", 1,
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-a",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 10,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 10,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       10,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     10,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 10,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Preferred topology at multiple subgroup hierarchy levels - rack and zone",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							replica1 := subgroup_info.NewSubGroupSet("replica1",
								&topology_info.TopologyConstraintInfo{
									PreferredLevel: "k8s.io/rack",
									Topology:       "cluster-topology",
								},
							)
							replica1.AddPodSet(subgroup_info.NewPodSet(
								"encoder1",
								1,
								nil,
							))
							replica1.AddPodSet(subgroup_info.NewPodSet(
								"decoder1",
								1,
								nil,
							))

							replica2 := subgroup_info.NewSubGroupSet("replica2",
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/rack",
								},
							)
							replica2.AddPodSet(subgroup_info.NewPodSet(
								"encoder2",
								1,
								nil,
							))
							replica2.AddPodSet(subgroup_info.NewPodSet(
								"decoder2",
								1,
								nil,
							))

							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/zone",
								},
							)
							root.AddSubGroup(replica1)
							root.AddSubGroup(replica2)

							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "encoder1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "decoder1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "encoder2",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "decoder2",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
							"k8s.io/zone": "zone1",
						},
					},
					"node1": {
						GPUs: 4,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
							"k8s.io/zone": "zone1",
						},
					},
					"node2": {
						GPUs: 4,
						Labels: map[string]string{
							"k8s.io/rack": "rack3",
							"k8s.io/zone": "zone2",
						},
					},
					"node3": {
						GPUs: 4,
						Labels: map[string]string{
							"k8s.io/rack": "rack4",
							"k8s.io/zone": "zone2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       10,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     10,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 10,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node2",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node2",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node3",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						NodeName:             "node3",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required and preferred topology constraints combined at different levels - rack and zone",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							replica1 := subgroup_info.NewSubGroupSet("replica1",
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							)
							replica1.AddPodSet(subgroup_info.NewPodSet(
								"encoder1",
								1,
								nil,
							))
							replica1.AddPodSet(subgroup_info.NewPodSet(
								"decoder1",
								1,
								nil,
							))

							replica2 := subgroup_info.NewSubGroupSet("replica2",
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/rack",
								},
							)
							replica2.AddPodSet(subgroup_info.NewPodSet(
								"encoder2",
								1,
								nil,
							))
							replica2.AddPodSet(subgroup_info.NewPodSet(
								"decoder2",
								1,
								nil,
							))

							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/zone",
								},
							)
							root.AddSubGroup(replica1)
							root.AddSubGroup(replica2)

							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "encoder1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "decoder1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "encoder2",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "decoder2",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
							"k8s.io/zone": "zone1",
						},
					},
					"node1": {
						GPUs: 4,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
							"k8s.io/zone": "zone1",
						},
					},
					"node2": {
						GPUs: 4,
						Labels: map[string]string{
							"k8s.io/rack": "rack3",
							"k8s.io/zone": "zone2",
						},
					},
					"node3": {
						GPUs: 4,
						Labels: map[string]string{
							"k8s.io/rack": "rack4",
							"k8s.io/zone": "zone2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       10,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     10,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 10,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node2",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node2",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node3",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						NodeName:             "node3",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Required and preferred topology constraints with single unconstrained level",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							replica1 := subgroup_info.NewSubGroupSet("replica1",
								&topology_info.TopologyConstraintInfo{
									RequiredLevel: "k8s.io/spine",
									Topology:      "cluster-topology",
								},
							)
							replica1.AddPodSet(subgroup_info.NewPodSet(
								"rep1-sub-a",
								1,
								nil,
							))
							replica1.AddPodSet(subgroup_info.NewPodSet(
								"rep1-sub-b",
								1,
								nil,
							))

							replica2 := subgroup_info.NewSubGroupSet("replica2",
								&topology_info.TopologyConstraintInfo{
									Topology:      "cluster-topology",
									RequiredLevel: "k8s.io/spine",
								},
							)
							replica2.AddPodSet(subgroup_info.NewPodSet(
								"rep2-sub-a",
								1,
								nil,
							))
							replica2.AddPodSet(subgroup_info.NewPodSet(
								"rep2-sub-b",
								1,
								nil,
							))

							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/zone",
								},
							)
							root.AddSubGroup(replica1)
							root.AddSubGroup(replica2)

							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "rep1-sub-a",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "rep1-sub-b",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "rep2-sub-a",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "rep2-sub-b",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
							"k8s.io/zone":  "zone1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack2",
							"k8s.io/spine": "spine1",
							"k8s.io/zone":  "zone1",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack3",
							"k8s.io/spine": "spine2",
							"k8s.io/zone":  "zone1",
						},
					},
					"node3": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack4",
							"k8s.io/spine": "spine2",
							"k8s.io/zone":  "zone1",
						},
					},
					"node4": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack5",
							"k8s.io/spine": "spine3",
							"k8s.io/zone":  "zone2",
						},
					},
					"node5": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack6",
							"k8s.io/spine": "spine3",
							"k8s.io/zone":  "zone2",
						},
					},
					"node6": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack7",
							"k8s.io/spine": "spine4",
							"k8s.io/zone":  "zone2",
						},
					},
					"node7": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack":  "rack8",
							"k8s.io/spine": "spine4",
							"k8s.io/zone":  "zone2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       10,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     10,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 10,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending_job0-0": {
						NodeName:             "node4",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node5",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node6",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						NodeName:             "node7",
						GPUsRequired:         2,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Combined preferred and required topology constraints - preferred level respected when possible",
				// Tests that when both required (rack) and preferred (spine) topology levels are specified,
				// the scheduler respects both: all tasks stay within the required rack level, and tasks are
				// concentrated on minimum number of spines (2 spines used for 3 tasks, not spread across 3-4 spines).
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "cluster-topology",
								RequiredLevel:  "k8s.io/rack",
								PreferredLevel: "k8s.io/spine",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
						},
					},
					"node1": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine1",
						},
					},
					"node2": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine2",
						},
					},
					"node3": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine2",
						},
					},
					"node4": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine3",
						},
					},
					"node5": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine3",
						},
					},
					"node6": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine4",
						},
					},
					"node7": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/rack":  "rack1",
							"k8s.io/spine": "spine4",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       8,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     8,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 8,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:             "node6",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node7",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node4",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 3,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Elastic subgroups with required topology constraints",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("subgroup-a", 1,
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/rack",
								}))
							root.AddPodSet(subgroup_info.NewPodSet("subgroup-b", 1,
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/rack",
								}))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "subgroup-a",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "subgroup-a",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "subgroup-b",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "subgroup-b",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"k8s.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     4,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Hierarchical Preferred Topology - PodGroup prefers Spine, SubGroups has no preference - should allocate on closest Spine nodes",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							subgroupA := subgroup_info.NewSubGroupSet("subgroup-a", &topology_info.TopologyConstraintInfo{})
							subgroupA.AddPodSet(subgroup_info.NewPodSet("pod-a-1", 1, nil))
							subgroupA.AddPodSet(subgroup_info.NewPodSet("pod-a-2", 1, nil))
							subgroupA.AddPodSet(subgroup_info.NewPodSet("pod-a-3", 1, nil))

							subgroupB := subgroup_info.NewSubGroupSet("subgroup-b", &topology_info.TopologyConstraintInfo{})
							subgroupB.AddPodSet(subgroup_info.NewPodSet("pod-b-1", 1, nil))
							subgroupB.AddPodSet(subgroup_info.NewPodSet("pod-b-2", 1, nil))
							subgroupB.AddPodSet(subgroup_info.NewPodSet("pod-b-3", 1, nil))

							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/spine",
								},
							)
							root.AddSubGroup(subgroupA)
							root.AddSubGroup(subgroupB)

							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-a-1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-a-2",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-a-3",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-b-1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-b-2",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-b-3",
							},
						},
					},
				},
				// zone1
				// ├── spine1
				// │   ├── rack1
				// │   │   ├── node0 (1 GPU)
				// │   │   └── node1 (1 GPU)
				// │   └── rack2
				// │       ├── node2 (1 GPU)
				// │       └── node3 (1 GPU)
				// └── spine2
				// 	├── rack3
				// 	│   ├── node4 (1 GPU)
				// 	│   └── node5 (1 GPU)
				// 	└── rack4
				// 		├── node6 (1 GPU)
				// 		└── node7 (1 GPU)
				Nodes: buildEvenlyDistributedTopologyNodes(1, 2, 2, 2, 1),
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       16,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     16,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 16,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:             "node4",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node5",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node6",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						NodeName:             "node7",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-4": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-5": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 6,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Hierarchical Conflicting Preferred Topology - PodGroup prefers Spine, SubGroups prefer Rack - should allocate on closest Rack nodes",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							subgroupA := subgroup_info.NewSubGroupSet("subgroup-a",
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/rack",
								},
							)
							subgroupA.AddPodSet(subgroup_info.NewPodSet("pod-a-1", 1, nil))
							subgroupA.AddPodSet(subgroup_info.NewPodSet("pod-a-2", 1, nil))
							subgroupA.AddPodSet(subgroup_info.NewPodSet("pod-a-3", 1, nil))

							subgroupB := subgroup_info.NewSubGroupSet("subgroup-b",
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/rack",
								},
							)
							subgroupB.AddPodSet(subgroup_info.NewPodSet("pod-b-1", 1, nil))
							subgroupB.AddPodSet(subgroup_info.NewPodSet("pod-b-2", 1, nil))
							subgroupB.AddPodSet(subgroup_info.NewPodSet("pod-b-3", 1, nil))

							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
								&topology_info.TopologyConstraintInfo{
									Topology:       "cluster-topology",
									PreferredLevel: "k8s.io/spine",
								},
							)
							root.AddSubGroup(subgroupA)
							root.AddSubGroup(subgroupB)

							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-a-1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-a-2",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-a-3",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-b-1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-b-2",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "pod-b-3",
							},
						},
					},
				},
				// zone1
				// ├── spine1
				// │   ├── rack1
				// │   │   ├── node0 (1 GPU)
				// │   │   └── node1 (1 GPU)
				// │   └── rack2
				// │       ├── node2 (1 GPU)
				// │       └── node3 (1 GPU)
				// └── spine2
				// 	├── rack3
				// 	│   ├── node4 (1 GPU)
				// 	│   └── node5 (1 GPU)
				// 	└── rack4
				// 		├── node6 (1 GPU)
				// 		└── node7 (1 GPU)
				Nodes: buildEvenlyDistributedTopologyNodes(1, 2, 2, 2, 1),
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       16,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     16,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 16,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-1": {
						NodeName:             "node3",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-2": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-3": {
						NodeName:             "node6",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-4": {
						NodeName:             "node7",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
					"pending_job0-5": {
						NodeName:             "node4",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 6,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Bin Packing - allocate on domain with least free resources (most occupied)",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0", // spine1, rack1
							},
							{
								State:    pod_status.Running,
								NodeName: "node1", // spine1, rack1
							},
							{
								State:    pod_status.Running,
								NodeName: "node2", // spine1, rack2
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "cluster-topology",
								PreferredLevel: "k8s.io/spine",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				// zone1
				// ├── spine1 (3/4 GPUs occupied - 1 free)
				// │   ├── rack1
				// │   │   ├── node0 (1 GPU) - OCCUPIED
				// │   │   └── node1 (1 GPU) - OCCUPIED
				// │   └── rack2
				// │       ├── node2 (1 GPU) - OCCUPIED
				// │       └── node3 (1 GPU) - FREE - **Allocation should happen here
				// └── spine2 (0/4 GPUs occupied - 4 free)
				// 	├── rack3
				// 	│   ├── node4 (1 GPU) - FREE
				// 	│   └── node5 (1 GPU) - FREE
				// 	└── rack4
				// 		├── node6 (1 GPU) - FREE
				// 		└── node7 (1 GPU) - FREE
				Nodes: buildEvenlyDistributedTopologyNodes(1, 2, 2, 2, 1),
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       16,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     16,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 16,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running_job0-1": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running_job0-2": {
						NodeName:             "node2",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					// Bin packing: should allocate on spine1 (most occupied, least free resources)
					// rather than spine2 (completely free)
					"pending_job0-0": {
						NodeName:             "node3", // spine1, rack2 - the only free node on spine1
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Bin Packing - Pack Top-Most domain first",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "k8s.io/zone",
								},
								{
									NodeLabel: "k8s.io/spine",
								},
								{
									NodeLabel: "k8s.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "cluster-topology",
								PreferredLevel: "k8s.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				// zone1 (5 GPUs total)
				// ├── spine1 (3 GPUs total)
				// │   ├── rack1 (1 GPU)
				// │   │   └── node0 (1 GPU)
				// │   ├── rack2 (1 GPU)
				// │   │   └── node1 (1 GPU)
				// │   └── rack3 (1 GPU)
				// │       └── node2 (1 GPU)
				// └── spine2 (2 GPUs total)
				//     └── rack4 (2 GPUs)
				//         ├── node3 (1 GPU) - **Should allocate here
				//         └── node4 (1 GPU)
				//
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/zone":  "zone1",
							"k8s.io/spine": "spine1",
							"k8s.io/rack":  "rack1",
						},
					},
					"node1": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/zone":  "zone1",
							"k8s.io/spine": "spine1",
							"k8s.io/rack":  "rack2",
						},
					},
					"node2": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/zone":  "zone1",
							"k8s.io/spine": "spine1",
							"k8s.io/rack":  "rack3",
						},
					},
					"node3": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/zone":  "zone1",
							"k8s.io/spine": "spine2",
							"k8s.io/rack":  "rack4",
						},
					},
					"node4": {
						GPUs: 1,
						Labels: map[string]string{
							"k8s.io/zone":  "zone1",
							"k8s.io/spine": "spine2",
							"k8s.io/rack":  "rack4",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     5,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department-a",
						DeservedGPUs: 5,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:             "node3",
						GPUsRequired:         1,
						Status:               pod_status.Binding,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
	}
}

func buildEvenlyDistributedTopologyNodes(numZones, numSpinesPerZone, numRacksPerSpine, numNodesPerRack, gpusPerNode int) map[string]nodes_fake.TestNodeBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic)
	nodeID := 0
	for z := 1; z <= numZones; z++ {
		zoneLabel := fmt.Sprintf("zone%d", z)
		for sp := 1; sp <= numSpinesPerZone; sp++ {
			spineLabel := fmt.Sprintf("spine%d", sp+(z-1)*numSpinesPerZone)
			for r := 1; r <= numRacksPerSpine; r++ {
				rackLabel := fmt.Sprintf("rack%d", r+(sp-1)*numRacksPerSpine+(z-1)*numSpinesPerZone*numRacksPerSpine)
				for n := 1; n <= numNodesPerRack; n++ {
					nodeName := fmt.Sprintf("node%d", nodeID)
					nodes[nodeName] = nodes_fake.TestNodeBasic{
						GPUs: gpusPerNode,
						Labels: map[string]string{
							"k8s.io/zone":  zoneLabel,
							"k8s.io/spine": spineLabel,
							"k8s.io/rack":  rackLabel,
						},
					}
					nodeID++
				}
			}
		}
	}

	return nodes
}
