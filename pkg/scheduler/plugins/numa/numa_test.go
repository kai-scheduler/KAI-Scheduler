// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	schedapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
)

// numaZone builds a NUMA zone spec with the given per-resource Available quantities. In tests all
// zones are at full capacity (no pre-existing allocations), so Allocatable == Available.
func numaZone(id string, available map[string]string) node_info.NumaZoneSpec {
	list := v1.ResourceList{}
	for name, qty := range available {
		list[v1.ResourceName(name)] = resource.MustParse(qty)
	}
	return node_info.NumaZoneSpec{ID: id, Available: list, Allocatable: list}
}

// numaTopology builds a NumaTopology from zone specs against a fresh seeded map.
func numaTopology(policy node_info.TopologyManagerPolicy, scope node_info.TopologyManagerScope, zones ...node_info.NumaZoneSpec) *node_info.NumaTopology {
	return nodes_fake.NewNumaTopology(policy, scope, zones...)
}

func singleNUMANodeTopology(scope node_info.TopologyManagerScope, zones ...node_info.NumaZoneSpec) *node_info.NumaTopology {
	return numaTopology(node_info.TopologyPolicySingleNUMANode, scope, zones...)
}

// zoneAvail reads a zone's Available amount for a resource in the request's natural unit (cpu in
// cores, others by count), translating from the vector's milli-cpu storage.
func zoneAvail(topo *node_info.NumaTopology, zoneIdx int, name v1.ResourceName) int64 {
	idx := topo.VectorMap.GetIndex(name)
	val := topo.Zones[zoneIdx].Available.Get(idx)
	if idx == resource_info.CPUIndex {
		return int64(val) / 1000
	}
	return int64(val)
}

// wiredPlugin returns a plugin, a session whose node map holds a single node-a carrying topo,
// and that node (the same object the plugin charges, so predicate observes the in-cycle ledger).
func wiredPlugin(topo *node_info.NumaTopology) (*numaPlugin, *framework.Session, *node_info.NodeInfo) {
	node := &node_info.NodeInfo{Name: "node-a", NumaTopology: topo}
	ssn := &framework.Session{ClusterInfo: &schedapi.ClusterInfo{Nodes: map[string]*node_info.NodeInfo{"node-a": node}}}
	pp := &numaPlugin{ignoreList: sets.New[v1.ResourceName](), ssn: ssn}
	return pp, ssn, node
}

func TestParseIgnoreList(t *testing.T) {
	tests := map[string]struct {
		raw      string
		expected []v1.ResourceName
	}{
		"empty":                  {raw: "", expected: nil},
		"single":                 {raw: "memory", expected: []v1.ResourceName{"memory"}},
		"multiple with spaces":   {raw: " memory , cpu ", expected: []v1.ResourceName{"memory", "cpu"}},
		"trailing comma ignored": {raw: "memory,", expected: []v1.ResourceName{"memory"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			args := framework.PluginArguments{}
			if test.raw != "" {
				args[ignoreListArg] = test.raw
			}
			ignoreList := parseIgnoreList(args)
			assert.Equal(t, len(test.expected), ignoreList.Len())
			for _, r := range test.expected {
				assert.True(t, ignoreList.Has(r), "expected %s in ignoreList", r)
			}
		})
	}
}

// makeTask builds a minimal task with the given QoS, request type and whole-GPU count.
func makeTask(qos v1.PodQOSClass, reqType pod_info.ResourceRequestType, gpus float64) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		ResourceRequestType: reqType,
		GpuRequirement:      *resource_info.NewGpuResourceRequirementWithGpus(gpus, 0),
		Pod:                 &v1.Pod{Status: v1.PodStatus{QOSClass: qos}},
	}
}

func TestShouldHandle(t *testing.T) {
	plugin := &numaPlugin{}
	singleNUMA := &node_info.NumaTopology{Policy: node_info.TopologyPolicySingleNUMANode}
	restricted := &node_info.NumaTopology{Policy: node_info.TopologyPolicyRestricted}
	bestEffort := &node_info.NumaTopology{Policy: node_info.TopologyPolicyBestEffort}

	tests := map[string]struct {
		task     *pod_info.PodInfo
		topo     *node_info.NumaTopology
		expected bool
	}{
		"guaranteed whole-GPU on single-numa-node": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 2), topo: singleNUMA, expected: true,
		},
		"guaranteed whole-GPU on restricted": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), topo: restricted, expected: true,
		},
		"nil node topology passes through": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), topo: nil, expected: false,
		},
		"best-effort policy passes through": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), topo: bestEffort, expected: false,
		},
		"non-guaranteed QoS passes through": {
			task: makeTask(v1.PodQOSBurstable, pod_info.RequestTypeRegular, 1), topo: singleNUMA, expected: false,
		},
		"best-effort QoS passes through": {
			task: makeTask(v1.PodQOSBestEffort, pod_info.RequestTypeRegular, 0), topo: singleNUMA, expected: false,
		},
		"guaranteed fraction request is handled (cpu/memory still aligned)": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeFraction, 0), topo: singleNUMA, expected: true,
		},
		"guaranteed mig request is handled": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeMigInstance, 0), topo: singleNUMA, expected: true,
		},
		"guaranteed cpu/memory-only pod is handled": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 0), topo: singleNUMA, expected: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expected, plugin.shouldHandle(test.task, test.topo))
		})
	}
}

