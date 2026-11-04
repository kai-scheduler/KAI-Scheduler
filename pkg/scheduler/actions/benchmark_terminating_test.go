// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package actions_test

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const (
	terminatingBenchNodes       = 50
	terminatingBenchGPUsPerNode = 8
	terminatingBenchNodesInRack = 10
	terminatingBenchPendingJobs = 200
	terminatingBenchHostPort    = 8080
	hostnameLabel               = "kubernetes.io/hostname"
	rackLabel                   = "rack"
)

var (
	preprocessLabels = map[string]string{"tier": "preprocess"}
	trainLabels      = map[string]string{"tier": "train"}
)

// BenchmarkAllocateTerminatingPods_MediumCluster: a full cluster where 10% of the pods are
// terminating, some holding a host port and some anti-affine to the pending pods; a third of the
// pending pods are rack anti-affine to the terminating ones.
func BenchmarkAllocateTerminatingPods_MediumCluster(b *testing.B) {
	benchmarkTerminating(b, terminatingTopology(), allocate.New(), false)
}

func BenchmarkReclaimTerminatingPods_MediumCluster(b *testing.B) {
	benchmarkTerminating(b, terminatingTopology(), reclaim.New(), false)
}

// BenchmarkAllocateTerminatingDRAPods_MediumCluster: every GPU is a DRA device and 10% of them
// are held by terminating pods.
func BenchmarkAllocateTerminatingDRAPods_MediumCluster(b *testing.B) {
	benchmarkTerminating(b, terminatingDRATopology(), allocate.New(), true)
}

func BenchmarkReclaimTerminatingDRAPods_MediumCluster(b *testing.B) {
	benchmarkTerminating(b, terminatingDRATopology(), reclaim.New(), true)
}

func benchmarkTerminating(b *testing.B, topology test_utils.TestTopologyBasic, action framework.Action, dra bool) {
	if dra {
		featuregates.SetDynamicResourcesEnabledForTest(true)
		b.Cleanup(func() { featuregates.SetDynamicResourcesEnabledForTest(false) })
	}
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		ssn := test_utils.BuildSession(topology, ctrl)
		if dra {
			// The DRA manager cache is populated asynchronously.
			time.Sleep(time.Millisecond)
		}
		b.StartTimer()
		action.Execute(ssn)
	}
}

// Every GPU is taken. Node i holds a terminating pod on its last GPU unless i%5 == 0, so 40 of
// 400 pods are terminating.
func isTerminatingSlot(node, gpu int) bool {
	return gpu == terminatingBenchGPUsPerNode-1 && node%5 != 0
}

func terminatingTopology() test_utils.TestTopologyBasic {
	var jobs []*jobs_fake.TestJobBasic
	for node := range terminatingBenchNodes {
		for gpu := range terminatingBenchGPUsPerNode {
			task := &tasks_fake.TestTaskBasic{State: pod_status.Running, NodeName: nodeName(node)}
			if isTerminatingSlot(node, gpu) {
				task.State = pod_status.Releasing
				task.PodAffinityLabels = preprocessLabels
				if node%2 == 0 {
					task.HostPorts = []int32{terminatingBenchHostPort}
				} else {
					task.PodAntiAffinitySelector = trainLabels
					task.PodAntiAffinityTopologyKey = hostnameLabel
				}
			}
			jobs = append(jobs, benchJob(fmt.Sprintf("running-%d-%d", node, gpu), gpu%2, task))
		}
	}
	for i := range terminatingBenchPendingJobs {
		task := &tasks_fake.TestTaskBasic{State: pod_status.Pending, PodAffinityLabels: trainLabels}
		switch i % 3 {
		case 0:
			task.PodAntiAffinitySelector = preprocessLabels
			task.PodAntiAffinityTopologyKey = rackLabel
		case 1:
			task.HostPorts = []int32{terminatingBenchHostPort}
		}
		jobs = append(jobs, benchJob(fmt.Sprintf("pending-%d", i), 2+i%2, task))
	}
	return terminatingBenchTopology("terminating-pods", jobs, dra_fake.TestDRAObjects{})
}

