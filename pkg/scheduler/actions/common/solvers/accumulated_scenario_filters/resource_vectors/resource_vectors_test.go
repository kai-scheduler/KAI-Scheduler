// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_vectors

import (
	"testing"

	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

var testVectorMap = resource_info.NewResourceVectorMap()

func makeNode(t *testing.T, name string, gpus int, milliCPU, memory int64) *node_info.NodeInfo {
	ctrl := gomock.NewController(t)
	mockAffinity := pod_affinity.NewMockNodePodAffinityInfo(ctrl)
	mockAffinity.EXPECT().AddPod(gomock.Any()).AnyTimes()
	mockAffinity.EXPECT().RemovePod(gomock.Any()).AnyTimes()

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1.NodeStatus{
			Allocatable: v1.ResourceList{
				v1.ResourceCPU:                *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
				v1.ResourceMemory:             *resource.NewQuantity(memory, resource.BinarySI),
				resource_info.GPUResourceName: *resource.NewQuantity(int64(gpus), resource.DecimalSI),
				v1.ResourcePods:               *resource.NewQuantity(110, resource.DecimalSI),
			},
			Capacity: v1.ResourceList{
				v1.ResourceCPU:                *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
				v1.ResourceMemory:             *resource.NewQuantity(memory, resource.BinarySI),
				resource_info.GPUResourceName: *resource.NewQuantity(int64(gpus), resource.DecimalSI),
				v1.ResourcePods:               *resource.NewQuantity(110, resource.DecimalSI),
			},
		},
	}
	return node_info.NewNodeInfo(node, mockAffinity, testVectorMap)
}

func makePendingTask(uid, name string, gpus int, milliCPU, memory float64) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		UID:                    common_info.PodID(uid),
		Name:                   name,
		Namespace:              "ns",
		ResReqVector:           resource_info.NewResourceVectorWithValues(milliCPU, memory, float64(gpus), testVectorMap),
		AcceptedResourceVector: resource_info.NewResourceVector(testVectorMap),
		VectorMap:              testVectorMap,
		GpuRequirement:         *resource_info.NewGpuResourceRequirementWithGpus(float64(gpus), 0),
	}
}

func makeRunningTask(uid, name, nodeName, jobID string, gpus int, milliCPU, memory float64) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		UID:                    common_info.PodID(uid),
		Job:                    common_info.PodGroupID(jobID),
		Name:                   name,
		Namespace:              "ns",
		NodeName:               nodeName,
		ResReqVector:           resource_info.NewResourceVectorWithValues(milliCPU, memory, float64(gpus), testVectorMap),
		AcceptedResourceVector: resource_info.NewResourceVectorWithValues(milliCPU, memory, float64(gpus), testVectorMap),
		AcceptedGpuRequirement: *resource_info.NewGpuResourceRequirementWithGpus(float64(gpus), 0),
		VectorMap:              testVectorMap,
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "ns", UID: k8stypes.UID(uid),
			},
			Spec: v1.PodSpec{NodeName: nodeName},
		},
	}
}

func makeSession(jobs map[common_info.PodGroupID]*podgroup_info.PodGroupInfo) *framework.Session {
	return &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: jobs,
		},
	}
}

func makeScenario(
	session *framework.Session,
	pendingJob *podgroup_info.PodGroupInfo,
	potentialVictims []*pod_info.PodInfo,
	recordedVictimsJobs []*podgroup_info.PodGroupInfo,
) *scenario.ByNodeScenario {
	return scenario.NewByNodeScenario(session, pendingJob, pendingJob, potentialVictims, recordedVictimsJobs)
}

func TestNewResourceVectorFilter_NilScenario(t *testing.T) {
	nodes := map[string]*node_info.NodeInfo{}
	if f := NewResourceVectorFilter(nil, nodes); f != nil {
		t.Error("expected nil filter for nil scenario")
	}
}

