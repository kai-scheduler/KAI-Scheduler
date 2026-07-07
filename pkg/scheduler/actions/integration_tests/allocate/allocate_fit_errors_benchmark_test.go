// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const (
	fitErrorBenchmarkRunningQueue = "fit-error-running-queue"
	fitErrorBenchmarkPendingQueue = "fit-error-pending-queue"
	fitErrorBenchmarkDepartment   = "fit-error-department"
	fitErrorBenchmarkGPUsPerNode  = 8
)

var allocateFitErrorsBenchmarkKeepAlive *framework.Session

func TestAllocateFitErrorsResourceFull(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const numNodes = 10
	ssn := test_utils.BuildSession(buildResourceFullFitErrorTopology(numNodes), ctrl)
	allocate.New().Execute(ssn)

	assertPendingFitErrorMessage(t, ssn, numNodes*fitErrorBenchmarkGPUsPerNode,
		fmt.Sprintf("no nodes with enough resources were found: %d node(s) didn't have enough resources: GPUs.", numNodes))
}

func TestAllocateFitErrorsMixedPredicates(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const numNodes = 10
	ssn := test_utils.BuildSession(buildPredicateFitErrorTopology(numNodes), ctrl)
	addMixedFailurePredicate(ssn)
	allocate.New().Execute(ssn)

	assertPendingFitErrorMessage(t, ssn, numNodes*fitErrorBenchmarkGPUsPerNode,
		"no nodes with enough resources were found: 5 node(s) didn't match Pod's node affinity/selector. \n"+
			"5 node(s) had no matching persistent volumes.")
}

func TestAllocateFitErrorsReplacesRepeatedAttempt(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const numNodes = 2
	ssn := test_utils.BuildSession(buildResourceFullFitErrorTopology(numNodes), ctrl)
	action := allocate.New()
	action.Execute(ssn)
	action.Execute(ssn)

	assertPendingFitErrorMessage(t, ssn, numNodes*fitErrorBenchmarkGPUsPerNode,
		"no nodes with enough resources were found: 2 node(s) didn't have enough resources: GPUs.")
}

func BenchmarkAllocateFitErrorsResourceFull_10Nodes(b *testing.B) {
	benchmarkAllocateFitErrors(b, 10, buildResourceFullFitErrorTopology, nil)
}

func BenchmarkAllocateFitErrorsResourceFull_100Nodes(b *testing.B) {
	benchmarkAllocateFitErrors(b, 100, buildResourceFullFitErrorTopology, nil)
}

func BenchmarkAllocateFitErrorsResourceFull_500Nodes(b *testing.B) {
	benchmarkAllocateFitErrors(b, 500, buildResourceFullFitErrorTopology, nil)
}

func BenchmarkAllocateFitErrorsMixedPredicates_10Nodes(b *testing.B) {
	benchmarkAllocateFitErrors(b, 10, buildPredicateFitErrorTopology, addMixedFailurePredicate)
}

func BenchmarkAllocateFitErrorsMixedPredicates_100Nodes(b *testing.B) {
	benchmarkAllocateFitErrors(b, 100, buildPredicateFitErrorTopology, addMixedFailurePredicate)
}

func BenchmarkAllocateFitErrorsMixedPredicates_500Nodes(b *testing.B) {
	benchmarkAllocateFitErrors(b, 500, buildPredicateFitErrorTopology, addMixedFailurePredicate)
}

func benchmarkAllocateFitErrors(
	b *testing.B,
	numNodes int,
	buildTopology func(int) test_utils.TestTopologyBasic,
	configureSession func(*framework.Session),
) {
	test_utils.InitTestingInfrastructure()
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	topology := buildTopology(numNodes)
	action := allocate.New()
	failedTasks := numNodes * fitErrorBenchmarkGPUsPerNode
	recordedReasons := failedTasks
	if configureSession != nil {
		recordedReasons *= 2
	}

	b.ReportAllocs()
	b.ResetTimer()
	var heapLiveDelta uint64
	for range b.N {
		b.StopTimer()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		b.StartTimer()

		ssn := test_utils.BuildSession(topology, ctrl)
		if configureSession != nil {
			configureSession(ssn)
		}
		action.Execute(ssn)
		allocateFitErrorsBenchmarkKeepAlive = ssn

		b.StopTimer()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		if after.HeapAlloc > before.HeapAlloc {
			heapLiveDelta = after.HeapAlloc - before.HeapAlloc
		} else {
			heapLiveDelta = 0
		}
		b.StartTimer()
	}
	b.StopTimer()
	b.ReportMetric(float64(failedTasks), "failed_tasks/op")
	b.ReportMetric(float64(recordedReasons), "recorded_reasons/op")
	b.ReportMetric(float64(heapLiveDelta), "heap_live_delta_bytes/op")
}

