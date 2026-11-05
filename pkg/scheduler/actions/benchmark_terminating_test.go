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
	terminatingBenchGPUsPerNode = 8
	terminatingBenchNodesInRack = 10
	terminatingBenchHostPort    = 8080
	sharedPoolDevices           = 8
	hostnameLabel               = "kubernetes.io/hostname"
	rackLabel                   = "rack"
)

var (
	preprocessLabels = map[string]string{"tier": "preprocess"}
	trainLabels      = map[string]string{"tier": "train"}
	benchSizes       = []int{50, 500, 1000}
)

type terminatingScenario struct {
	name     string
	topology func(nodes, pending int) test_utils.TestTopologyBasic
	dra      bool
}

// Scenarios on a full cluster with pending single-GPU jobs:
//   - control: no terminating pods.
//   - mixed10: 10% of the pods are terminating, holding a host port or anti-affine to the
//     pending pods; a third of the pending pods are rack anti-affine to them.
//   - mixed30: as mixed10 with 30% terminating.
//   - antiAffinity1: a single terminating pod with required anti-affinity, which sends every
//     failed check of every pod to the future view.
//   - dra*: every GPU is a DRA device; dra*/mixed hold 0/10/30% by terminating pods.
//   - draSharedPool: one releasing device in an AllNodes pool, which sends every DRA pod to the
//     future view on every node.
var terminatingScenarios = []terminatingScenario{
	{name: "control", topology: func(n, p int) test_utils.TestTopologyBasic { return terminatingTopology(n, p, 0) }},
	{name: "mixed10", topology: func(n, p int) test_utils.TestTopologyBasic { return terminatingTopology(n, p, 1) }},
	{name: "mixed30", topology: func(n, p int) test_utils.TestTopologyBasic { return terminatingTopology(n, p, 3) }},
	{name: "antiAffinity1", topology: singleAntiAffineTerminatingTopology},
	{name: "draControl", topology: func(n, p int) test_utils.TestTopologyBasic { return terminatingDRATopology(n, p, 0, false) }, dra: true},
	{name: "dra10", topology: func(n, p int) test_utils.TestTopologyBasic { return terminatingDRATopology(n, p, 1, false) }, dra: true},
	{name: "dra30", topology: func(n, p int) test_utils.TestTopologyBasic { return terminatingDRATopology(n, p, 3, false) }, dra: true},
	{name: "draSharedPool", topology: func(n, p int) test_utils.TestTopologyBasic { return terminatingDRATopology(n, p, 0, true) }, dra: true},
}

func BenchmarkAllocateTerminating(b *testing.B) {
	runTerminatingScenarios(b, func() framework.Action { return allocate.New() }, 200)
}

func BenchmarkReclaimTerminating(b *testing.B) {
	// Reclaim simulates victims per pending job; 200 jobs take ~30s/op on 50 nodes.
	runTerminatingScenarios(b, func() framework.Action { return reclaim.New() }, 20)
}

func runTerminatingScenarios(b *testing.B, newAction func() framework.Action, pending int) {
	for _, scenario := range terminatingScenarios {
		for _, nodes := range benchSizes {
			b.Run(fmt.Sprintf("%s/nodes=%d", scenario.name, nodes), func(b *testing.B) {
				benchmarkTerminating(b, scenario.topology(nodes, pending), newAction(), scenario.dra)
			})
		}
	}
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
			time.Sleep(10 * time.Millisecond)
		}
		b.StartTimer()
		action.Execute(ssn)
	}
}

// Every GPU is taken. The last perNode GPUs of node i hold terminating pods unless i%5 == 0:
// perNode=1 is 10% of the pods, perNode=3 is 30%.
func isTerminatingSlot(node, gpu, perNode int) bool {
	return gpu >= terminatingBenchGPUsPerNode-perNode && node%5 != 0
}

