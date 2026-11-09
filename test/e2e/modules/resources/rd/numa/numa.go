// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package numa provides read-only discovery over the cluster's NodeResourceTopology (NRT) objects and
// helpers for building Guaranteed-QoS pods, for the NUMA-aware scheduling e2e suite. Tests never create
// NRT/nodes; they discover pre-provisioned NUMA nodes and skip when the prerequisites are absent.
package numa

import (
	"context"
	"encoding/json"
	"fmt"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	ginkgo "github.com/onsi/ginkgo/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	commonconstants "github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

// Topology Manager policy values as reported in NRT (mirror the kubelet policy names).
const (
	PolicyNone           = "none"
	PolicyBestEffort     = "best-effort"
	PolicyRestricted     = "restricted"
	PolicySingleNUMANode = "single-numa-node"
)

const (
	zoneTypeNode              = "Node"
	attrTopologyManagerPolicy = "topologyManagerPolicy"
	hostnameLabelKey          = "kubernetes.io/hostname"
)

// Zone is a single NUMA-node zone's free/total resources as reported in NRT.
type Zone struct {
	ID          string
	Available   v1.ResourceList
	Allocatable v1.ResourceList
}

// Node is the NUMA view of a cluster node built from its NRT object.
type Node struct {
	Name   string
	Policy string
	Zones  []Zone
}

// Requirement describes what a test case needs from a NUMA node. A zero value matches any NUMA node.
type Requirement struct {
	Policy   string // "" matches any policy
	Modeled  bool   // when true, match only modeled policies (single-numa-node or restricted)
	MinNodes int    // minimum number of matching nodes (default 1)
	MinZones int    // minimum NUMA-node zones per matching node
	ZoneGPUs int64  // require at least one zone with this many free GPUs
}

// IsModeledPolicy reports whether the numa plugin enforces alignment on this node's policy.
func (n Node) IsModeledPolicy() bool {
	return n.Policy == PolicySingleNUMANode || n.Policy == PolicyRestricted
}

// Pin confines the pod to this node via a hostname nodeSelector, returning the pod for chaining. A
// policy-specific spec must pin its pod so the discovered node's Topology Manager policy governs the
// outcome; otherwise, when several policies coexist (the numa-full layout), the pod can schedule on a
// more permissive node and invalidate the assertion.
func (n Node) Pin(pod *v1.Pod) *v1.Pod {
	if pod.Spec.NodeSelector == nil {
		pod.Spec.NodeSelector = map[string]string{}
	}
	pod.Spec.NodeSelector[hostnameLabelKey] = n.Name
	return pod
}

// HostSelector returns a nodeSelector confining a pod (or pod template) to this node's host. Use it to
// pin pods built outside a *v1.Pod (e.g. a Job's pod template) where Pin doesn't apply.
func (n Node) HostSelector() map[string]string {
	return map[string]string{hostnameLabelKey: n.Name}
}

// List returns the NUMA view of every node that publishes an NRT object.
func List(ctx context.Context, c runtimeClient.Client) ([]Node, error) {
	nrtList := &nrtv1alpha2.NodeResourceTopologyList{}
	if err := c.List(ctx, nrtList); err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(nrtList.Items))
	for i := range nrtList.Items {
		nodes = append(nodes, buildNode(&nrtList.Items[i]))
	}
	return nodes, nil
}

// RequireNodes discovers NUMA nodes matching req, or Skips the running spec when the NRT CRD is absent
// or too few nodes match.
func RequireNodes(ctx context.Context, c runtimeClient.Client, req Requirement) []Node {
	nodes, err := List(ctx, c)
	if err != nil {
		ginkgo.Skip(fmt.Sprintf("NodeResourceTopology not available (%v); skipping NUMA test", err))
	}

	minNodes := req.MinNodes
	if minNodes == 0 {
		minNodes = 1
	}

	matches := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if matchesRequirement(node, req) {
			matches = append(matches, node)
		}
	}
	if len(matches) < minNodes {
		ginkgo.Skip(fmt.Sprintf(
			"need %d NUMA node(s) matching %+v, found %d; skipping", minNodes, req, len(matches)))
	}
	return matches
}

