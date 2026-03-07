// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package actions_test

import (
	"fmt"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/stalegangeviction"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func init() {
	test_utils.InitTestingInfrastructure()
}

// BenchmarkAllocateAction_SmallCluster benchmarks allocation with 10 nodes, 50 jobs
func BenchmarkAllocateAction_SmallCluster(b *testing.B) {
	benchmarkAllocate(b, 10, 50)
}

// BenchmarkAllocateAction_MediumCluster benchmarks allocation with 50 nodes, 200 jobs
func BenchmarkAllocateAction_MediumCluster(b *testing.B) {
	benchmarkAllocate(b, 50, 200)
}

// BenchmarkAllocateAction_LargeCluster benchmarks allocation with 100 nodes, 500 jobs
func BenchmarkAllocateAction_LargeCluster(b *testing.B) {
	benchmarkAllocate(b, 100, 500)
}

func benchmarkAllocate(b *testing.B, numNodes, numJobs int) {
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	topology := createBenchmarkTopology(numNodes, numJobs)
	action := allocate.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ssn := test_utils.BuildSession(topology, ctrl)
		action.Execute(ssn)
	}
}

// BenchmarkReclaimAction_SmallCluster benchmarks reclaim with 10 nodes, 50 jobs
func BenchmarkReclaimAction_SmallCluster(b *testing.B) {
	benchmarkReclaim(b, 10, 50)
}

// BenchmarkReclaimAction_MediumCluster benchmarks reclaim with 50 nodes, 200 jobs
func BenchmarkReclaimAction_MediumCluster(b *testing.B) {
	benchmarkReclaim(b, 50, 200)
}

func benchmarkReclaim(b *testing.B, numNodes, numJobs int) {
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	topology := createBenchmarkTopologyWithRunningJobs(numNodes, numJobs)
	action := reclaim.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ssn := test_utils.BuildSession(topology, ctrl)
		action.Execute(ssn)
	}
}

// BenchmarkPreemptAction_SmallCluster benchmarks preempt with 10 nodes, 50 jobs
func BenchmarkPreemptAction_SmallCluster(b *testing.B) {
	benchmarkPreempt(b, 10, 50)
}

// BenchmarkPreemptAction_MediumCluster benchmarks preempt with 50 nodes, 200 jobs
func BenchmarkPreemptAction_MediumCluster(b *testing.B) {
	benchmarkPreempt(b, 50, 200)
}

func benchmarkPreempt(b *testing.B, numNodes, numJobs int) {
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	topology := createBenchmarkTopologyWithMixedJobs(numNodes, numJobs)
	action := preempt.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ssn := test_utils.BuildSession(topology, ctrl)
		action.Execute(ssn)
	}
}

// BenchmarkConsolidationAction_SmallCluster benchmarks consolidation with 10 nodes, 50 jobs
func BenchmarkConsolidationAction_SmallCluster(b *testing.B) {
	benchmarkConsolidation(b, 10, 50)
}

// BenchmarkConsolidationAction_MediumCluster benchmarks consolidation with 50 nodes, 200 jobs
func BenchmarkConsolidationAction_MediumCluster(b *testing.B) {
	benchmarkConsolidation(b, 50, 200)
}

func benchmarkConsolidation(b *testing.B, numNodes, numJobs int) {
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	topology := createBenchmarkTopologyWithMixedJobs(numNodes, numJobs)
	action := consolidation.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ssn := test_utils.BuildSession(topology, ctrl)
		action.Execute(ssn)
	}
}

// BenchmarkFullSchedulingCycle_SmallCluster benchmarks all actions in sequence with 10 nodes, 50 jobs
func BenchmarkFullSchedulingCycle_SmallCluster(b *testing.B) {
	benchmarkFullCycle(b, 10, 50)
}

// BenchmarkFullSchedulingCycle_MediumCluster benchmarks all actions in sequence with 50 nodes, 200 jobs
func BenchmarkFullSchedulingCycle_MediumCluster(b *testing.B) {
	benchmarkFullCycle(b, 50, 200)
}

// BenchmarkFullSchedulingCycle_LargeCluster benchmarks all actions in sequence with 100 nodes, 500 jobs
func BenchmarkFullSchedulingCycle_LargeCluster(b *testing.B) {
	benchmarkFullCycle(b, 100, 500)
}

