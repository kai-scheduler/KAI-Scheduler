// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodelocalgreedy

import (
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const prefixDomainLabel = "example.com/domain"

func TestPrefixSearchSinglePodProbeDefersStockSuperset(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "4"},
		"node-b": {gpus: 1, cpu: "4"},
	})
	pending := addPrefixPending(t, ssn, 1, 10, "1", "100m")
	job, tasks := prefixRunningJob(t, ssn, 20, "100m", "1", "node-a", "node-b")
	base := accumulatedBatches(ssn, pending, job, tasks[:1], tasks[1:])

	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: pending,
		ProbeK:            1,
	}}
	emitted := generator.scenariosForPrefix(base)

	require.NotEmpty(t, emitted)
	for _, sn := range emitted {
		require.Len(t, sn.PotentialVictimsTasks(), 1, "touching batches are emitted one batch at a time")
	}
	require.NotEmpty(t, generator.deferredStock)
	require.GreaterOrEqual(t, len(generator.deferredStock[0].PotentialVictimsTasks()), 2,
		"the held stock scenario keeps the accumulated superset")
}

func TestPrefixSearchPartialProbeKeepsStockStream(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "4"},
		"node-b": {gpus: 1, cpu: "4"},
	})
	pending := addPrefixPending(t, ssn, 2, 11, "1", "100m")
	job, tasks := prefixRunningJob(t, ssn, 21, "100m", "1", "node-a", "node-b")
	base := accumulatedBatches(ssn, pending, job, tasks[:1], tasks[1:])
	basePending := base.PendingTasks()[:1]

	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: pending,
		ProbeK:            1,
	}}
	require.False(t, generator.isSinglePodPlacement(len(basePending)))

	narrowed := scenario.NewByNodeScenario(ssn, pending, basePending, nil, nil)
	narrowed.AddPotentialVictimsTasks(base.PotentialVictimsTasks())
	emitted := generator.scenariosForPrefix(narrowed)
	require.Empty(t, generator.deferredStock)
	require.NotEmpty(t, emitted)
	foundSuperset := false
	for _, sn := range emitted {
		if len(sn.PotentialVictimsTasks()) >= 2 {
			foundSuperset = true
		}
	}
	require.True(t, foundSuperset, "a k=1 probe of a larger job stays on the stock stream")
}

func TestPrefixSearchDropsShortCPUSubsetAndHoldsStock(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "500m"},
		"node-b": {gpus: 1, cpu: "4"},
	})
	pending := addPrefixPending(t, ssn, 1, 12, "1", "2")
	job, tasks := prefixRunningJob(t, ssn, 22, "100m", "1", "node-a", "node-b")
	require.NoError(t, ssn.ClusterInfo.Nodes["node-a"].AddTask(tasks[0]))
	require.NoError(t, ssn.ClusterInfo.Nodes["node-b"].AddTask(tasks[1]))
	base := accumulatedBatches(ssn, pending, job, tasks[:1], tasks[1:])

	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: pending,
		ProbeK:            1,
	}}
	emitted := generator.scenariosForPrefix(base)

	require.Len(t, emitted, 1)
	require.Equal(t, []string{"node-b"}, nodeNamesOfTasks(emitted[0].PotentialVictimsTasks()))
	require.NotEmpty(t, generator.deferredStock)

	generator.scenarios = generator.deferredStock
	generator.deferredStock = nil
	fallback := generator.popScenario()
	require.NotNil(t, fallback)
	require.ElementsMatch(t, []string{"node-a", "node-b"}, nodeNamesOfTasks(fallback.PotentialVictimsTasks()))
}