func matchesRequirement(node Node, req Requirement) bool {
	if req.Policy != "" && node.Policy != req.Policy {
		return false
	}
	if req.Modeled && !node.IsModeledPolicy() {
		return false
	}
	if req.MinZones > 0 && len(node.Zones) < req.MinZones {
		return false
	}
	if req.ZoneGPUs > 0 && node.MaxZoneGPUs() < req.ZoneGPUs {
		return false
	}
	return true
}

// Sizing helpers read per-zone Allocatable (capacity), not Available: Available lags real free capacity
// after workload churn (the NRT is republished on an interval), while each spec starts from a clean node
// and the scheduler reconstructs actual availability from placements. Sizing from capacity is stable.

// MaxZoneGPUs is the largest GPU capacity of any single zone.
func (n Node) MaxZoneGPUs() int64 {
	var max int64
	for _, zone := range n.Zones {
		if cap := zoneGPU(zone.Allocatable); cap > max {
			max = cap
		}
	}
	return max
}

// TotalGPUs is the GPU capacity summed across all zones.
func (n Node) TotalGPUs() int64 {
	var total int64
	for _, zone := range n.Zones {
		total += zoneGPU(zone.Allocatable)
	}
	return total
}

// OneZoneGPUs returns a GPU count that fits within a single zone, and whether such a request exists.
func (n Node) OneZoneGPUs() (int64, bool) {
	gpus := n.MaxZoneGPUs()
	return gpus, gpus >= 1
}

// SpanTwoZonesGPUs returns a GPU count that no single zone can satisfy but the node's zones can together,
// and whether such a request exists on this node.
func (n Node) SpanTwoZonesGPUs() (int64, bool) {
	maxZone := n.MaxZoneGPUs()
	if len(n.Zones) < 2 || maxZone < 1 || n.TotalGPUs() <= maxZone {
		return 0, false
	}
	return maxZone + 1, true
}

func zoneGPU(list v1.ResourceList) int64 {
	if qty, ok := list[commonconstants.NvidiaGpuResource]; ok {
		return qty.Value()
	}
	return 0
}

// MaxZoneCPU is the largest CPU capacity of any single zone.
func (n Node) MaxZoneCPU() resource.Quantity {
	var max resource.Quantity
	for _, zone := range n.Zones {
		if cpu, ok := zone.Allocatable[v1.ResourceCPU]; ok && cpu.Cmp(max) > 0 {
			max = cpu
		}
	}
	return max
}

// TotalCPU is the CPU capacity summed across all zones.
func (n Node) TotalCPU() resource.Quantity {
	total := resource.Quantity{}
	for _, zone := range n.Zones {
		if cpu, ok := zone.Allocatable[v1.ResourceCPU]; ok {
			total.Add(cpu)
		}
	}
	return total
}

// TwoZoneCPU returns a CPU request that no single zone can satisfy but the node's zones can together
// (forcing a width-2 NUMA mask), and whether such a request exists. Used to build a restricted-policy
// pod whose CPU and GPU minimal widths agree.
func (n Node) TwoZoneCPU() (resource.Quantity, bool) {
	maxZone := n.MaxZoneCPU()
	if maxZone.IsZero() {
		return resource.Quantity{}, false
	}
	req := maxZone.DeepCopy()
	req.Add(resource.MustParse("1"))
	if req.Cmp(n.TotalCPU()) > 0 {
		return resource.Quantity{}, false
	}
	return req, true
}

// MaxZoneMemory is the largest memory capacity of any single zone.
func (n Node) MaxZoneMemory() resource.Quantity {
	var max resource.Quantity
	for _, zone := range n.Zones {
		if mem, ok := zone.Allocatable[v1.ResourceMemory]; ok && mem.Cmp(max) > 0 {
			max = mem
		}
	}
	return max
}

// TotalMemory is the memory capacity summed across all zones.
func (n Node) TotalMemory() resource.Quantity {
	total := resource.Quantity{}
	for _, zone := range n.Zones {
		if mem, ok := zone.Allocatable[v1.ResourceMemory]; ok {
			total.Add(mem)
		}
	}
	return total
}