func TestFilter_SingleTaskFitsOnIdleNode(t *testing.T) {
	nodeA := makeNode(t, "node-a", 4, 8000, 16000)
	nodes := map[string]*node_info.NodeInfo{"node-a": nodeA}

	pending := makePendingTask("p1", "pending-1", 2, 1000, 4000)
	pendingJob := podgroup_info.NewPodGroupInfo("pjob", pending)

	session := makeSession(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"pjob": pendingJob})
	sc := makeScenario(session, pendingJob, nil, nil)

	f := NewResourceVectorFilter(sc, nodes)
	if f == nil {
		t.Fatal("expected non-nil filter")
	}

	ok, err := f.Filter(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected feasible: task should fit on idle node")
	}
}

func TestFilter_SingleTaskDoesNotFit(t *testing.T) {
	nodeA := makeNode(t, "node-a", 1, 8000, 16000)
	nodes := map[string]*node_info.NodeInfo{"node-a": nodeA}

	pending := makePendingTask("p1", "pending-1", 2, 1000, 4000)
	pendingJob := podgroup_info.NewPodGroupInfo("pjob", pending)

	session := makeSession(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"pjob": pendingJob})
	sc := makeScenario(session, pendingJob, nil, nil)

	f := NewResourceVectorFilter(sc, nodes)
	if f == nil {
		t.Fatal("expected non-nil filter")
	}

	ok, err := f.Filter(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected infeasible: task needs 2 GPUs but node only has 1")
	}
}

func TestFilter_VictimEvictionMakesTaskFit(t *testing.T) {
	nodeA := makeNode(t, "node-a", 4, 8000, 16000)
	victim := makeRunningTask("v1", "victim-1", "node-a", "vjob", 3, 2000, 4000)
	_ = nodeA.AddTask(victim)
	nodes := map[string]*node_info.NodeInfo{"node-a": nodeA}

	pending := makePendingTask("p1", "pending-1", 2, 1000, 4000)
	pendingJob := podgroup_info.NewPodGroupInfo("pjob", pending)

	victimJob := podgroup_info.NewPodGroupInfo("vjob", victim)
	jobs := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
		"pjob": pendingJob,
		"vjob": victimJob,
	}
	session := makeSession(jobs)

	// Scenario with no victims yet -> infeasible (idle=1 GPU)
	sc := makeScenario(session, pendingJob, nil, nil)
	f := NewResourceVectorFilter(sc, nodes)
	if f == nil {
		t.Fatal("expected non-nil filter")
	}

	ok, _ := f.Filter(sc)
	if ok {
		t.Error("expected infeasible before victim eviction")
	}

	// Add victim as potential victim -> feasible (idle=1 + freed=3 = 4 GPUs)
	sc.AddPotentialVictimsTasks([]*pod_info.PodInfo{victim})
	ok, err := f.Filter(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected feasible after victim eviction")
	}
}

func TestFilter_GangPlacedAcrossNodes(t *testing.T) {
	nodeA := makeNode(t, "node-a", 2, 8000, 16000)
	nodeB := makeNode(t, "node-b", 2, 8000, 16000)
	nodes := map[string]*node_info.NodeInfo{"node-a": nodeA, "node-b": nodeB}

	t1 := makePendingTask("p1", "pending-1", 2, 1000, 4000)
	t2 := makePendingTask("p2", "pending-2", 2, 1000, 4000)
	pendingJob := podgroup_info.NewPodGroupInfo("pjob", t1, t2)

	session := makeSession(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"pjob": pendingJob})
	sc := makeScenario(session, pendingJob, nil, nil)

	f := NewResourceVectorFilter(sc, nodes)
	if f == nil {
		t.Fatal("expected non-nil filter")
	}

	ok, err := f.Filter(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected feasible: 2 tasks x 2 GPUs should fit across 2 nodes with 2 GPUs each")
	}
}

