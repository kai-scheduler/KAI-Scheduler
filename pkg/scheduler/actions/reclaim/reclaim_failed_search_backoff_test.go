// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// With a failed-search backoff configured, reclaim reports each search it completes to the cache: a
// job it found no victims for starts or extends a backoff, and a job it reclaimed for ends it.
func TestReclaimReportsSearchOutcomes(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	isPendingJob := Cond(func(job any) bool { return job.(*podgroup_info.PodGroupInfo).Name == "q1_pending_job" })
	tests := []struct {
		name               string
		runningJobPriority int32
		expect             func(cacheMock *cache.MockCache)
		mocks              *test_utils.CacheMocking
	}{
		{
			name:               "a job with no victims starts a backoff",
			runningJobPriority: constants.PriorityBuildNumber,
			expect: func(cacheMock *cache.MockCache) {
				cacheMock.EXPECT().RecordFailedSearch(commonconstants.ActionReclaim, isPendingJob, 10*time.Second, time.Minute)
			},
			mocks: &test_utils.CacheMocking{},
		},
		{
			name:               "a job reclaimed for ends its backoff",
			runningJobPriority: constants.PriorityTrainNumber,
			expect: func(cacheMock *cache.MockCache) {
				cacheMock.EXPECT().ClearFailedSearch(commonconstants.ActionReclaim, isPendingJob)
			},
			mocks: &test_utils.CacheMocking{NumberOfCacheEvictions: 2, NumberOfPipelineActions: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := NewController(t)
			defer controller.Finish()
			ssn := test_utils.BuildSession(searchOutcomeTopology(tt.runningJobPriority, tt.mocks), controller)
			ssn.Config.ScenarioSearchBudgets = &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: map[string]metav1.Duration{commonconstants.ActionReclaim: {Duration: 10 * time.Second}},
				MaxFailedSearchBackoff: map[string]metav1.Duration{commonconstants.ActionReclaim: {Duration: time.Minute}},
			}
			tt.expect(ssn.Cache.(*cache.MockCache))

			reclaim.New().Execute(ssn)
		})
	}
}

// searchOutcomeTopology runs a job of queue0 with runningJobPriority on two nodes, beside a pending
// job of queue1 that fits only if one of its tasks is reclaimed.
func searchOutcomeTopology(runningJobPriority int32, mocks *test_utils.CacheMocking) test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name: "search outcome",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "q0_running_job",
				RequiredGPUsPerTask: 10,
				Priority:            runningJobPriority,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{NodeName: "node0", State: pod_status.Running},
					{NodeName: "node1", State: pod_status.Running},
				},
			},
			{
				Name:                "q1_pending_job",
				RequiredGPUsPerTask: 10,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue1",
				Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 16}, "node1": {GPUs: 16}},
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue0", DeservedGPUs: 10, GPUOverQuotaWeight: 1},
			{Name: "queue1", DeservedGPUs: 10, GPUOverQuotaWeight: 1},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: mocks},
	}
}