// TwoZoneMemory returns a memory request that no single zone can satisfy but the node's zones can
// together (forcing a width-2 NUMA mask), and whether such a request exists. Restricted requires every
// topology-aware resource to share the same minimal width, so a width-2 GPU/CPU pod must also request
// memory spanning two zones - otherwise memory's width-1 minimum disagrees and the pod is rejected.
func (n Node) TwoZoneMemory() (resource.Quantity, bool) {
	maxZone := n.MaxZoneMemory()
	if maxZone.IsZero() {
		return resource.Quantity{}, false
	}
	req := maxZone.DeepCopy()
	req.Add(resource.MustParse("1Mi"))
	if req.Cmp(n.TotalMemory()) > 0 {
		return resource.Quantity{}, false
	}
	return req, true
}

func buildNode(nrt *nrtv1alpha2.NodeResourceTopology) Node {
	node := Node{Name: nrt.Name, Policy: policyOf(nrt)}
	for i := range nrt.Zones {
		nrtZone := &nrt.Zones[i]
		if nrtZone.Type != zoneTypeNode {
			continue
		}
		zone := Zone{
			ID:          nrtZone.Name,
			Available:   v1.ResourceList{},
			Allocatable: v1.ResourceList{},
		}
		for _, ri := range nrtZone.Resources {
			zone.Available[v1.ResourceName(ri.Name)] = ri.Available.DeepCopy()
			zone.Allocatable[v1.ResourceName(ri.Name)] = ri.Allocatable.DeepCopy()
		}
		node.Zones = append(node.Zones, zone)
	}
	return node
}

func policyOf(nrt *nrtv1alpha2.NodeResourceTopology) string {
	for _, attr := range nrt.Attributes {
		if attr.Name == attrTopologyManagerPolicy {
			return attr.Value
		}
	}
	if len(nrt.TopologyPolicies) > 0 {
		return nrt.TopologyPolicies[0]
	}
	return ""
}

// ObservedZones returns the NUMA placement the exporter published for a pod, or nil when the annotation
// is absent or malformed.
func ObservedZones(pod *v1.Pod) []schedulingv1alpha2.NUMAZonePlacement {
	raw, ok := pod.Annotations[commonconstants.NumaPlacementObserved]
	if !ok {
		return nil
	}
	var record []schedulingv1alpha2.NUMAZonePlacement
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil
	}
	return record
}

// GuaranteedGPUPod builds (does not create) a Guaranteed-QoS pod requesting the given number of GPUs.
// GPU alignment is what the NUMA plugin reasons about; the small cpu/memory limits==requests make the
// pod Guaranteed so the plugin handles it.
func GuaranteedGPUPod(q *v2.Queue, gpus int64) *v1.Pod {
	return GuaranteedPod(q, gpuResourceList(gpus))
}

// GuaranteedGPURequirements returns the ResourceRequirements of a Guaranteed GPU pod, for callers (e.g.
// PodGroup creation) that build pods from requirements rather than a queue.
func GuaranteedGPURequirements(gpus int64) v1.ResourceRequirements {
	rl := gpuResourceList(gpus)
	return v1.ResourceRequirements{Limits: rl, Requests: rl.DeepCopy()}
}

func gpuResourceList(gpus int64) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:                    resource.MustParse("100m"),
		v1.ResourceMemory:                 resource.MustParse("128Mi"),
		commonconstants.NvidiaGpuResource: *resource.NewQuantity(gpus, resource.DecimalSI),
	}
}

// GuaranteedGPUCPUPod builds (does not create) a Guaranteed-QoS pod requesting GPUs plus specific CPU
// and memory quantities. Used by restricted-policy tests that need every topology-aware resource's
// minimal width to agree: a width-2 GPU request must be paired with width-2 CPU and memory.
func GuaranteedGPUCPUPod(q *v2.Queue, gpus int64, cpu, memory resource.Quantity) *v1.Pod {
	return GuaranteedPod(q, v1.ResourceList{
		v1.ResourceCPU:                    cpu,
		v1.ResourceMemory:                 memory,
		commonconstants.NvidiaGpuResource: *resource.NewQuantity(gpus, resource.DecimalSI),
	})
}

// GuaranteedPod builds (does not create) a pod whose single container sets limits == requests for every
// resource in rl, yielding Guaranteed QoS.
func GuaranteedPod(q *v2.Queue, rl v1.ResourceList) *v1.Pod {
	requests := rl.DeepCopy()
	return rd.CreatePodObject(q, v1.ResourceRequirements{
		Limits:   rl,
		Requests: requests,
	})
}