func buildResourceFullFitErrorTopology(numNodes int) test_utils.TestTopologyBasic {
	totalGPUs := numNodes * fitErrorBenchmarkGPUsPerNode
	nodes := make(map[string]nodes_fake.TestNodeBasic, numNodes)
	for nodeIndex := range numNodes {
		nodes[fmt.Sprintf("node-%d", nodeIndex)] = nodes_fake.TestNodeBasic{GPUs: fitErrorBenchmarkGPUsPerNode}
	}

	jobs := make([]*jobs_fake.TestJobBasic, 0, totalGPUs*2)
	for jobIndex := range totalGPUs {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("running-job-%d", jobIndex),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           fitErrorBenchmarkRunningQueue,
			Tasks: []*tasks_fake.TestTaskBasic{{
				State:    pod_status.Running,
				NodeName: fmt.Sprintf("node-%d", jobIndex/fitErrorBenchmarkGPUsPerNode),
			}},
		})
	}
	jobs = append(jobs, buildPendingFitErrorJobs(totalGPUs)...)
	return fitErrorBenchmarkTopology("resource full fit errors", nodes, jobs, totalGPUs)
}

func buildPredicateFitErrorTopology(numNodes int) test_utils.TestTopologyBasic {
	totalPendingTasks := numNodes * fitErrorBenchmarkGPUsPerNode
	nodes := make(map[string]nodes_fake.TestNodeBasic, numNodes)
	for nodeIndex := range numNodes {
		nodes[fmt.Sprintf("node-%d", nodeIndex)] = nodes_fake.TestNodeBasic{GPUs: fitErrorBenchmarkGPUsPerNode}
	}
	return fitErrorBenchmarkTopology(
		"mixed predicate fit errors", nodes, buildPendingFitErrorJobs(totalPendingTasks), totalPendingTasks)
}

func buildPendingFitErrorJobs(count int) []*jobs_fake.TestJobBasic {
	jobs := make([]*jobs_fake.TestJobBasic, 0, count)
	for jobIndex := range count {
		jobs = append(jobs, &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("pending-job-%d", jobIndex),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           fitErrorBenchmarkPendingQueue,
			Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
		})
	}
	return jobs
}

func fitErrorBenchmarkTopology(
	name string,
	nodes map[string]nodes_fake.TestNodeBasic,
	jobs []*jobs_fake.TestJobBasic,
	deservedGPUs int,
) test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name:  name,
		Nodes: nodes,
		Jobs:  jobs,
		Queues: []test_utils.TestQueueBasic{
			{
				Name:               fitErrorBenchmarkRunningQueue,
				ParentQueue:        fitErrorBenchmarkDepartment,
				DeservedGPUs:       float64(deservedGPUs),
				GPUOverQuotaWeight: 1,
			},
			{
				Name:               fitErrorBenchmarkPendingQueue,
				ParentQueue:        fitErrorBenchmarkDepartment,
				DeservedGPUs:       float64(deservedGPUs),
				GPUOverQuotaWeight: 1,
			},
		},
		Departments: []test_utils.TestDepartmentBasic{{
			Name:         fitErrorBenchmarkDepartment,
			DeservedGPUs: float64(deservedGPUs * 2),
		}},
	}
}

func addMixedFailurePredicate(ssn *framework.Session) {
	ssn.AddPredicateFn(func(
		task *pod_info.PodInfo,
		_ *podgroup_info.PodGroupInfo,
		node *node_info.NodeInfo,
	) error {
		index, err := strconv.Atoi(strings.TrimPrefix(node.Name, "node-"))
		if err != nil {
			return err
		}
		if index%2 == 0 {
			return common_info.NewFitError(task.Name, task.Namespace, node.Name,
				"node(s) didn't match Pod's node affinity/selector")
		}
		return common_info.NewFitError(task.Name, task.Namespace, node.Name,
			"node(s) had no matching persistent volumes")
	})
}

func assertPendingFitErrorMessage(
	t testing.TB,
	ssn *framework.Session,
	expectedPendingJobs int,
	expectedMessage string,
) {
	t.Helper()
	pendingJobs := 0
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if strings.HasPrefix(job.Name, "pending-job-") {
			pendingJobs++
		}
	}
	if pendingJobs != expectedPendingJobs {
		t.Fatalf("pending jobs = %d, want %d", pendingJobs, expectedPendingJobs)
	}

	pendingJob := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("pending-job-0")]
	if pendingJob == nil {
		t.Fatal("expected pending-job-0 to exist")
	}
	task := pendingJob.PodStatusIndex[pod_status.Pending]["pending-job-0-0"]
	if task == nil {
		t.Fatal("expected pending-job-0-0 to remain pending")
	}
	fitErrors := pendingJob.TasksFitErrors[task.UID]
	if fitErrors == nil {
		t.Fatal("expected pending task to have fit errors")
	}
	if got := fitErrors.Error(); got != expectedMessage {
		t.Fatalf("fitErrors.Error() = %q, want %q", got, expectedMessage)
	}
}