func TestOnSessionOpenRegistersWithoutState(t *testing.T) {
	nodes := map[string]*node_info.NodeInfo{
		"with-single-numa": {
			Name:         "with-single-numa",
			NumaTopology: singleNUMANodeTopology(node_info.TopologyScopeContainer, numaZone("node-0", map[string]string{"cpu": "4"})),
		},
		"without-nrt": {Name: "without-nrt"},
	}

	plugin := New(framework.PluginArguments{}).(*numaPlugin)
	ssn := &framework.Session{ClusterInfo: &schedapi.ClusterInfo{Nodes: nodes}}

	// OnSessionOpen registers callbacks and records the session; OnSessionClose clears it. No state
	// leaks across sessions, so open/close are safe.
	plugin.OnSessionOpen(ssn)
	plugin.OnSessionClose(ssn)

	assert.True(t, plugin.shouldHandle(
		makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1),
		nodes["with-single-numa"].NumaTopology,
	))
	assert.False(t, plugin.shouldHandle(
		makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1),
		nodes["without-nrt"].NumaTopology,
	), "node without topology is a pass-through")
}

// gPod builds a Guaranteed single-container task on node-a with the given requests.
func gPod(uid string, requests map[string]string) *pod_info.PodInfo {
	rl := v1.ResourceList{}
	for name, qty := range requests {
		rl[v1.ResourceName(name)] = resource.MustParse(qty)
	}
	return &pod_info.PodInfo{
		UID:                 common_info.PodID(uid),
		Name:                uid,
		Namespace:           "ns",
		NodeName:            "node-a",
		ResourceRequestType: pod_info.RequestTypeRegular,
		Pod: &v1.Pod{
			Status: v1.PodStatus{QOSClass: v1.PodQOSGuaranteed},
			Spec:   v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Requests: rl}}}},
		},
	}
}

func TestBuildNumaRequests(t *testing.T) {
	always := v1.ContainerRestartPolicyAlways
	cpu := func(q string) v1.ResourceRequirements {
		return v1.ResourceRequirements{Requests: v1.ResourceList{"cpu": resource.MustParse(q)}}
	}
	pod := &v1.Pod{Spec: v1.PodSpec{
		InitContainers: []v1.Container{
			{Resources: cpu("10")},                        // ordinary init: not a steady-state request
			{Resources: cpu("1"), RestartPolicy: &always}, // native sidecar: a steady-state request
		},
		Containers: []v1.Container{{Resources: cpu("2")}, {Resources: cpu("2")}},
	}}
	reqs := buildNumaRequests(pod, resource_info.NewResourceVectorMap())
	cores := func(v resource_info.ResourceVector) int64 { return int64(v.Get(resource_info.CPUIndex)) / 1000 }

	t.Run("pod scope aggregates into one request", func(t *testing.T) {
		concurrent, serial := reqs.forScope(node_info.TopologyScopePod)
		assert.Len(t, concurrent, 1)
		assert.Empty(t, serial, "pod scope folds init containers into the effective pod request")
		// PodRequests = max(init peak 10, sidecar+regulars 1+2+2=5) = 10.
		assert.Equal(t, int64(10), cores(concurrent[0]))
	})

	t.Run("container scope splits concurrent and serial requests", func(t *testing.T) {
		concurrent, serial := reqs.forScope(node_info.TopologyScopeContainer)
		assert.Len(t, concurrent, 3, "native sidecar + two regular containers")
		var total int64
		for _, u := range concurrent {
			total += cores(u)
		}
		assert.Equal(t, int64(5), total, "1 (sidecar) + 2 + 2")

		assert.Len(t, serial, 1, "the ordinary init container is a serial request")
		assert.Equal(t, int64(10), cores(serial[0]))
	})
}

