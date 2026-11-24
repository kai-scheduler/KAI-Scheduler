// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestReclaimWithTopologySpreadConstraints(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	for testNumber, testMetadata := range getReclaimTopologySpreadTestsMetadata() {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		reclaimAction := reclaim.New()
		reclaimAction.Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getReclaimTopologySpreadTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	spreadLabels := map[string]string{"app": "spread"}
	spreadOverHosts := []v1.TopologySpreadConstraint{{
		MaxSkew:           1,
		TopologyKey:       hostnameTopologyKey,
		WhenUnsatisfiable: v1.DoNotSchedule,
		LabelSelector:     &metav1.LabelSelector{MatchLabels: spreadLabels},
	}}
	runningJob := func(name, queue, nodeName string, labels map[string]string) *jobs_fake.TestJobBasic {
		return &jobs_fake.TestJobBasic{
			Name:                name,
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           queue,
			Tasks: []*tasks_fake.TestTaskBasic{{
				State:             pod_status.Running,
				NodeName:          nodeName,
				PodAffinityLabels: labels,
			}},
		}
	}
	nonPreemptibleJob := func(job *jobs_fake.TestJobBasic) *jobs_fake.TestJobBasic {
		job.Priority = constants.PriorityBuildNumber
		return job
	}
	pendingSpreadJob := &jobs_fake.TestJobBasic{
		Name:                "pending_job0",
		RequiredGPUsPerTask: 1,
		Priority:            constants.PriorityBuildNumber,
		QueueName:           "queue1",
		Tasks: []*tasks_fake.TestTaskBasic{{
			State:                     pod_status.Pending,
			PodAffinityLabels:         spreadLabels,
			TopologySpreadConstraints: spreadOverHosts,
		}},
	}
	nodes := map[string]nodes_fake.TestNodeBasic{
		"node0": {GPUs: 2, Labels: map[string]string{hostnameTopologyKey: "node0"}},
		"node1": {GPUs: 1, Labels: map[string]string{hostnameTopologyKey: "node1"}},
	}
	queues := func(queue2DeservedGPUs float64) []test_utils.TestQueueBasic {
		return []test_utils.TestQueueBasic{
			{Name: "queue0", DeservedGPUs: 0, GPUOverQuotaWeight: 0},
			{Name: "queue1", DeservedGPUs: 1, GPUOverQuotaWeight: 1},
			{Name: "queue2", DeservedGPUs: queue2DeservedGPUs, GPUOverQuotaWeight: 1},
		}
	}

	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DoNotSchedule spread over hosts: reclaim the victim on the host that keeps the skew",
				Jobs: []*jobs_fake.TestJobBasic{
					nonPreemptibleJob(runningJob("running_job0", "queue2", "node0", spreadLabels)),
					runningJob("running_job1", "queue0", "node0", nil),
					runningJob("running_job2", "queue0", "node1", nil),
					pendingSpreadJob,
				},
				Nodes:  nodes,
				Queues: queues(1),
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"running_job1": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"running_job2": {GPUsRequired: 1, Status: pod_status.Releasing, NodeName: "node1"},
					"pending_job0": {GPUsRequired: 1, Status: pod_status.Pipelined, NodeName: "node1"},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DoNotSchedule spread over hosts: do not reclaim when the only freed host breaks the skew",
				Jobs: []*jobs_fake.TestJobBasic{
					nonPreemptibleJob(runningJob("running_job0", "queue2", "node0", spreadLabels)),
					runningJob("running_job1", "queue0", "node0", nil),
					nonPreemptibleJob(runningJob("running_job2", "queue2", "node1", nil)),
					pendingSpreadJob,
				},
				Nodes:  nodes,
				Queues: queues(2),
				Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"running_job1": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"running_job2": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node1"},
					"pending_job0": {GPUsRequired: 1, Status: pod_status.Pending},
				},
			},
		},
	}
}
