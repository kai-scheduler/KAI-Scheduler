// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"sort"
	"strconv"
	"strings"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// TopologyManagerPolicy mirrors the kubelet Topology Manager policy reported per node via NRT.
// See https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-policies for details.
type TopologyManagerPolicy int

const (
	TopologyPolicyNone TopologyManagerPolicy = iota
	TopologyPolicyBestEffort
	TopologyPolicyRestricted
	TopologyPolicySingleNUMANode
)

// TopologyManagerScope mirrors the kubelet Topology Manager scope: alignment is computed per
// container or once for the whole pod.
// See https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-scopes for details.
type TopologyManagerScope int

const (
	TopologyScopeContainer TopologyManagerScope = iota
	TopologyScopePod
)

// zoneTypeNode is the NRT Zone.Type for a NUMA node; see buildZones for why only
// this zone type is modeled.
const zoneTypeNode = "Node"

const (
	attrTopologyManagerPolicy = "topologyManagerPolicy"
	attrTopologyManagerScope  = "topologyManagerScope"
)

const (
	policyValueNone           = "none"
	policyValueBestEffort     = "best-effort"
	policyValueRestricted     = "restricted"
	policyValueSingleNUMANode = "single-numa-node"

	scopeValueContainer = "container"
	scopeValuePod       = "pod"
)

type NumaTopology struct {
	Policy    TopologyManagerPolicy
	Scope     TopologyManagerScope
	Zones     []*NumaZone
	Resources sets.Set[v1.ResourceName]

	VectorMap *resource_info.ResourceVectorMap
	// AwareIndices holds the VectorMap indices of the zone-reported resources — the only ones the
	// kubelet aligns. Sorted ascending.
	AwareIndices []int
	// AwareNames maps each aware index to the NRT-reported resource name, so placement amounts
	// materialize under the durable name (e.g. nvidia.com/gpu) rather than the map's normalized "gpu".
	AwareNames map[int]v1.ResourceName
	// AllocatablePrefix holds, per aware index, the descending-sorted prefix sums of the zones'
	// Allocatable: [idx][k] is the sum of the k+1 largest zone allocatables. Precomputed (Allocatable
	// is static) so the restricted policy's preferred-width lookup is a prefix scan, no per-call sort.
	AllocatablePrefix map[int][]float64
}

// NumaZone describes a single NUMA zone's per-resource Available and Allocatable amounts in a ResourceVector format.
type NumaZone struct {
	ID          string
	Available   resource_info.ResourceVector
	Allocatable resource_info.ResourceVector
}

// NumaZoneSpec describes a single NUMA zone's per-resource Available and Allocatable amounts in a non-vector (ResourceList) format.
type NumaZoneSpec struct {
	ID          string
	Available   v1.ResourceList
	Allocatable v1.ResourceList
}

// ZoneIndexByID returns the index of the zone with the given durable id, or false if no zone has
// it. Used to translate a persisted (id-based) placement back to the internal index representation.
func (t *NumaTopology) ZoneIndexByID(id string) (int, bool) {
	for i, zone := range t.Zones {
		if zone.ID == id {
			return i, true
		}
	}
	return -1, false
}

// ZoneID returns the durable id of the zone at the given index, or false if out of range. Used to
// translate the internal index representation to the persisted (id-based) placement at bind.
func (t *NumaTopology) ZoneID(index int) (string, bool) {
	if index < 0 || index >= len(t.Zones) {
		return "", false
	}
	return t.Zones[index].ID, true
}

func (t *NumaTopology) Clone() *NumaTopology {
	if t == nil {
		return nil
	}
	zones := make([]*NumaZone, len(t.Zones))
	for i, zone := range t.Zones {
		zones[i] = &NumaZone{
			ID:          zone.ID,
			Available:   zone.Available.Clone(),
			Allocatable: zone.Allocatable.Clone(),
		}
	}
	return &NumaTopology{
		Policy:            t.Policy,
		Scope:             t.Scope,
		Zones:             zones,
		Resources:         t.Resources.Clone(),
		VectorMap:         t.VectorMap,         // shared, read-only during scoring
		AwareIndices:      t.AwareIndices,      // shared, read-only
		AwareNames:        t.AwareNames,        // shared, read-only
		AllocatablePrefix: t.AllocatablePrefix, // shared, read-only (Allocatable is static)
	}
}

// BuildNumaTopology derives a node's NumaTopology from its NodeResourceTopology object, or
// returns nil when the object is absent or reports no NUMA-node zones. The zone vectors and the
// aware-index projection are built against vectorMap, the cluster-shared resource index map;
// zone-reported resources absent from the map are ignored (see newNumaTopology).
func BuildNumaTopology(nrt *nrtv1alpha2.NodeResourceTopology, vectorMap *resource_info.ResourceVectorMap) *NumaTopology {
	if nrt == nil {
		return nil
	}

	specs := zoneSpecs(nrt.Zones)
	if len(specs) == 0 {
		return nil
	}

	policy, scope := parsePolicyAndScope(nrt)
	return newNumaTopology(policy, scope, vectorMap, specs)
}