// TestPredicateOrdinaryInitContainer verifies an ordinary init container is checked for
// alignability on its own (rejected if it cannot fit a zone) but is not accumulated into the
// concurrent app containers' headroom.
func TestPredicateOrdinaryInitContainer(t *testing.T) {
	always := v1.ContainerRestartPolicyAlways
	cpu := func(q string) v1.ResourceRequirements {
		return v1.ResourceRequirements{Requests: v1.ResourceList{"cpu": resource.MustParse(q)}}
	}
	build := func(initCPU string, restartable bool) *pod_info.PodInfo {
		init := v1.Container{Resources: cpu(initCPU)}
		if restartable {
			init.RestartPolicy = &always
		}
		return &pod_info.PodInfo{Pod: &v1.Pod{
			Status: v1.PodStatus{QOSClass: v1.PodQOSGuaranteed},
			Spec: v1.PodSpec{
				InitContainers: []v1.Container{init},
				Containers:     []v1.Container{{Resources: cpu("2")}},
			},
		}}
	}

	t.Run("ordinary init within a zone is admitted, not accumulated", func(t *testing.T) {
		pp, _, node := wiredPlugin(singleNUMANodeTopology(node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{"cpu": "4"}),
			numaZone("node-1", map[string]string{"cpu": "4"}),
		))
		// init wants 4 (fits a 4-CPU zone alone); app wants 2. If init were accumulated with the
		// app container, the two together (6) would not fit a single zone — admission proves it isn't.
		_, admit := pp.evaluate(build("4", false), node)
		assert.True(t, admit)
	})

	t.Run("ordinary init larger than any zone is rejected", func(t *testing.T) {
		pp, _, node := wiredPlugin(singleNUMANodeTopology(node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{"cpu": "4"}),
			numaZone("node-1", map[string]string{"cpu": "4"}),
		))
		_, admit := pp.evaluate(build("5", false), node)
		assert.False(t, admit, "an init container that fits no single zone cannot be NUMA-aligned")
	})
}

func TestPredicate(t *testing.T) {
	pp, _, node := wiredPlugin(singleNUMANodeTopology(node_info.TopologyScopePod,
		numaZone("node-0", map[string]string{"cpu": "4"}),
		numaZone("node-1", map[string]string{"cpu": "4"}),
	))

	assert.NoError(t, pp.predicate(gPod("fits", map[string]string{"cpu": "3"}), nil, node))
	assert.Error(t, pp.predicate(gPod("too-big", map[string]string{"cpu": "5"}), nil, node),
		"5 cpu cannot fit a single 4-cpu NUMA zone under single-numa-node")

	assert.NoError(t, pp.predicate(gPod("nonode", map[string]string{"cpu": "5"}), nil, &node_info.NodeInfo{Name: "no-topology"}),
		"nodes without NRT pass through")
}

func TestInCycleReservation(t *testing.T) {
	pp, _, node := wiredPlugin(singleNUMANodeTopology(node_info.TopologyScopePod,
		numaZone("node-0", map[string]string{"cpu": "4"}),
	))
	avail := func() int64 { return zoneAvail(node.NumaTopology, 0, "cpu") }

	first := gPod("first", map[string]string{"cpu": "3"})
	first.NUMAPlacement = pp.placement(first, node) // stamped before the op, as the allocation path does
	pp.allocate(&framework.Event{Task: first})
	assert.Equal(t, int64(1), avail(), "zone charged by the first pod")
	assert.Equal(t, []int{0}, first.NUMAPlacement.ZoneIndices(), "placement recorded on the task (zone 0)")

	second := gPod("second", map[string]string{"cpu": "3"})
	assert.Error(t, pp.predicate(second, nil, node), "only 1 cpu left in the single zone")

	pp.deallocate(&framework.Event{Task: first})
	assert.Equal(t, int64(4), avail(), "zone credited back exactly on rollback")
	assert.NoError(t, pp.predicate(second, nil, node), "zone freed, second pod now fits")
}