func TestPrefixSearchSkipsScenarioThatCannotHostLargestGPUTask(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-small": {gpus: 1, cpu: "4"},
		"node-open":  {gpus: 2, cpu: "4"},
	})
	pending := addPrefixPending(t, ssn, 1, 13, "2", "100m")
	smallJob, smallTasks := prefixRunningJob(t, ssn, 23, "100m", "1", "node-small")
	require.NoError(t, ssn.ClusterInfo.Nodes["node-small"].AddTask(smallTasks[0]))
	openJob, openTasks := prefixRunningJob(t, ssn, 24, "100m", "1", "node-open")

	short := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil, nil)
	short.AddPotentialVictimsTasks(smallTasks)
	open := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil, nil)
	open.AddPotentialVictimsTasks(openTasks)

	generator := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{
		Session:           ssn,
		PartialPendingJob: pending,
		FeasibleNodes: map[string]*node_info.NodeInfo{
			"node-small": ssn.ClusterInfo.Nodes["node-small"],
		},
	}}
	require.False(t, generator.canHostLargestTask(short))
	generator.solveCtx.FeasibleNodes = ssn.ClusterInfo.Nodes
	require.True(t, generator.canHostLargestTask(short), "idle room on a feasible node keeps the scenario")

	generator.solveCtx.FeasibleNodes = map[string]*node_info.NodeInfo{
		"node-small": ssn.ClusterInfo.Nodes["node-small"],
	}
	generator.scenarios = []*scenario.ByNodeScenario{short, open}
	got := generator.popScenario()
	require.Equal(t, podNames(openTasks), podNames(got.PotentialVictimsTasks()))
	require.Nil(t, generator.popScenario())
	_ = smallJob
	_ = openJob
}

func TestPrefixSearchDomainBatchesStayInsideOneDomain(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{
		"node-a": {gpus: 1, cpu: "4", domain: "a"},
		"node-b": {gpus: 1, cpu: "4", domain: "b"},
	})
	pending := addPrefixPending(t, ssn, 1, 14, "1", "100m")
	pending.RootSubGroupSet = subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
		&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: prefixDomainLabel})
	recorded, recordedTasks := prefixRunningJob(t, ssn, 30, "100m", "1", "node-a")
	job, tasks := prefixRunningJob(t, ssn, 31, "100m", "1", "node-a", "node-b")

	base := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil,
		[]*podgroup_info.PodGroupInfo{recorded})
	base.AddPotentialVictimsTasks(tasks[:1])
	base.AddPotentialVictimsTasks(tasks[1:])

	scenarios := domainBatchScenarios(ssn, base)
	require.Len(t, scenarios, 2)
	byDomain := map[string]*scenario.ByNodeScenario{}
	for _, sn := range scenarios {
		names := nodeNamesOfTasks(sn.PotentialVictimsTasks())
		require.Len(t, names, 1)
		byDomain[ssn.ClusterInfo.Nodes[names[0]].Node.Labels[prefixDomainLabel]] = sn
	}
	require.Equal(t, podNames(tasks[:1]), podNames(byDomain["a"].PotentialVictimsTasks()))
	require.Equal(t, podNames(recordedTasks), podNames(byDomain["a"].RecordedVictimsTasks()))
	require.Equal(t, podNames(tasks[1:]), podNames(byDomain["b"].PotentialVictimsTasks()))
	require.Empty(t, byDomain["b"].RecordedVictimsTasks())
	_ = job
}

func TestSinglePodPlacementIgnoresPartialProbes(t *testing.T) {
	ssn := prefixSession(t, map[string]prefixNodeSpec{"node-a": {gpus: 1, cpu: "4"}})
	one := addPrefixPending(t, ssn, 1, 40, "1", "100m")
	two := addPrefixPending(t, ssn, 2, 41, "1", "100m")
	oneGen := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{Session: ssn, PartialPendingJob: one, ProbeK: 1}}
	twoGen := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{Session: ssn, PartialPendingJob: two, ProbeK: 1}}
	wide := &nodeLocalGreedyGenerator{solveCtx: &solvers.SolveContext{Session: ssn, PartialPendingJob: one, ProbeK: 2}}

	require.True(t, oneGen.isSinglePodPlacement(1))
	require.False(t, twoGen.isSinglePodPlacement(1))
	require.False(t, wide.isSinglePodPlacement(1))
}

type prefixNodeSpec struct {
	gpus   int
	cpu    string
	domain string
}