func terminatingDRATopology() test_utils.TestTopologyBasic {
	var jobs []*jobs_fake.TestJobBasic
	draObjects := dra_fake.TestDRAObjects{DeviceClasses: []string{"nvidia.com/gpu"}}
	for node := range terminatingBenchNodes {
		draObjects.ResourceSlices = append(draObjects.ResourceSlices, &dra_fake.TestResourceSlice{
			Name: nodeName(node) + "-gpu", DeviceClassName: "nvidia.com/gpu", NodeName: nodeName(node),
			Count: terminatingBenchGPUsPerNode,
		})
		for gpu := range terminatingBenchGPUsPerNode {
			name := fmt.Sprintf("running-%d-%d", node, gpu)
			claimName := name + "-claim"
			task := &tasks_fake.TestTaskBasic{
				State: pod_status.Running, NodeName: nodeName(node), ResourceClaimNames: []string{claimName},
			}
			if isTerminatingSlot(node, gpu) {
				task.State = pod_status.Releasing
			}
			queue := queueName(gpu % 2)
			draObjects.ResourceClaims = append(draObjects.ResourceClaims,
				dra_fake.NewTestResourceClaim(claimName, "test", queue).
					WithAllocation(nodeName(node), fmt.Sprint(gpu), name+"-0").GeneratedFor(name+"-0"))
			job := benchJob(name, gpu%2, task)
			job.RequiredGPUsPerTask = 0
			jobs = append(jobs, job)
		}
	}
	for i := range terminatingBenchPendingJobs {
		name := fmt.Sprintf("pending-%d", i)
		claimName := name + "-claim"
		queue := 2 + i%2
		draObjects.ResourceClaims = append(draObjects.ResourceClaims,
			dra_fake.NewTestResourceClaim(claimName, "test", queueName(queue)))
		job := benchJob(name, queue, &tasks_fake.TestTaskBasic{
			State: pod_status.Pending, ResourceClaimNames: []string{claimName},
		})
		job.RequiredGPUsPerTask = 0
		jobs = append(jobs, job)
	}
	return terminatingBenchTopology("terminating-dra-pods", jobs, draObjects)
}

func benchJob(name string, queue int, task *tasks_fake.TestTaskBasic) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name: name, Namespace: "test", RequiredGPUsPerTask: 1, Priority: constants.PriorityTrainNumber,
		QueueName: queueName(queue), Tasks: []*tasks_fake.TestTaskBasic{task},
	}
}

func nodeName(node int) string {
	return fmt.Sprintf("node-%d", node)
}

func queueName(queue int) string {
	return fmt.Sprintf("queue-%d", queue)
}

// Running pods are in queues 0 and 1, over their quota; pending pods are in queues 2 and 3.
func terminatingBenchTopology(
	name string, jobs []*jobs_fake.TestJobBasic, draObjects dra_fake.TestDRAObjects,
) test_utils.TestTopologyBasic {
	nodes := map[string]nodes_fake.TestNodeBasic{}
	for node := range terminatingBenchNodes {
		nodes[nodeName(node)] = nodes_fake.TestNodeBasic{
			GPUs: terminatingBenchGPUsPerNode,
			Labels: map[string]string{
				hostnameLabel: nodeName(node),
				rackLabel:     fmt.Sprintf("rack-%d", node/terminatingBenchNodesInRack),
			},
		}
	}
	quarterGPUs := float64(terminatingBenchNodes*terminatingBenchGPUsPerNode) / 4
	var queues []test_utils.TestQueueBasic
	for queue := range 4 {
		queues = append(queues, test_utils.TestQueueBasic{
			Name: queueName(queue), DeservedGPUs: quarterGPUs, GPUOverQuotaWeight: 1,
		})
	}
	return test_utils.TestTopologyBasic{
		Name:           name,
		Nodes:          nodes,
		Jobs:           jobs,
		Queues:         queues,
		TestDRAObjects: draObjects,
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds:      len(jobs),
				NumberOfPipelineActions: len(jobs),
				NumberOfCacheEvictions:  len(jobs),
			},
		},
	}
}
