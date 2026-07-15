// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package placement derives the per-NUMA-node attribution of a pod's topology-aligned resources
// from the kubelet podresources data, and serializes it into the annotation the scheduler reads.
package placement

import (
	"encoding/json"
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/cputopology"
)

const (
	cpuResourceName = "cpu"
	regularMemory   = "memory"
)

// Placement maps NRT zone names (e.g. "node-0") to the resources the pod holds in that zone.
// The key matches the zone IDs the scheduler reads from NodeResourceTopology, so the annotation
// can be correlated without a translation step.
type Placement map[string]v1.ResourceList

// zonePlacement is the wire format the scheduler's NUMA plugin decodes from the annotation.
type zonePlacement struct {
	Zone   string          `json:"zone"`
	Amount v1.ResourceList `json:"amount"`
}

// Marshal serializes the placement as the JSON array the scheduler expects:
// [{"zone":"node-0","amount":{"cpu":"8","nvidia.com/gpu":"2"}}, ...]
// Zones are emitted in sorted order for deterministic output.
func (p Placement) Marshal() (string, error) {
	zones := make([]string, 0, len(p))
	for z := range p {
		zones = append(zones, z)
	}
	sort.Strings(zones)

	out := make([]zonePlacement, len(zones))
	for i, z := range zones {
		out[i] = zonePlacement{Zone: z, Amount: p[z]}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Compute attributes a single pod's allocated resources to NUMA nodes. Devices and memory carry
// their NUMA topology directly in the podresources data; CPU IDs are resolved via cpuToNUMA.
// Resources without a resolvable NUMA node are skipped (nothing to attribute). Returns nil when
// the pod holds no topology-aligned resources.
func Compute(pod *podresourcesv1.PodResources, cpuToNUMA cputopology.CPUToNUMA) Placement {
	result := Placement{}

	for _, container := range pod.GetContainers() {
		for _, device := range container.GetDevices() {
			node, ok := singleNUMANode(device.GetTopology())
			if !ok {
				continue
			}
			result.add(device.GetResourceName(), node, int64(len(device.GetDeviceIds())))
		}

		for _, cpuID := range container.GetCpuIds() {
			node, ok := cpuToNUMA[cpuID]
			if !ok {
				continue
			}
			result.add(cpuResourceName, node, 1)
		}

		for _, mem := range container.GetMemory() {
			if mem.GetSize() == 0 {
				continue
			}
			node, ok := singleNUMANode(mem.GetTopology())
			if !ok {
				continue
			}
			result.add(memoryResourceName(mem.GetMemoryType()), node, int64(mem.GetSize()))
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func (p Placement) add(resourceName string, numaNode int, quantity int64) {
	if quantity == 0 {
		return
	}
	zone := fmt.Sprintf("node-%d", numaNode)
	rl, ok := p[zone]
	if !ok {
		rl = v1.ResourceList{}
		p[zone] = rl
	}
	existing := rl[v1.ResourceName(resourceName)]
	existing.Add(*resource.NewQuantity(quantity, resource.DecimalSI))
	rl[v1.ResourceName(resourceName)] = existing
}

// singleNUMANode returns the NUMA node a resource is pinned to. A resource attributed to more
// than one node (or none) cannot be placed unambiguously and is skipped.
func singleNUMANode(topology *podresourcesv1.TopologyInfo) (int, bool) {
	nodes := topology.GetNodes()
	if len(nodes) != 1 {
		return 0, false
	}
	return int(nodes[0].GetID()), true
}

// memoryResourceName keeps regular memory under the canonical "memory" key and preserves the
// kubelet's hugepage type names (e.g. "hugepages-2Mi") as distinct resources.
func memoryResourceName(memoryType string) string {
	if memoryType == "" {
		return regularMemory
	}
	return memoryType
}