func benchmarkFullCycle(b *testing.B, numNodes, numJobs int) {
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	topology := createBenchmarkTopologyWithMixedJobs(numNodes, numJobs)
	actions := []framework.Action{
		allocate.New(),
		consolidation.New(),
		reclaim.New(),
		preempt.New(),
		stalegangeviction.New(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ssn := test_utils.BuildSession(topology, ctrl)
		for _, action := range actions {
			action.Execute(ssn)
		}
	}
}

// BenchmarkManyQueues_MediumCluster benchmarks scheduling with many queues (20 queues, 50 nodes, 200 jobs)
func BenchmarkManyQueues_MediumCluster(b *testing.B) {
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	topology := createBenchmarkTopologyWithManyQueues(50, 200, 20)
	action := allocate.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ssn := test_utils.BuildSession(topology, ctrl)
		action.Execute(ssn)
	}
}

// BenchmarkGangScheduling_MediumCluster benchmarks gang scheduling with multi-task jobs
func BenchmarkGangScheduling_MediumCluster(b *testing.B) {
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	topology := createBenchmarkTopologyWithGangJobs(50, 100, 4)
	action := allocate.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ssn := test_utils.BuildSession(topology, ctrl)
		action.Execute(ssn)
	}
}

// createBenchmarkTopology creates a topology with all pending jobs
func createBenchmarkTopology(numNodes, numJobs int) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic)
	for i := 0; i < numNodes; i++ {
		nodes[fmt.Sprintf("node-%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: 8,
		}
	}

	jobs := make([]*jobs_fake.TestJobBasic, numJobs)
	for i := 0; i < numJobs; i++ {
		queueIdx := i % 4 // Distribute across 4 queues
		jobs[i] = &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("job-%d", i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           fmt.Sprintf("queue-%d", queueIdx),
			Tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Pending},
			},
		}
	}

	totalGPUs := float64(numNodes * 8)
	quarterGPUs := totalGPUs / 4

	return test_utils.TestTopologyBasic{
		Name:  "benchmark-topology",
		Nodes: nodes,
		Jobs:  jobs,
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue-0", ParentQueue: "dept-a", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-1", ParentQueue: "dept-a", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-2", ParentQueue: "dept-b", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-3", ParentQueue: "dept-b", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
		},
		Departments: []test_utils.TestDepartmentBasic{
			{Name: "dept-a", DeservedGPUs: totalGPUs / 2},
			{Name: "dept-b", DeservedGPUs: totalGPUs / 2},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: numJobs * 2,
			},
		},
	}
}

// createBenchmarkTopologyWithRunningJobs creates a topology with running jobs for reclaim testing
func createBenchmarkTopologyWithRunningJobs(numNodes, numJobs int) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic)
	for i := 0; i < numNodes; i++ {
		nodes[fmt.Sprintf("node-%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: 8,
		}
	}

	jobs := make([]*jobs_fake.TestJobBasic, numJobs)
	for i := 0; i < numJobs; i++ {
		queueIdx := i % 4
		nodeIdx := i % numNodes
		jobs[i] = &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("job-%d", i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           fmt.Sprintf("queue-%d", queueIdx),
			Tasks: []*tasks_fake.TestTaskBasic{
				{
					State:    pod_status.Running,
					NodeName: fmt.Sprintf("node-%d", nodeIdx),
				},
			},
		}
	}

	totalGPUs := float64(numNodes * 8)
	quarterGPUs := totalGPUs / 4

	return test_utils.TestTopologyBasic{
		Name:  "benchmark-topology-running",
		Nodes: nodes,
		Jobs:  jobs,
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue-0", ParentQueue: "dept-a", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-1", ParentQueue: "dept-a", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-2", ParentQueue: "dept-b", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-3", ParentQueue: "dept-b", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
		},
		Departments: []test_utils.TestDepartmentBasic{
			{Name: "dept-a", DeservedGPUs: totalGPUs / 2},
			{Name: "dept-b", DeservedGPUs: totalGPUs / 2},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds:     numJobs * 2,
				NumberOfCacheEvictions: numJobs,
			},
		},
	}
}

// createBenchmarkTopologyWithMixedJobs creates a topology with mix of pending and running jobs
func createBenchmarkTopologyWithMixedJobs(numNodes, numJobs int) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic)
	for i := 0; i < numNodes; i++ {
		nodes[fmt.Sprintf("node-%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: 8,
		}
	}

	jobs := make([]*jobs_fake.TestJobBasic, numJobs)
	for i := 0; i < numJobs; i++ {
		queueIdx := i % 4
		nodeIdx := i % numNodes
		state := pod_status.Pending
		nodeName := ""
		if i%2 == 0 { // Half running, half pending
			state = pod_status.Running
			nodeName = fmt.Sprintf("node-%d", nodeIdx)
		}
		jobs[i] = &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("job-%d", i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           fmt.Sprintf("queue-%d", queueIdx),
			Tasks: []*tasks_fake.TestTaskBasic{
				{
					State:    state,
					NodeName: nodeName,
				},
			},
		}
	}

	totalGPUs := float64(numNodes * 8)
	quarterGPUs := totalGPUs / 4

	return test_utils.TestTopologyBasic{
		Name:  "benchmark-topology-mixed",
		Nodes: nodes,
		Jobs:  jobs,
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue-0", ParentQueue: "dept-a", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-1", ParentQueue: "dept-a", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-2", ParentQueue: "dept-b", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-3", ParentQueue: "dept-b", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
		},
		Departments: []test_utils.TestDepartmentBasic{
			{Name: "dept-a", DeservedGPUs: totalGPUs / 2},
			{Name: "dept-b", DeservedGPUs: totalGPUs / 2},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds:      numJobs * 2,
				NumberOfCacheEvictions:  numJobs,
				NumberOfPipelineActions: numJobs * 2,
			},
		},
	}
}