// newNumaTopology vectorizes the zone specs against the shared map and records the aware-resource
// indices. A zone-reported resource absent from the shared map is ignored: the map is seeded from
// the cluster's node resources, so its absence means the node does not actually expose that resource.
// Zones are ordered by ascending NUMA-node id (see sortZones).
func newNumaTopology(
	policy TopologyManagerPolicy, scope TopologyManagerScope,
	vectorMap *resource_info.ResourceVectorMap, specs []NumaZoneSpec,
) *NumaTopology {
	resources := sets.New[v1.ResourceName]()
	for _, spec := range specs {
		for name := range spec.Allocatable {
			if vectorMap.GetIndex(name) >= 0 {
				resources.Insert(name)
			}
		}
		for name := range spec.Available {
			if vectorMap.GetIndex(name) >= 0 {
				resources.Insert(name)
			}
		}
	}

	zones := make([]*NumaZone, len(specs))
	for i, spec := range specs {
		zones[i] = &NumaZone{
			ID:          spec.ID,
			Available:   resource_info.NewResourceVectorFromResourceList(spec.Available, vectorMap),
			Allocatable: resource_info.NewResourceVectorFromResourceList(spec.Allocatable, vectorMap),
		}
	}
	sortZones(zones)

	indices, names := awareIndices(resources, vectorMap)
	return &NumaTopology{
		Policy:            policy,
		Scope:             scope,
		Zones:             zones,
		Resources:         resources,
		VectorMap:         vectorMap,
		AwareIndices:      indices,
		AwareNames:        names,
		AllocatablePrefix: allocatablePrefixSums(zones, indices),
	}
}

// allocatablePrefixSums computes, per aware index, the descending-sorted prefix sums of the zones'
// Allocatable for that resource (see NumaTopology.AllocatablePrefix).
func allocatablePrefixSums(zones []*NumaZone, indices []int) map[int][]float64 {
	prefix := make(map[int][]float64, len(indices))
	for _, idx := range indices {
		vals := make([]float64, len(zones))
		for z, zone := range zones {
			vals[z] = zone.Allocatable.Get(idx)
		}
		sort.Sort(sort.Reverse(sort.Float64Slice(vals)))
		acc := 0.0
		for k := range vals {
			acc += vals[k]
			vals[k] = acc
		}
		prefix[idx] = vals
	}
	return prefix
}

// awareIndices returns the sorted, deduplicated VectorMap indices of the given resource names, and
// the reported name for each index. Distinct names that normalize to the same index (e.g. GPU
// vendor variants) collapse to one entry; the reported name preserved is the last seen.
func awareIndices(resources sets.Set[v1.ResourceName], vectorMap *resource_info.ResourceVectorMap) ([]int, map[int]v1.ResourceName) {
	names := map[int]v1.ResourceName{}
	for name := range resources {
		if idx := vectorMap.GetIndex(name); idx >= 0 {
			names[idx] = name
		}
	}
	out := make([]int, 0, len(names))
	for idx := range names {
		out = append(out, idx)
	}
	sort.Ints(out)
	return out, names
}

// zoneSpecs keeps only NUMA-node zones (NRT Zone.Type == "Node") and their per-resource Available
// and Allocatable quantities.
//
// We deliberately model only the NUMA-node level and drop every other zone type
// the NRT API can express (sockets, dies, ...). This is because the kubelet
// Topology Manager aligns purely at NUMA-node granularity.
//
// References:
//   - kubelet builds NUMA-node bitmasks only:
//     https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/cm/topologymanager/numa_info.go (NewNUMAInfo)
//   - upstream plugin skips zone.Type != "Node":
//     sigs.k8s.io/scheduler-plugins/pkg/noderesourcetopology/pluginhelpers.go (createNUMANodeList)
//   - rationale and history: docs/developer/designs/numa-topology/README.md
func zoneSpecs(nrtZones nrtv1alpha2.ZoneList) []NumaZoneSpec {
	var specs []NumaZoneSpec
	for i := range nrtZones {
		nrtZone := &nrtZones[i]
		if nrtZone.Type != zoneTypeNode {
			continue
		}

		available := make(v1.ResourceList, len(nrtZone.Resources))
		allocatable := make(v1.ResourceList, len(nrtZone.Resources))
		for _, ri := range nrtZone.Resources {
			available[v1.ResourceName(ri.Name)] = ri.Available
			allocatable[v1.ResourceName(ri.Name)] = ri.Allocatable
		}

		specs = append(specs, NumaZoneSpec{
			ID:          nrtZone.Name,
			Available:   available,
			Allocatable: allocatable,
		})
	}
	return specs
}

