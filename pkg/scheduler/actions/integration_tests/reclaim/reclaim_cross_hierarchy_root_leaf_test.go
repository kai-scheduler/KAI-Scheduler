// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"testing"

	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const (
	rootLeafQueueName       = "non-gpu"
	inferenceChildQueueName = "inference-default"
	defaultDepartmentName   = "default"
)

// TestReclaimCrossHierarchyRootLeafVictimDoesNotPanic reproduces the #1863 panic path:
// a pending job under a named parent reclaims from a running job in a root-level leaf queue
// (ParentQueue == ""). Reclaim's JobSolver probes evictions via EvictAllPreemptees, which
// calls GetMessageOfEviction with Queues[""] on unfixed code.
func TestReclaimCrossHierarchyRootLeafVictimDoesNotPanic(t *testing.T) {
	defer gock.Off()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topology := buildCrossHierarchyRootLeafReclaimTopology()
	ssn := test_utils.BuildSession(topology, ctrl)

	rootLeafQueue := ssn.ClusterInfo.Queues[common_info.QueueID(rootLeafQueueName)]
	inferenceChildQueue := ssn.ClusterInfo.Queues[common_info.QueueID(inferenceChildQueueName)]
	if rootLeafQueue == nil || inferenceChildQueue == nil {
		t.Fatal("expected cross-hierarchy queues to exist in session")
	}
	if rootLeafQueue.ParentQueue != "" {
		t.Fatalf("expected %q to be a root leaf queue, got parent %q", rootLeafQueueName, rootLeafQueue.ParentQueue)
	}
	if inferenceChildQueue.ParentQueue != common_info.QueueID(defaultDepartmentName) {
		t.Fatalf("expected %q parent %q, got %q", inferenceChildQueueName, defaultDepartmentName, inferenceChildQueue.ParentQueue)
	}

	reclaim.New().Execute(ssn)

	victimJob := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("root-leaf-victim")]
	pendingJob := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("inference-pending")]
	if victimJob == nil || pendingJob == nil {
		t.Fatal("expected victim and pending jobs in session")
	}
	if len(victimJob.PodStatusIndex[pod_status.Releasing]) != 1 {
		t.Fatalf("expected root-leaf victim to be releasing, got statuses %#v", victimJob.PodStatusIndex)
	}
	if len(pendingJob.PodStatusIndex[pod_status.Pipelined]) != 1 {
		t.Fatalf("expected inference pending job to be pipelined, got statuses %#v", pendingJob.PodStatusIndex)
	}
}

func buildCrossHierarchyRootLeafReclaimTopology() test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name:                     "cross-hierarchy reclaim from root leaf victim",
		DisableDefaultDepartment: true,
		Departments: []test_utils.TestDepartmentBasic{{
			Name:         defaultDepartmentName,
			DeservedGPUs: 1,
		}},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 1},
		},
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "root-leaf-victim",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           rootLeafQueueName,
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName: "node0",
					State:    pod_status.Running,
				}},
			},
			{
				Name:                "inference-pending",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           inferenceChildQueueName,
				Tasks: []*tasks_fake.TestTaskBasic{{
					State: pod_status.Pending,
				}},
			},
		},
		Queues: []test_utils.TestQueueBasic{
			{
				Name:               inferenceChildQueueName,
				ParentQueue:        defaultDepartmentName,
				DeservedGPUs:       1,
				GPUOverQuotaWeight: 1,
			},
			{
				Name:               rootLeafQueueName,
				ParentQueue:        "",
				DeservedGPUs:       0,
				GPUOverQuotaWeight: 1,
			},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds:      1,
				NumberOfCacheEvictions:  1,
				NumberOfPipelineActions: 1,
			},
		},
	}
}
