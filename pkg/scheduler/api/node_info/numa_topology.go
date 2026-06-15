// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"sort"
	"strconv"
	"strings"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
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
}

type NumaZone struct {
	ID          string
	Available   map[v1.ResourceName]resource.Quantity
	Allocatable map[v1.ResourceName]resource.Quantity
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

func (t *NumaTopology) Clone() *NumaTopology {
	if t == nil {
		return nil
	}
	zones := make([]*NumaZone, len(t.Zones))
	for i, zone := range t.Zones {
		available := make(map[v1.ResourceName]resource.Quantity, len(zone.Available))
		for r, qty := range zone.Available {
			available[r] = qty.DeepCopy()
		}
		allocatable := make(map[v1.ResourceName]resource.Quantity, len(zone.Allocatable))
		for r, qty := range zone.Allocatable {
			allocatable[r] = qty.DeepCopy()
		}
		zones[i] = &NumaZone{ID: zone.ID, Available: available, Allocatable: allocatable}
	}
	return &NumaTopology{
		Policy:    t.Policy,
		Scope:     t.Scope,
		Zones:     zones,
		Resources: t.Resources.Clone(),
	}
}

// BuildNumaTopology derives a node's NumaTopology from its NodeResourceTopology object, or
// returns nil when the object is absent or reports no NUMA-node zones.
func BuildNumaTopology(nrt *nrtv1alpha2.NodeResourceTopology) *NumaTopology {
	if nrt == nil {
		return nil
	}

	zones := buildZones(nrt.Zones)
	if len(zones) == 0 {
		return nil
	}

	policy, scope := parsePolicyAndScope(nrt)

	resources := sets.New[v1.ResourceName]()
	for _, zone := range zones {
		for name := range zone.Available {
			resources.Insert(name)
		}
	}

	return &NumaTopology{
		Policy:    policy,
		Scope:     scope,
		Zones:     zones,
		Resources: resources,
	}
}

// buildZones keeps only NUMA-node zones (NRT Zone.Type == "Node") and their
// per-resource Available and Allocatable quantities.
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
func buildZones(nrtZones nrtv1alpha2.ZoneList) []*NumaZone {
	var zones []*NumaZone
	for i := range nrtZones {
		nrtZone := &nrtZones[i]
		if nrtZone.Type != zoneTypeNode {
			continue
		}

		available := make(map[v1.ResourceName]resource.Quantity, len(nrtZone.Resources))
		allocatable := make(map[v1.ResourceName]resource.Quantity, len(nrtZone.Resources))
		for _, ri := range nrtZone.Resources {
			available[v1.ResourceName(ri.Name)] = ri.Available.DeepCopy()
			allocatable[v1.ResourceName(ri.Name)] = ri.Allocatable.DeepCopy()
		}

		zones = append(zones, &NumaZone{
			ID:          nrtZone.Name,
			Available:   available,
			Allocatable: allocatable,
		})
	}

	sortZones(zones)
	return zones
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
