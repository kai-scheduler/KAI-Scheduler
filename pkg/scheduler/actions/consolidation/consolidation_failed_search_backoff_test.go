// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation_test

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// With a failed-search backoff configured, consolidation reports each search it completes to the
// cache: a job it found no pods to move for starts or extends a backoff, and a job it consolidated
// for ends it.
func TestConsolidationReportsSearchOutcomes(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	isPendingJob := Cond(func(job any) bool { return job.(*podgroup_info.PodGroupInfo).Name == "pending_job" })
	tests := []struct {
		name                string
		runningJobsPriority int32
		expect              func(cacheMock *cache.MockCache)
		mocks               *test_utils.CacheMocking
	}{
		{
			name:                "a job with no pods to move starts a backoff",
			runningJobsPriority: constants.PriorityBuildNumber,
			expect: func(cacheMock *cache.MockCache) {
				cacheMock.EXPECT().RecordFailedSearch(commonconstants.ActionConsolidation, isPendingJob, 10*time.Second, time.Minute)
			},
			mocks: &test_utils.CacheMocking{},
		},
		{
			name:                "a job consolidated for ends its backoff",
			runningJobsPriority: constants.PriorityTrainNumber,
			expect: func(cacheMock *cache.MockCache) {
				cacheMock.EXPECT().ClearFailedSearch(commonconstants.ActionConsolidation, isPendingJob)
			},
			mocks: &test_utils.CacheMocking{NumberOfCacheEvictions: 1, NumberOfPipelineActions: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := NewController(t)
			defer controller.Finish()
			runningJob := func(name, nodeName string) *jobs_fake.TestJobBasic {
				return &jobs_fake.TestJobBasic{
					Name:                name,
					RequiredGPUsPerTask: 2,
					Priority:            tt.runningJobsPriority,
					QueueName:           "queue0",
					Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: nodeName, State: pod_status.Running}},
				}
			}
			ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
				Name: "search outcome",
				Jobs: []*jobs_fake.TestJobBasic{
					runningJob("running_job0", "node0"),
					runningJob("running_job1", "node1"),
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
					},
				},
				Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 4}, "node1": {GPUs: 4}},
				Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 8}},
				Mocks:  &test_utils.TestMock{CacheRequirements: tt.mocks},
			}, controller)
			ssn.Config.ScenarioSearchBudgets = &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: map[string]metav1.Duration{commonconstants.ActionConsolidation: {Duration: 10 * time.Second}},
				MaxFailedSearchBackoff: map[string]metav1.Duration{commonconstants.ActionConsolidation: {Duration: time.Minute}},
			}
			tt.expect(ssn.Cache.(*cache.MockCache))

			consolidation.New().Execute(ssn)
		})
	}
}
