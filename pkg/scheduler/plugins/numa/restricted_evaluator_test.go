// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

// initReq is one init container in a test pod: its request, and whether it is a native sidecar
// (restartable, so it runs alongside the app containers) or an ordinary init container.
type initReq struct {
	req         v1.ResourceList
	restartable bool
}

// restrictedPod builds a Guaranteed pod with the given app-container and init-container requests.
// The unique name doubles as the pod UID so the per-task request cache does not alias across rows.
func restrictedPod(name string, apps []v1.ResourceList, inits ...initReq) *pod_info.PodInfo {
	always := v1.ContainerRestartPolicyAlways
	spec := v1.PodSpec{}
	for _, in := range inits {
		c := v1.Container{Resources: v1.ResourceRequirements{Requests: in.req}}
		if in.restartable {
			c.RestartPolicy = &always
		}
		spec.InitContainers = append(spec.InitContainers, c)
	}
	for _, a := range apps {
		spec.Containers = append(spec.Containers, v1.Container{Resources: v1.ResourceRequirements{Requests: a}})
	}
	return &pod_info.PodInfo{
		UID:  common_info.PodID(name),
		Name: name,
		Pod:  &v1.Pod{Status: v1.PodStatus{QOSClass: v1.PodQOSGuaranteed}, Spec: spec},
	}
}

// assertPlacement checks the placement's zones and exact per-zone amounts against want (an empty want
// asserts an empty placement).
func assertPlacement(t *testing.T, placement pod_info.NUMAPlacement, want map[int]v1.ResourceList) {
	t.Helper()

	wantZones := make([]int, 0, len(want))
	for z := range want {
		wantZones = append(wantZones, z)
	}
	sort.Ints(wantZones)
	assert.Equal(t, wantZones, placement.ZoneIndices(), "placement zones")

	for z, amounts := range want {
		got := amountAt(placement, z)
		assert.Lenf(t, got, len(amounts), "resource count on zone %d", z)
		for name, wantQty := range amounts {
			gotQty := got[name]
			assert.Equalf(t, 0, gotQty.Cmp(wantQty), "zone %d resource %s", z, name)
		}
	}
}

// TestRestrictedScopeAndInit exercises the request decomposition feeding the restricted evaluator:
// pod vs container scope, ordinary init containers (serial, checked alone) and native sidecars
// (concurrent, accumulated). Two symmetric NUMA zones, 4 GPU / 8 CPU / 16Gi each.
func TestRestrictedScopeAndInit(t *testing.T) {
	base := []node_info.NumaZoneSpec{
		numaZone("node-0", map[string]string{gpu: "4", "cpu": "8", "memory": "16Gi"}),
		numaZone("node-1", map[string]string{gpu: "4", "cpu": "8", "memory": "16Gi"}),
	}

	tests := []struct {
		name  string
		scope node_info.TopologyManagerScope
		apps  []v1.ResourceList
		inits []initReq
		admit bool
		want  map[int]v1.ResourceList
	}{
		{
			name:  "container scope: second container spills to the second zone",
			scope: node_info.TopologyScopeContainer,
			apps:  []v1.ResourceList{req(gpu, "3"), req(gpu, "3")},
			admit: true,
			want:  map[int]v1.ResourceList{0: req(gpu, "3"), 1: req(gpu, "3")},
		},
		{
			name:  "pod scope: the same pod aggregates into a width-2 mask",
			scope: node_info.TopologyScopePod,
			apps:  []v1.ResourceList{req(gpu, "3"), req(gpu, "3")},
			admit: true,
			want:  map[int]v1.ResourceList{0: req(gpu, "4"), 1: req(gpu, "2")},
		},
		{
			name:  "container scope: per-container widths agree, admits across zones",
			scope: node_info.TopologyScopeContainer,
			apps:  []v1.ResourceList{req(gpu, "3", "memory", "8Gi"), req(gpu, "3", "memory", "8Gi")},
			admit: true,
			want: map[int]v1.ResourceList{
				0: req(gpu, "3", "memory", "8Gi"),
				1: req(gpu, "3", "memory", "8Gi"),
			},
		},
		{
			name:  "pod scope: the same pod rejects on aggregate width disagreement",
			scope: node_info.TopologyScopePod,
			apps:  []v1.ResourceList{req(gpu, "3", "memory", "8Gi"), req(gpu, "3", "memory", "8Gi")},
			admit: false, // pod request 6 GPU (width 2) + 16Gi (width 1) → no common preferred width
		},
		{
			name:  "container scope: ordinary init is checked alone, not accumulated",
			scope: node_info.TopologyScopeContainer,
			inits: []initReq{{req: req(gpu, "4")}},
			apps:  []v1.ResourceList{req(gpu, "4")},
			admit: true,
			want:  map[int]v1.ResourceList{0: req(gpu, "4")}, // app reuses zone 0; the init is not in the placement
		},
		{
			name:  "container scope: an init larger than any mask rejects the pod",
			scope: node_info.TopologyScopeContainer,
			inits: []initReq{{req: req(gpu, "9")}},
			apps:  []v1.ResourceList{req(gpu, "1")},
			admit: false,
		},
		{
			name:  "container scope: a native sidecar is accumulated with the app container",
			scope: node_info.TopologyScopeContainer,
			inits: []initReq{{req: req(gpu, "3"), restartable: true}},
			apps:  []v1.ResourceList{req(gpu, "3")},
			admit: true,
			want:  map[int]v1.ResourceList{0: req(gpu, "3"), 1: req(gpu, "3")}, // sidecar on 0, app pushed to 1
		},
		{
			name:  "pod scope: the request uses the init peak",
			scope: node_info.TopologyScopePod,
			inits: []initReq{{req: req(gpu, "8")}},
			apps:  []v1.ResourceList{req(gpu, "2")},
			admit: true,
			want:  map[int]v1.ResourceList{0: req(gpu, "4"), 1: req(gpu, "4")}, // max(init 8, app 2) = 8 → width 2
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			topo := numaTopology(node_info.TopologyPolicyRestricted, tc.scope, base...)
			pp, _, node := wiredPlugin(topo)

			alloc, admit := pp.evaluate(restrictedPod(tc.name, tc.apps, tc.inits...), node)
			assert.Equal(t, tc.admit, admit)
			if tc.admit {
				assertPlacement(t, placementFromAllocation(alloc, node.NumaTopology), tc.want)
			}
		})
	}
}