func prefixSession(t *testing.T, specs map[string]prefixNodeSpec) *framework.Session {
	t.Helper()
	lists := make([]v1.ResourceList, 0, len(specs))
	for _, spec := range specs {
		lists = append(lists, prefixResources(spec.gpus, spec.cpu))
	}
	vectorMap := resource_info.BuildResourceVectorMap(lists)
	nodes := map[string]*node_info.NodeInfo{}
	for name, spec := range specs {
		controller := gomock.NewController(t)
		affinity := pod_affinity.NewMockNodePodAffinityInfo(controller)
		affinity.EXPECT().AddPod(gomock.Any()).AnyTimes()
		affinity.EXPECT().RemovePod(gomock.Any()).AnyTimes()
		labels := map[string]string{}
		if spec.domain != "" {
			labels[prefixDomainLabel] = spec.domain
		}
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
			Status: v1.NodeStatus{
				Allocatable: prefixResources(spec.gpus, spec.cpu),
				Capacity:    prefixResources(spec.gpus, spec.cpu),
			},
		}
		nodes[name] = node_info.NewNodeInfo(node, affinity, vectorMap)
	}
	queue := createQueue("default")
	queue.ParentQueue = ""
	return &framework.Session{ClusterInfo: &api.ClusterInfo{
		PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{},
		Queues: map[common_info.QueueID]*queue_info.QueueInfo{
			queue.UID: queue,
		},
		Nodes: nodes,
	}}
}

func prefixResources(gpus int, cpu string) v1.ResourceList {
	return v1.ResourceList{
		resource_info.GPUResourceName: resource.MustParse(strconv.Itoa(gpus)),
		v1.ResourceCPU:                resource.MustParse(cpu),
		v1.ResourcePods:               resource.MustParse("100"),
	}
}

func addPrefixPending(t *testing.T, ssn *framework.Session, tasks, jobID int, gpus, cpu string) *podgroup_info.PodGroupInfo {
	t.Helper()
	job, _ := createJobWithTasks(tasks, jobID, "team-pending", v1.PodPending, []v1.ResourceRequirements{prefixPodResources(gpus, cpu)})
	ssn.ClusterInfo.PodGroupInfos[job.UID] = job
	return job
}

func prefixRunningJob(
	t *testing.T, ssn *framework.Session, jobID int, cpu, gpus string, nodes ...string,
) (*podgroup_info.PodGroupInfo, []*pod_info.PodInfo) {
	t.Helper()
	job, tasks := createJobWithTasks(len(nodes), jobID, "team-victim", v1.PodRunning, []v1.ResourceRequirements{prefixPodResources(gpus, cpu)})
	ssn.ClusterInfo.PodGroupInfos[job.UID] = job
	for i, task := range tasks {
		task.NodeName = nodes[i]
		task.Pod.Spec.NodeName = nodes[i]
	}
	return job, tasks
}

func prefixPodResources(gpus, cpu string) v1.ResourceRequirements {
	return v1.ResourceRequirements{Requests: v1.ResourceList{
		resource_info.GPUResourceName: resource.MustParse(gpus),
		v1.ResourceCPU:                resource.MustParse(cpu),
	}}
}

func accumulatedBatches(
	ssn *framework.Session, pending, victim *podgroup_info.PodGroupInfo, batches ...[]*pod_info.PodInfo,
) *scenario.ByNodeScenario {
	base := scenario.NewByNodeScenario(ssn, pending, podsOf(pending), nil, nil)
	for _, batch := range batches {
		base.AddPotentialVictimsTasks(batch)
	}
	_ = victim
	return base
}

func nodeNamesOfTasks(tasks []*pod_info.PodInfo) []string {
	seen := map[string]struct{}{}
	for _, task := range tasks {
		if task != nil && task.NodeName != "" {
			seen[task.NodeName] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func podsOf(job *podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	tasks := make([]*pod_info.PodInfo, 0, len(job.GetAllPodsMap()))
	for _, task := range job.GetAllPodsMap() {
		tasks = append(tasks, task)
	}
	return tasks
}