// sortZones orders the zones by ascending NUMA-node id so the evaluators' zone/mask selection
// matches the kubelet's allocation preference, independent of the order the exporter happened to
// list zones in the NRT object (array position is not guaranteed to be the NUMA-node id — the id
// is in the zone name). Among equally-preferred (minimal-width) hints the kubelet picks the
// numerically-lowest NUMA-node affinity: bitmask.IsNarrowerThan breaks count ties via IsLessThan,
// so mask {0} beats {1} and {0,1} beats {0,2}. Sorting ascending makes singleNUMAEvaluator's
// lowest-fitting-zone and restrictedEvaluator's lowest-satisfying-mask reproduce that choice, so
// the zone we predict and charge is the one the kubelet would use.
//
// Reference: k8s.io/kubernetes/pkg/kubelet/cm/topologymanager — bitmask/bitmask.go
// (IsNarrowerThan -> IsLessThan) and policy.go (compare / narrowest-hint selection).
func sortZones(zones []*NumaZone) {
	sort.Slice(zones, func(i, j int) bool {
		iNum, iOK := numaNodeID(zones[i].ID)
		jNum, jOK := numaNodeID(zones[j].ID)
		if iOK && jOK && iNum != jNum {
			return iNum < jNum
		}
		if iOK != jOK {
			return iOK // numbered zones sort before unnumbered ones
		}
		return zones[i].ID < zones[j].ID
	})
}

// numaNodeID extracts the trailing integer of an NRT zone name (the convention exporters
// use, e.g. "node-3"), returning false when the name has no numeric suffix.
func numaNodeID(name string) (int, bool) {
	idx := strings.LastIndexFunc(name, func(r rune) bool { return r < '0' || r > '9' })
	suffix := name[idx+1:]
	if suffix == "" {
		return 0, false
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 0, false
	}
	return n, true
}

// parsePolicyAndScope reads the Topology Manager policy and scope from the NRT
// top-level attributes, falling back to the deprecated TopologyPolicies field for
// exporters that have not migrated to attributes. The default scope is container,
// matching the kubelet.
func parsePolicyAndScope(nrt *nrtv1alpha2.NodeResourceTopology) (TopologyManagerPolicy, TopologyManagerScope) {
	policyAttr, scopeAttr := "", ""
	for _, attr := range nrt.Attributes {
		switch attr.Name {
		case attrTopologyManagerPolicy:
			policyAttr = attr.Value
		case attrTopologyManagerScope:
			scopeAttr = attr.Value
		}
	}

	if policyAttr != "" {
		return policyFromAttribute(policyAttr), scopeFromAttribute(scopeAttr)
	}

	return policyAndScopeFromLegacy(nrt.TopologyPolicies)
}

func policyFromAttribute(value string) TopologyManagerPolicy {
	switch value {
	case policyValueSingleNUMANode:
		return TopologyPolicySingleNUMANode
	case policyValueRestricted:
		return TopologyPolicyRestricted
	case policyValueBestEffort:
		return TopologyPolicyBestEffort
	case policyValueNone:
		return TopologyPolicyNone
	default:
		return TopologyPolicyNone
	}
}

func scopeFromAttribute(value string) TopologyManagerScope {
	switch value {
	case scopeValuePod:
		return TopologyScopePod
	case scopeValueContainer:
		return TopologyScopeContainer
	default:
		return TopologyScopeContainer
	}
}

// policyAndScopeFromLegacy maps the deprecated combined TopologyPolicies enum (which
// encodes both policy and scope) onto the policy/scope pair.
func policyAndScopeFromLegacy(policies []string) (TopologyManagerPolicy, TopologyManagerScope) {
	if len(policies) == 0 {
		return TopologyPolicyNone, TopologyScopeContainer
	}

	switch nrtv1alpha2.TopologyManagerPolicy(policies[0]) {
	case nrtv1alpha2.SingleNUMANodePodLevel:
		return TopologyPolicySingleNUMANode, TopologyScopePod
	case nrtv1alpha2.SingleNUMANodeContainerLevel:
		return TopologyPolicySingleNUMANode, TopologyScopeContainer
	case nrtv1alpha2.RestrictedPodLevel:
		return TopologyPolicyRestricted, TopologyScopePod
	case nrtv1alpha2.Restricted, nrtv1alpha2.RestrictedContainerLevel:
		return TopologyPolicyRestricted, TopologyScopeContainer
	case nrtv1alpha2.BestEffortPodLevel:
		return TopologyPolicyBestEffort, TopologyScopePod
	case nrtv1alpha2.BestEffort, nrtv1alpha2.BestEffortContainerLevel:
		return TopologyPolicyBestEffort, TopologyScopeContainer
	default:
		return TopologyPolicyNone, TopologyScopeContainer
	}
}