func TestAllocateReusesExistingPlacement(t *testing.T) {
	// A task arrives with a placement already set (restored on unevict, or seeded from
	// the annotation). allocate must charge THAT placement, not re-evaluate — a fresh
	// evaluate would pick the lowest zone (node-0), but the placement says node-1.
	pp, _, node := wiredPlugin(singleNUMANodeTopology(node_info.TopologyScopePod,
		numaZone("node-0", map[string]string{"cpu": "4"}),
		numaZone("node-1", map[string]string{"cpu": "4"}),
	))
	task := gPod("seeded", map[string]string{"cpu": "3"})
	task.NUMAPlacement = pod_info.NUMAPlacement{
		{ZoneIndex: 1, Amount: v1.ResourceList{"cpu": resource.MustParse("3")}},
	}

	pp.allocate(&framework.Event{Task: task})
	n0, n1 := zoneAvail(node.NumaTopology, 0, "cpu"), zoneAvail(node.NumaTopology, 1, "cpu")
	assert.Equal(t, int64(4), n0, "node-0 untouched (no re-evaluation)")
	assert.Equal(t, int64(1), n1, "the existing placement on zone 1 is charged")
	assert.Equal(t, []int{1}, task.NUMAPlacement.ZoneIndices(), "placement unchanged")

	pp.deallocate(&framework.Event{Task: task})
	n1 = zoneAvail(node.NumaTopology, 1, "cpu")
	assert.Equal(t, int64(4), n1, "credited back to node-1")
}

func observedZone(zone, cpu string) schedulingv1alpha2.NUMAZonePlacement {
	return schedulingv1alpha2.NUMAZonePlacement{Zone: zone, Amount: v1.ResourceList{"cpu": resource.MustParse(cpu)}}
}

func observedAnnotation(zones ...schedulingv1alpha2.NUMAZonePlacement) string {
	b, _ := json.Marshal(zones)
	return string(b)
}

func TestPlacementFromObserved(t *testing.T) {
	topo := singleNUMANodeTopology(node_info.TopologyScopePod,
		numaZone("node-0", map[string]string{"cpu": "4"}),
		numaZone("node-1", map[string]string{"cpu": "4"}),
	)

	t.Run("translates zone id to index, ordered ascending", func(t *testing.T) {
		got := placementFromRecord([]schedulingv1alpha2.NUMAZonePlacement{observedZone("node-1", "3"), observedZone("node-0", "1")}, topo)
		assert.Equal(t, []int{0, 1}, got.ZoneIndices(), "sorted by zone index")
	})

	t.Run("unknown zone id voids the whole placement", func(t *testing.T) {
		got := placementFromRecord([]schedulingv1alpha2.NUMAZonePlacement{observedZone("node-0", "1"), observedZone("node-9", "1")}, topo)
		assert.Nil(t, got, "a missing zone makes the placement unknown")
	})
}

// TestResolvePlacementRecord covers the precedence the plugin applies when reconstructing a pod's
// durable placement: observed annotation > BindRequest zones > predicted annotation > nil.
func TestResolvePlacementRecord(t *testing.T) {
	observed := []schedulingv1alpha2.NUMAZonePlacement{observedZone("node-0", "1")}
	predicted := []schedulingv1alpha2.NUMAZonePlacement{observedZone("node-1", "2")}
	bindReq := []schedulingv1alpha2.NUMAZonePlacement{observedZone("node-2", "3")}

	podWith := func(ann map[string]string) *v1.Pod {
		pod := &v1.Pod{}
		pod.Annotations = ann
		return pod
	}

	t.Run("observed annotation wins over everything", func(t *testing.T) {
		pod := podWith(map[string]string{
			commonconstants.NumaPlacementObserved:  observedAnnotation(observed...),
			commonconstants.NumaPlacementPredicted: observedAnnotation(predicted...),
		})
		assert.Equal(t, "node-0", resolvePlacementRecord(pod, bindReq)[0].Zone)
	})

	t.Run("BindRequest beats predicted annotation when no observed", func(t *testing.T) {
		pod := podWith(map[string]string{commonconstants.NumaPlacementPredicted: observedAnnotation(predicted...)})
		assert.Equal(t, "node-2", resolvePlacementRecord(pod, bindReq)[0].Zone, "BindRequest is the freshest prediction")
	})

	t.Run("predicted annotation used when no observed and no BindRequest", func(t *testing.T) {
		pod := podWith(map[string]string{commonconstants.NumaPlacementPredicted: observedAnnotation(predicted...)})
		assert.Equal(t, "node-1", resolvePlacementRecord(pod, nil)[0].Zone)
	})

	t.Run("none present yields nil", func(t *testing.T) {
		assert.Nil(t, resolvePlacementRecord(podWith(nil), nil))
	})

	t.Run("malformed observed is ignored, falls through to predicted", func(t *testing.T) {
		pod := podWith(map[string]string{
			commonconstants.NumaPlacementObserved:  "{not json",
			commonconstants.NumaPlacementPredicted: observedAnnotation(predicted...),
		})
		assert.Equal(t, "node-1", resolvePlacementRecord(pod, nil)[0].Zone, "malformed observed ignored, predicted used")
	})
}