// TestRestrictedMaskSelection exercises the evaluator's mask choice when the preferred width (from
// Allocatable) and feasibility (from Available) diverge: masks that exclude the first zone, memory
// participating in the width merge, and a request on a resource the node does not report per zone.
func TestRestrictedMaskSelection(t *testing.T) {
	tests := []struct {
		name  string
		zones []node_info.NumaZoneSpec
		req   v1.ResourceList
		admit bool
		want  map[int]v1.ResourceList
	}{
		{
			name: "width-1 mask lands on the second zone",
			zones: []node_info.NumaZoneSpec{
				partialZone("node-0", map[string]string{gpu: "4"}, map[string]string{gpu: "1"}),
				partialZone("node-1", map[string]string{gpu: "4"}, map[string]string{gpu: "4"}),
			},
			req:   req(gpu, "3"),
			admit: true,
			want:  map[int]v1.ResourceList{1: req(gpu, "3")}, // preferred width 1, only zone 1 is feasible
		},
		{
			name: "width-2 mask excludes the first zone",
			zones: []node_info.NumaZoneSpec{
				partialZone("node-0", map[string]string{gpu: "4"}, map[string]string{gpu: "1"}),
				partialZone("node-1", map[string]string{gpu: "4"}, map[string]string{gpu: "4"}),
				partialZone("node-2", map[string]string{gpu: "4"}, map[string]string{gpu: "4"}),
			},
			req:   req(gpu, "6"),
			admit: true,
			want:  map[int]v1.ResourceList{1: req(gpu, "4"), 2: req(gpu, "2")}, // {0,1} and {0,2} infeasible → {1,2}
		},
		{
			name: "memory-driven width disagreement rejects",
			zones: []node_info.NumaZoneSpec{
				numaZone("node-0", map[string]string{gpu: "4", "cpu": "8", "memory": "16Gi"}),
				numaZone("node-1", map[string]string{gpu: "4", "cpu": "8", "memory": "16Gi"}),
			},
			req:   req(gpu, "2", "memory", "20Gi"),
			admit: false, // GPU width 1, memory 20Gi width 2 → no common preferred width
		},
		{
			name: "a non-reported resource is trivially aligned",
			zones: []node_info.NumaZoneSpec{
				numaZone("node-0", map[string]string{gpu: "4"}),
				numaZone("node-1", map[string]string{gpu: "4"}),
			},
			req:   req("cpu", "2"),
			admit: true,
			want:  map[int]v1.ResourceList{}, // cpu is not zone-reported → no aware request → empty placement
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			topo := numaTopology(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer, tc.zones...)
			pp, _, node := wiredPlugin(topo)

			alloc, admit := pp.evaluate(restrictedPod(tc.name, []v1.ResourceList{tc.req}), node)
			assert.Equal(t, tc.admit, admit)
			if tc.admit {
				assertPlacement(t, placementFromAllocation(alloc, node.NumaTopology), tc.want)
			}
		})
	}
}
