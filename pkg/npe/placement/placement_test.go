// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package placement

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/cputopology"
)

func topology(nodes ...int64) *podresourcesv1.TopologyInfo {
	info := &podresourcesv1.TopologyInfo{}
	for _, n := range nodes {
		info.Nodes = append(info.Nodes, &podresourcesv1.NUMANode{ID: n})
	}
	return info
}

func qty(s string) resource.Quantity {
	return resource.MustParse(s)
}

func TestComputeFullPlacement(t *testing.T) {
	cpuToNUMA := cputopology.CPUToNUMA{0: 0, 1: 0, 2: 0, 3: 1}

	pod := &podresourcesv1.PodResources{
		Containers: []*podresourcesv1.ContainerResources{
			{
				Devices: []*podresourcesv1.ContainerDevices{
					{ResourceName: "nvidia.com/gpu", DeviceIds: []string{"GPU-0", "GPU-1"}, Topology: topology(0)},
				},
				CpuIds: []int64{0, 1, 2, 3},
				Memory: []*podresourcesv1.ContainerMemory{
					{MemoryType: "memory", Size: 1 << 30, Topology: topology(0)},
				},
			},
		},
	}

	got := Compute(pod, cpuToNUMA)
	want := Placement{
		"node-0": v1.ResourceList{
			"nvidia.com/gpu": qty("2"),
			"cpu":            qty("3"),
			"memory":         qty("1073741824"),
		},
		"node-1": v1.ResourceList{
			"cpu": qty("1"),
		},
	}
	assertEqual(t, got, want)

	marshaled, err := got.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const wantJSON = `[{"zone":"node-0","amount":{"cpu":"3","memory":"1073741824","nvidia.com/gpu":"2"}},{"zone":"node-1","amount":{"cpu":"1"}}]`
	if marshaled != wantJSON {
		t.Fatalf("marshal got %q, want %q", marshaled, wantJSON)
	}
}

func TestComputeSkipsUnresolvable(t *testing.T) {
	pod := &podresourcesv1.PodResources{
		Containers: []*podresourcesv1.ContainerResources{
			{
				// Device spanning multiple NUMA nodes cannot be attributed unambiguously.
				Devices: []*podresourcesv1.ContainerDevices{
					{ResourceName: "nvidia.com/gpu", DeviceIds: []string{"GPU-0"}, Topology: topology(0, 1)},
					// Device with no topology.
					{ResourceName: "example.com/nic", DeviceIds: []string{"NIC-0"}},
				},
				// CPU not present in the topology map.
				CpuIds: []int64{99},
				// Zero-size memory block.
				Memory: []*podresourcesv1.ContainerMemory{
					{MemoryType: "memory", Size: 0, Topology: topology(0)},
				},
			},
		},
	}

	if got := Compute(pod, cputopology.CPUToNUMA{}); got != nil {
		t.Fatalf("expected nil placement, got %v", got)
	}
}

func TestComputeAggregatesAcrossContainers(t *testing.T) {
	cpuToNUMA := cputopology.CPUToNUMA{0: 0, 1: 1}
	pod := &podresourcesv1.PodResources{
		Containers: []*podresourcesv1.ContainerResources{
			{CpuIds: []int64{0}},
			{CpuIds: []int64{1}},
		},
	}

	got := Compute(pod, cpuToNUMA)
	want := Placement{
		"node-0": v1.ResourceList{"cpu": qty("1")},
		"node-1": v1.ResourceList{"cpu": qty("1")},
	}
	assertEqual(t, got, want)
}

func assertEqual(t *testing.T, got, want Placement) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for zone, wantRL := range want {
		gotRL, ok := got[zone]
		if !ok {
			t.Fatalf("missing zone %q in %v", zone, got)
		}
		if len(gotRL) != len(wantRL) {
			t.Fatalf("zone %q: got %v, want %v", zone, gotRL, wantRL)
		}
		for res, wantQty := range wantRL {
			gotQty, ok := gotRL[res]
			if !ok {
				t.Fatalf("zone %q: missing resource %q", zone, res)
			}
			if gotQty.Cmp(wantQty) != 0 {
				t.Fatalf("zone %q resource %q: got %s, want %s", zone, res, gotQty.String(), wantQty.String())
			}
		}
	}
}