func TestSeedObservedPlacements(t *testing.T) {
	topo := singleNUMANodeTopology(node_info.TopologyScopePod,
		numaZone("node-0", map[string]string{"cpu": "4"}),
		numaZone("node-1", map[string]string{"cpu": "4"}),
	)

	// Seeded from the observed annotation.
	withObserved := gPod("observed", map[string]string{"cpu": "2"})
	withObserved.Pod.Annotations = map[string]string{commonconstants.NumaPlacementObserved: observedAnnotation(observedZone("node-1", "2"))}

	// Seeded from the BindRequest (no annotation), read via the session's clone-independent map.
	fromBindRequest := gPod("from-bind-request", map[string]string{"cpu": "2"})
	fromBindRequest.Pod.Namespace, fromBindRequest.Pod.Name = "ns", "from-bind-request"

	alreadyPlaced := gPod("already", map[string]string{"cpu": "2"})
	alreadyPlaced.Pod.Annotations = map[string]string{commonconstants.NumaPlacementObserved: observedAnnotation(observedZone("node-1", "2"))}
	alreadyPlaced.NUMAPlacement = pod_info.NUMAPlacement{{ZoneIndex: 0, Amount: v1.ResourceList{"cpu": resource.MustParse("2")}}}

	noRecord := gPod("none", map[string]string{"cpu": "2"})

	unknownZone := gPod("unknown-zone", map[string]string{"cpu": "2"})
	unknownZone.Pod.Annotations = map[string]string{commonconstants.NumaPlacementObserved: observedAnnotation(observedZone("node-9", "2"))}

	burstable := gPod("burstable", map[string]string{"cpu": "2"})
	burstable.Pod.Status.QOSClass = v1.PodQOSBurstable
	burstable.Pod.Annotations = map[string]string{commonconstants.NumaPlacementObserved: observedAnnotation(observedZone("node-1", "2"))}

	pending := gPod("pending", map[string]string{"cpu": "2"})
	pending.NodeName = ""
	pending.Pod.Annotations = map[string]string{commonconstants.NumaPlacementObserved: observedAnnotation(observedZone("node-1", "2"))}

	// The node holds a *clone* of the running pod (as the snapshot does via addTask). Seeding must
	// target the canonical job task, not this copy — the eviction path credits the job task.
	nodeCopy := withObserved.Clone()
	node := &node_info.NodeInfo{
		Name:         "node-a",
		NumaTopology: topo,
		PodInfos:     map[common_info.PodID]*pod_info.PodInfo{withObserved.UID: nodeCopy},
	}
	job := podgroup_info.NewPodGroupInfo("job", withObserved, fromBindRequest, alreadyPlaced, noRecord, unknownZone, burstable, pending)

	pp := &numaPlugin{ignoreList: sets.New[v1.ResourceName]()}
	ssn := &framework.Session{ClusterInfo: &schedapi.ClusterInfo{
		Nodes:         map[string]*node_info.NodeInfo{"node-a": node},
		PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"job": job},
		BindRequests: bindrequest_info.BindRequestMap{
			bindrequest_info.NewKeyFromPod(fromBindRequest.Pod): &bindrequest_info.BindRequestInfo{
				BindRequest: &schedulingv1alpha2.BindRequest{
					Spec: schedulingv1alpha2.BindRequestSpec{
						PredictedNUMAZones: []schedulingv1alpha2.NUMAZonePlacement{observedZone("node-0", "2")},
					},
				},
			},
		},
	}}

	pp.seedPlacements(ssn)

	assert.Equal(t, []int{1}, withObserved.NUMAPlacement.ZoneIndices(), "observed annotation translated onto the canonical job task")
	assert.Equal(t, []int{0}, fromBindRequest.NUMAPlacement.ZoneIndices(), "BindRequest zones seeded when no annotation")
	assert.Empty(t, nodeCopy.NUMAPlacement, "the node's clone is not the seed target (job task is)")
	assert.Equal(t, []int{0}, alreadyPlaced.NUMAPlacement.ZoneIndices(), "existing placement not overwritten")
	assert.Empty(t, noRecord.NUMAPlacement, "no record ⇒ unaccounted")
	assert.Empty(t, unknownZone.NUMAPlacement, "record naming an unknown zone ⇒ unaccounted")
	assert.Empty(t, burstable.NUMAPlacement, "non-Guaranteed pod is not seeded")
	assert.Empty(t, pending.NUMAPlacement, "pending pod (no node assigned) is not seeded")
}