func terminatingTopology(nodes, pending, perNode int) test_utils.TestTopologyBasic {
	var jobs []*jobs_fake.TestJobBasic
	for node := range nodes {
		for gpu := range terminatingBenchGPUsPerNode {
			task := &tasks_fake.TestTaskBasic{State: pod_status.Running, NodeName: nodeName(node)}
			if isTerminatingSlot(node, gpu, perNode) {
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
	for i := range pending {
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
	return terminatingBenchTopology("terminating-pods", nodes, jobs, dra_fake.TestDRAObjects{})
}

// One terminating pod on node-0 is hostname anti-affine to the plain pending pods.
func singleAntiAffineTerminatingTopology(nodes, pending int) test_utils.TestTopologyBasic {
	var jobs []*jobs_fake.TestJobBasic
	for node := range nodes {
		for gpu := range terminatingBenchGPUsPerNode {
			task := &tasks_fake.TestTaskBasic{State: pod_status.Running, NodeName: nodeName(node)}
			if node == 0 && gpu == terminatingBenchGPUsPerNode-1 {
				task.State = pod_status.Releasing
				task.PodAffinityLabels = preprocessLabels
				task.PodAntiAffinitySelector = trainLabels
				task.PodAntiAffinityTopologyKey = hostnameLabel
			}
			jobs = append(jobs, benchJob(fmt.Sprintf("running-%d-%d", node, gpu), gpu%2, task))
		}
	}
	for i := range pending {
		task := &tasks_fake.TestTaskBasic{State: pod_status.Pending, PodAffinityLabels: trainLabels}
		jobs = append(jobs, benchJob(fmt.Sprintf("pending-%d", i), 2+i%2, task))
	}
	return terminatingBenchTopology("single-anti-affine-terminating-pod", nodes, jobs, dra_fake.TestDRAObjects{})
}

// sharedPool adds a fully allocated AllNodes pool whose first device is held by a terminating
// pod on node-0.
func terminatingDRATopology(nodes, pending, perNode int, sharedPool bool) test_utils.TestTopologyBasic {
	var jobs []*jobs_fake.TestJobBasic
	draObjects := dra_fake.TestDRAObjects{DeviceClasses: []string{"nvidia.com/gpu"}}
	addRunning := func(name string, node, queue int, releasing bool, claim *dra_fake.TestResourceClaim) {
		task := &tasks_fake.TestTaskBasic{
			State: pod_status.Running, NodeName: nodeName(node), ResourceClaimNames: []string{claim.Name},
		}
		if releasing {
			task.State = pod_status.Releasing
		}
		draObjects.ResourceClaims = append(draObjects.ResourceClaims, claim.GeneratedFor(name+"-0"))
		job := benchJob(name, queue, task)
		job.RequiredGPUsPerTask = 0
		jobs = append(jobs, job)
	}
	for node := range nodes {
		draObjects.ResourceSlices = append(draObjects.ResourceSlices, &dra_fake.TestResourceSlice{
			Name: nodeName(node) + "-gpu", DeviceClassName: "nvidia.com/gpu", NodeName: nodeName(node),
			Count: terminatingBenchGPUsPerNode,
		})
		for gpu := range terminatingBenchGPUsPerNode {
			name := fmt.Sprintf("running-%d-%d", node, gpu)
			claim := dra_fake.NewTestResourceClaim(name+"-claim", "test", queueName(gpu%2)).
				WithAllocation(nodeName(node), fmt.Sprint(gpu), name+"-0")
			addRunning(name, node, gpu%2, isTerminatingSlot(node, gpu, perNode), claim)
		}
	}
	if sharedPool {
		const pool = "shared-gpu"
		draObjects.ResourceSlices = append(draObjects.ResourceSlices, &dra_fake.TestResourceSlice{
			Name: pool, DeviceClassName: "nvidia.com/gpu", AllNodes: true, Count: sharedPoolDevices,
		})
		for device := range sharedPoolDevices {
			name := fmt.Sprintf("shared-%d", device)
			claim := dra_fake.NewTestResourceClaim(name+"-claim", "test", queueName(0)).
				WithAllocation(pool, fmt.Sprint(device), name+"-0")
			addRunning(name, 0, 0, device == 0, claim)
		}
	}
	for i := range pending {
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
	return terminatingBenchTopology("terminating-dra-pods", nodes, jobs, draObjects)
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
	name string, numNodes int, jobs []*jobs_fake.TestJobBasic, draObjects dra_fake.TestDRAObjects,
) test_utils.TestTopologyBasic {
	nodes := map[string]nodes_fake.TestNodeBasic{}
	for node := range numNodes {
		nodes[nodeName(node)] = nodes_fake.TestNodeBasic{
			GPUs: terminatingBenchGPUsPerNode,
			Labels: map[string]string{
				hostnameLabel: nodeName(node),
				rackLabel:     fmt.Sprintf("rack-%d", node/terminatingBenchNodesInRack),
			},
		}
	}
	quarterGPUs := float64(numNodes*terminatingBenchGPUsPerNode) / 4
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
