// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestFirstStartWaitIsObservedOnlyWhenAJobFirstStarts(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	tests := []struct {
		name         string
		queue        string
		tasks        []*tasks_fake.TestTaskBasic
		startedAt    *time.Time
		failingBinds bool
		wantObserved bool
	}{
		{
			name:         "pending job that never started",
			queue:        "first-start-queue",
			tasks:        []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			wantObserved: true,
		},
		{
			name:      "pending job that started before",
			queue:     "restarted-queue",
			tasks:     []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			startedAt: ptrTo(time.Now().Add(-5 * time.Minute)),
		},
		{
			name:  "running job that grows",
			queue: "growing-queue",
			tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Running, NodeName: "node0"},
				{State: pod_status.Pending},
			},
		},
		{
			name:         "start whose commit fails",
			queue:        "failing-queue",
			tasks:        []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			failingBinds: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
				Name: tt.name,
				Jobs: []*jobs_fake.TestJobBasic{{
					Name:                "job",
					RequiredGPUsPerTask: 1,
					Priority:            constants.PriorityTrainNumber,
					QueueName:           tt.queue,
					JobAgeInMinutes:     10,
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
					Tasks:               tt.tasks,
				}},
				Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 4}},
				Queues: []test_utils.TestQueueBasic{{Name: tt.queue, DeservedGPUs: 4}},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 1},
				},
			}, controller)
			// Only the start annotation tells a restarted job apart; the growing job must be told apart by
			// its running task alone.
			findJob(t, ssn, "job").LastStartTimestamp = tt.startedAt
			if tt.failingBinds {
				ssn.Cache = &failingBindCache{Cache: ssn.Cache}
			}
			displayName := tt.queue + " display"
			queue := ssn.ClusterInfo.Queues[common_info.QueueID(tt.queue)]
			queue.Name, queue.DisplayName = displayName, displayName
			labels := map[string]string{
				"queue_name": displayName, "queue_metadata_name": tt.queue, "queue_display_name": displayName,
			}
			countBefore, sumBefore := firstStartWaitSamples(t, labels)

			allocate.New().Execute(ssn)

			countAfter, sumAfter := firstStartWaitSamples(t, labels)
			if !tt.wantObserved {
				require.Equal(t, countBefore, countAfter)
				return
			}
			require.Equal(t, countBefore+1, countAfter)
			require.InDelta(t, sumBefore+600, sumAfter, 30)
		})
	}
}

func findJob(t *testing.T, ssn *framework.Session, name string) *podgroup_info.PodGroupInfo {
	t.Helper()
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if job.Name == name {
			return job
		}
	}
	t.Fatalf("job %q not found", name)
	return nil
}

func firstStartWaitSamples(t *testing.T, labels map[string]string) (uint64, float64) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != "pod_group_first_start_wait_seconds" {
			continue
		}
		for _, metric := range family.GetMetric() {
			matched := 0
			for _, label := range metric.GetLabel() {
				if value, found := labels[label.GetName()]; found && value == label.GetValue() {
					matched++
				}
			}
			if matched == len(labels) && len(metric.GetLabel()) == len(labels) {
				return metric.GetHistogram().GetSampleCount(), metric.GetHistogram().GetSampleSum()
			}
		}
	}
	return 0, 0
}

func ptrTo[T any](v T) *T {
	return &v
}