func TestFilter_GangDoesNotFit(t *testing.T) {
	nodeA := makeNode(t, "node-a", 2, 8000, 16000)
	nodeB := makeNode(t, "node-b", 2, 8000, 16000)
	nodes := map[string]*node_info.NodeInfo{"node-a": nodeA, "node-b": nodeB}

	t1 := makePendingTask("p1", "pending-1", 2, 1000, 4000)
	t2 := makePendingTask("p2", "pending-2", 2, 1000, 4000)
	t3 := makePendingTask("p3", "pending-3", 2, 1000, 4000)
	pendingJob := podgroup_info.NewPodGroupInfo("pjob", t1, t2, t3)

	session := makeSession(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"pjob": pendingJob})
	sc := makeScenario(session, pendingJob, nil, nil)

	f := NewResourceVectorFilter(sc, nodes)
	if f == nil {
		t.Fatal("expected non-nil filter")
	}

	ok, err := f.Filter(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected infeasible: 3 tasks x 2 GPUs = 6 GPUs needed but only 4 available")
	}
}

func TestFilter_CPUBottleneck(t *testing.T) {
	// Node has enough GPUs but not enough CPU
	nodeA := makeNode(t, "node-a", 4, 1000, 16000)
	nodes := map[string]*node_info.NodeInfo{"node-a": nodeA}

	pending := makePendingTask("p1", "pending-1", 1, 2000, 4000)
	pendingJob := podgroup_info.NewPodGroupInfo("pjob", pending)

	session := makeSession(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"pjob": pendingJob})
	sc := makeScenario(session, pendingJob, nil, nil)

	f := NewResourceVectorFilter(sc, nodes)
	if f == nil {
		t.Fatal("expected non-nil filter")
	}

	ok, err := f.Filter(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected infeasible: node has 1000m CPU but task needs 2000m")
	}
}

func TestFilter_IncrementalVictimAccumulation(t *testing.T) {
	nodeA := makeNode(t, "node-a", 4, 8000, 16000)
	v1task := makeRunningTask("v1", "victim-1", "node-a", "vjob1", 2, 1000, 2000)
	v2task := makeRunningTask("v2", "victim-2", "node-a", "vjob2", 2, 1000, 2000)
	_ = nodeA.AddTask(v1task)
	_ = nodeA.AddTask(v2task)
	nodes := map[string]*node_info.NodeInfo{"node-a": nodeA}

	// Pending task needs 3 GPUs; node has 0 idle
	pending := makePendingTask("p1", "pending-1", 3, 1000, 4000)
	pendingJob := podgroup_info.NewPodGroupInfo("pjob", pending)

	v1job := podgroup_info.NewPodGroupInfo("vjob1", v1task)
	v2job := podgroup_info.NewPodGroupInfo("vjob2", v2task)
	jobs := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
		"pjob":  pendingJob,
		"vjob1": v1job,
		"vjob2": v2job,
	}
	session := makeSession(jobs)
	sc := makeScenario(session, pendingJob, nil, nil)

	f := NewResourceVectorFilter(sc, nodes)
	if f == nil {
		t.Fatal("expected non-nil filter")
	}

	// No victims -> infeasible (0 idle GPUs)
	ok, _ := f.Filter(sc)
	if ok {
		t.Error("expected infeasible with no victims")
	}

	// Add first victim (frees 2 GPUs) -> still infeasible (0+2=2 < 3)
	sc.AddPotentialVictimsTasks([]*pod_info.PodInfo{v1task})
	ok, _ = f.Filter(sc)
	if ok {
		t.Error("expected infeasible with 1 victim (2 GPUs freed, need 3)")
	}

	// Add second victim (frees 2 more GPUs) -> feasible (0+2+2=4 >= 3)
	sc.AddPotentialVictimsTasks([]*pod_info.PodInfo{v2task})
	ok, err := f.Filter(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected feasible with 2 victims (4 GPUs freed, need 3)")
	}
}
