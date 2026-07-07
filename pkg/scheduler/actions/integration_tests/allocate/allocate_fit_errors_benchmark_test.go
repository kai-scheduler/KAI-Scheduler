// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
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

func TestAllocateJobDiscardsFailedAlternativeDiagnosticsAfterSuccess(t *testing.T) {
	ssn, job, nodes := buildAlternativeAllocationSession(t, 2, 2)
	addAlternativeNodeSets(ssn, nodes)
	previousTask := job.GetAllPodsMap()["job-0"]
	previousTaskError := common_info.NewFitErrors()
	previousTaskError.SetError("previous allocation error")
	previousJobError := common_info.NewJobFitError(
		job.Name, podgroup_info.DefaultSubGroup, job.Namespace,
		podgroup_info.PodSchedulingErrors, []string{"previous allocation error"},
	)
	job.ReplaceAllocationFitErrors(previousTask, previousTaskError, []common_info.JobFitError{previousJobError})

	result := common.AllocateJob(ssn, ssn.Statement(), nodes, job, false)
	result.PublishFitErrors(job)

	require.True(t, result.Success)
	require.Empty(t, job.TasksFitErrors)
	require.Empty(t, job.JobFitErrors)
}

func TestAllocateJobPublishesBestFailedAlternative(t *testing.T) {
	ssn, job, nodes := buildAlternativeAllocationSession(t, 3, 2)
	addAlternativeNodeSets(ssn, nodes)

	result := common.AllocateJob(ssn, ssn.Statement(), nodes, job, false)
	result.PublishFitErrors(job)

	require.False(t, result.Success)
	require.Len(t, job.TasksFitErrors, 1)
	for _, taskFitError := range job.TasksFitErrors {
		require.Equal(t, 2, taskFitError.ReasonCount("node(s) didn't have enough resources: GPUs"))
	}
	require.Len(t, job.JobFitErrors, 1)
	require.Contains(t, job.JobFitErrors[0].DetailedMessage(),
		"Resources were found for 2 pods while 3 are required for gang scheduling")
}

func TestPipelineOnlyAllocateJobDoesNotMutateFitErrors(t *testing.T) {
	ssn, job, nodes := buildAlternativeAllocationSession(t, 3, 2)
	task := job.GetAllPodsMap()["job-0"]
	existingTaskError := common_info.NewFitErrors()
	existingTaskError.SetError("existing task error")
	job.SetTaskFitErrors(task, existingTaskError)
	existingJobError := common_info.NewJobFitError(
		job.Name, podgroup_info.DefaultSubGroup, job.Namespace,
		podgroup_info.PodSchedulingErrors, []string{"existing job error"},
	)
	job.AddJobFitError(existingJobError)

	result := common.AllocateJob(ssn, ssn.Statement(), nodes, job, true)

	require.False(t, result.Success)
	require.Same(t, existingTaskError, job.TasksFitErrors[task.UID])
	require.Equal(t, []common_info.JobFitError{existingJobError}, job.JobFitErrors)
}

func buildAlternativeAllocationSession(
	t *testing.T,
	taskCount int,
	nodeCount int,
) (*framework.Session, *podgroup_info.PodGroupInfo, []*node_info.NodeInfo) {
	t.Helper()
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	t.Cleanup(controller.Finish)

	root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	root.AddPodSet(subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, int32(taskCount), nil))
	tasks := make([]*tasks_fake.TestTaskBasic, taskCount)
	for index := range tasks {
		tasks[index] = &tasks_fake.TestTaskBasic{State: pod_status.Pending}
	}
	nodeSpecs := make(map[string]nodes_fake.TestNodeBasic, nodeCount)
	for index := range nodeCount {
		nodeSpecs[fmt.Sprintf("alternative-node-%d", index)] = nodes_fake.TestNodeBasic{GPUs: 1}
	}
	topology := test_utils.TestTopologyBasic{
		Name:  "alternative allocation fit errors",
		Nodes: nodeSpecs,
		Jobs: []*jobs_fake.TestJobBasic{{
			Name:                "job",
			QueueName:           "queue",
			Priority:            constants.PriorityTrainNumber,
			RequiredGPUsPerTask: 1,
			RootSubGroupSet:     root,
			Tasks:               tasks,
		}},
		Queues: []test_utils.TestQueueBasic{{Name: "queue", DeservedGPUs: 100}},
		Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}
	ssn := test_utils.BuildSession(topology, controller)
	nodes := make([]*node_info.NodeInfo, 0, len(ssn.ClusterInfo.Nodes))
	for _, node := range ssn.ClusterInfo.Nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return ssn, ssn.ClusterInfo.PodGroupInfos["job"], nodes
}

func addAlternativeNodeSets(ssn *framework.Session, nodes []*node_info.NodeInfo) {
	ssn.AddSubsetNodesFn(func(
		_ *podgroup_info.PodGroupInfo,
		subGroup *subgroup_info.SubGroupInfo,
		_ map[string]*subgroup_info.PodSet,
		_ []*pod_info.PodInfo,
		nodeSet node_info.NodeSet,
		_ bool,
	) (api.SubsetNodesResult, error) {
		if subGroup.GetName() != podgroup_info.DefaultSubGroup {
			return api.SubsetNodesResult{NodeSets: []node_info.NodeSet{nodeSet}}, nil
		}
		return api.SubsetNodesResult{NodeSets: []node_info.NodeSet{nodes[:1], nodes}}, nil
	})
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