// createBenchmarkTopologyWithManyQueues creates a topology with many queues
func createBenchmarkTopologyWithManyQueues(numNodes, numJobs, numQueues int) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic)
	for i := 0; i < numNodes; i++ {
		nodes[fmt.Sprintf("node-%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: 8,
		}
	}

	jobs := make([]*jobs_fake.TestJobBasic, numJobs)
	for i := 0; i < numJobs; i++ {
		queueIdx := i % numQueues
		jobs[i] = &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("job-%d", i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           fmt.Sprintf("queue-%d", queueIdx),
			Tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Pending},
			},
		}
	}

	totalGPUs := float64(numNodes * 8)
	gpusPerQueue := totalGPUs / float64(numQueues)
	numDepts := (numQueues + 3) / 4 // Ceiling division to get number of departments
	if numDepts < 1 {
		numDepts = 1
	}

	queues := make([]test_utils.TestQueueBasic, numQueues)
	for i := 0; i < numQueues; i++ {
		deptIdx := i % numDepts
		queues[i] = test_utils.TestQueueBasic{
			Name:               fmt.Sprintf("queue-%d", i),
			ParentQueue:        fmt.Sprintf("dept-%d", deptIdx),
			DeservedGPUs:       gpusPerQueue,
			GPUOverQuotaWeight: 1,
		}
	}

	departments := make([]test_utils.TestDepartmentBasic, numDepts)
	gpusPerDept := totalGPUs / float64(numDepts)
	for i := 0; i < numDepts; i++ {
		departments[i] = test_utils.TestDepartmentBasic{
			Name:         fmt.Sprintf("dept-%d", i),
			DeservedGPUs: gpusPerDept,
		}
	}

	return test_utils.TestTopologyBasic{
		Name:        "benchmark-topology-many-queues",
		Nodes:       nodes,
		Jobs:        jobs,
		Queues:      queues,
		Departments: departments,
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: numJobs * 2,
			},
		},
	}
}

// createBenchmarkTopologyWithGangJobs creates a topology with gang jobs (multi-task jobs)
func createBenchmarkTopologyWithGangJobs(numNodes, numJobs, tasksPerJob int) test_utils.TestTopologyBasic {
	nodes := make(map[string]nodes_fake.TestNodeBasic)
	for i := 0; i < numNodes; i++ {
		nodes[fmt.Sprintf("node-%d", i)] = nodes_fake.TestNodeBasic{
			GPUs: 8,
		}
	}

	jobs := make([]*jobs_fake.TestJobBasic, numJobs)
	for i := 0; i < numJobs; i++ {
		queueIdx := i % 4
		tasks := make([]*tasks_fake.TestTaskBasic, tasksPerJob)
		for j := 0; j < tasksPerJob; j++ {
			tasks[j] = &tasks_fake.TestTaskBasic{
				State: pod_status.Pending,
			}
		}
		jobs[i] = &jobs_fake.TestJobBasic{
			Name:                fmt.Sprintf("job-%d", i),
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           fmt.Sprintf("queue-%d", queueIdx),
			Tasks:               tasks,
		}
	}

	totalGPUs := float64(numNodes * 8)
	quarterGPUs := totalGPUs / 4

	return test_utils.TestTopologyBasic{
		Name:  "benchmark-topology-gang",
		Nodes: nodes,
		Jobs:  jobs,
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue-0", ParentQueue: "dept-a", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-1", ParentQueue: "dept-a", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-2", ParentQueue: "dept-b", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
			{Name: "queue-3", ParentQueue: "dept-b", DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1},
		},
		Departments: []test_utils.TestDepartmentBasic{
			{Name: "dept-a", DeservedGPUs: totalGPUs / 2},
			{Name: "dept-b", DeservedGPUs: totalGPUs / 2},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: numJobs * tasksPerJob * 2,
			},
		},
	}
}
