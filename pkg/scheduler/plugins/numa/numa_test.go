// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	schedapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestParseDenylist(t *testing.T) {
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
				args[denylistArg] = test.raw
			}
			denylist := parseDenylist(args)
			assert.Equal(t, len(test.expected), denylist.Len())
			for _, r := range test.expected {
				assert.True(t, denylist.Has(r), "expected %s in denylist", r)
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
	singleNUMA := &nodeTopology{policy: policySingleNUMANode}
	restricted := &nodeTopology{policy: policyRestricted}
	bestEffort := &nodeTopology{policy: policyBestEffort}

	tests := map[string]struct {
		task     *pod_info.PodInfo
		nt       *nodeTopology
		expected bool
	}{
		"guaranteed whole-GPU on single-numa-node": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 2), nt: singleNUMA, expected: true,
		},
		"guaranteed whole-GPU on restricted": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), nt: restricted, expected: true,
		},
		"nil node topology passes through": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), nt: nil, expected: false,
		},
		"best-effort policy passes through": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), nt: bestEffort, expected: false,
		},
		"non-guaranteed QoS passes through": {
			task: makeTask(v1.PodQOSBurstable, pod_info.RequestTypeRegular, 1), nt: singleNUMA, expected: false,
		},
		"best-effort QoS passes through": {
			task: makeTask(v1.PodQOSBestEffort, pod_info.RequestTypeRegular, 0), nt: singleNUMA, expected: false,
		},
		"guaranteed fraction request is handled (cpu/memory still aligned)": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeFraction, 0), nt: singleNUMA, expected: true,
		},
		"guaranteed mig request is handled": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeMigInstance, 0), nt: singleNUMA, expected: true,
		},
		"guaranteed cpu/memory-only pod is handled": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 0), nt: singleNUMA, expected: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expected, plugin.shouldHandle(test.task, test.nt))
		})
	}
}

func TestOnSessionOpenBuildsTopology(t *testing.T) {
	cpuZone := numaNodeZone("node-0", map[string]string{"cpu": "4"})

	nodes := map[string]*node_info.NodeInfo{
		"with-single-numa": {
			Name:                 "with-single-numa",
			NodeResourceTopology: nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer, cpuZone),
		},
		"with-best-effort": {
			Name:                 "with-best-effort",
			NodeResourceTopology: nrtWithAttributes(policyValueBestEffort, scopeValueContainer, cpuZone),
		},
		"without-nrt": {
			Name: "without-nrt",
		},
	}

	plugin := New(framework.PluginArguments{}).(*numaPlugin)
	ssn := &framework.Session{ClusterInfo: &schedapi.ClusterInfo{Nodes: nodes}}
	plugin.OnSessionOpen(ssn)

	// best-effort still builds a topology entry (its policy is recorded for v2 reuse), but
	// shouldHandle gates it out; only nodes with usable NUMA-node zones get an entry.
	assert.Contains(t, plugin.nodes, "with-single-numa")
	assert.Contains(t, plugin.nodes, "with-best-effort")
	assert.NotContains(t, plugin.nodes, "without-nrt", "node without NRT is a pass-through")

	assert.Equal(t, policySingleNUMANode, plugin.nodes["with-single-numa"].policy)
	assert.True(t, plugin.shouldHandle(
		makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1),
		plugin.nodes["with-single-numa"],
	))

	plugin.OnSessionClose(ssn)
	assert.Nil(t, plugin.nodes)
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

func singleNUMANodeTopology(scope string, zones ...nrtv1alpha2.Zone) *nodeTopology {
	return buildNodeTopology(nrtWithAttributes(policyValueSingleNUMANode, scope, zones...), sets.New[v1.ResourceName]())
}

func newWiredPlugin(nt *nodeTopology) *numaPlugin {
	return &numaPlugin{
		denylist: sets.New[v1.ResourceName](),
		nodes:    map[string]*nodeTopology{"node-a": nt},
		reserved: map[common_info.PodID][]zoneReservation{},
	}
}

func TestRequestUnits(t *testing.T) {
	always := v1.ContainerRestartPolicyAlways
	cpu := func(q string) v1.ResourceRequirements {
		return v1.ResourceRequirements{Requests: v1.ResourceList{"cpu": resource.MustParse(q)}}
	}
	task := &pod_info.PodInfo{Pod: &v1.Pod{Spec: v1.PodSpec{
		InitContainers: []v1.Container{
			{Resources: cpu("10")},                        // ordinary init: not a steady-state unit
			{Resources: cpu("1"), RestartPolicy: &always}, // native sidecar: a steady-state unit
		},
		Containers: []v1.Container{{Resources: cpu("2")}, {Resources: cpu("2")}},
	}}}

	t.Run("pod scope aggregates into one unit", func(t *testing.T) {
		units := requestUnits(task, scopePod)
		assert.Len(t, units, 1)
		// PodRequests = max(init peak 10, sidecar+regulars 1+2+2=5) = 10.
		got := units[0]["cpu"]
		assert.Equal(t, int64(10), got.Value())
	})

	t.Run("container scope yields one unit per concurrent container", func(t *testing.T) {
		units := requestUnits(task, scopeContainer)
		assert.Len(t, units, 3, "native sidecar + two regular containers (ordinary init excluded)")
		var total int64
		for _, u := range units {
			q := u["cpu"]
			total += q.Value()
		}
		assert.Equal(t, int64(5), total, "1 (sidecar) + 2 + 2")
	})
}

func TestPredicate(t *testing.T) {
	node := &node_info.NodeInfo{Name: "node-a"}
	pp := newWiredPlugin(singleNUMANodeTopology(scopeValuePod,
		numaNodeZone("node-0", map[string]string{"cpu": "4"}),
		numaNodeZone("node-1", map[string]string{"cpu": "4"}),
	))

	assert.NoError(t, pp.predicate(gPod("fits", map[string]string{"cpu": "3"}), nil, node))
	assert.Error(t, pp.predicate(gPod("too-big", map[string]string{"cpu": "5"}), nil, node),
		"5 cpu cannot fit a single 4-cpu NUMA zone under single-numa-node")

	assert.NoError(t, pp.predicate(gPod("nonode", map[string]string{"cpu": "5"}), nil, &node_info.NodeInfo{Name: "no-topology"}),
		"nodes without NRT pass through")
}

func TestInCycleReservation(t *testing.T) {
	node := &node_info.NodeInfo{Name: "node-a"}
	pp := newWiredPlugin(singleNUMANodeTopology(scopeValuePod,
		numaNodeZone("node-0", map[string]string{"cpu": "4"}),
	))
	nt := pp.nodes["node-a"]
	avail := func() int64 { q := nt.zones[0].available["cpu"]; return q.Value() }

	first := gPod("first", map[string]string{"cpu": "3"})
	pp.allocate(&framework.Event{Task: first})
	assert.Equal(t, int64(1), avail(), "zone charged by the first pod")
	assert.Contains(t, pp.reserved, first.UID)

	second := gPod("second", map[string]string{"cpu": "3"})
	assert.Error(t, pp.predicate(second, nil, node), "only 1 cpu left in the single zone")

	pp.deallocate(&framework.Event{Task: first})
	assert.Equal(t, int64(4), avail(), "zone credited back exactly on rollback")
	assert.NotContains(t, pp.reserved, first.UID)
	assert.NoError(t, pp.predicate(second, nil, node), "zone freed, second pod now fits")
}
