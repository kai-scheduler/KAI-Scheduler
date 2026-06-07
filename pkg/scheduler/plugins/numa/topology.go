// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"sort"
	"strconv"
	"strings"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

// tmPolicy mirrors the kubelet Topology Manager policy reported per node via NRT.
// See https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-policies for details.
type tmPolicy int

const (
	policyNone tmPolicy = iota
	policyBestEffort
	policyRestricted
	policySingleNUMANode
)

// tmScope mirrors the kubelet Topology Manager scope: alignment is computed per
// container or once for the whole pod.
// See https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-scopes for details.
type tmScope int

const (
	scopeContainer tmScope = iota
	scopePod
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

// nodeTopology is the plugin's per-node working state, derived from a node's
// NodeResourceTopology object at OnSessionOpen.
type nodeTopology struct {
	policy tmPolicy
	scope  tmScope
	zones  []*numaZone
	// topologyAware is the set of resources this node reports per-zone, minus the
	// operator denylist. A resource constrains zone selection only if it appears here.
	topologyAware sets.Set[v1.ResourceName]
}

type numaZone struct {
	id        string
	available map[v1.ResourceName]resource.Quantity
}

// isModeledPolicy reports whether the plugin engages for a node with this policy.
// Only single-numa-node and restricted are supported at this point.
func (nt *nodeTopology) isModeledPolicy() bool {
	return nt.policy == policySingleNUMANode || nt.policy == policyRestricted
}

func buildNodeTopology(nrt *nrtv1alpha2.NodeResourceTopology, denylist sets.Set[v1.ResourceName]) *nodeTopology {
	if nrt == nil {
		return nil
	}

	zones := buildZones(nrt.Zones)
	if len(zones) == 0 {
		return nil
	}

	policy, scope := parsePolicyAndScope(nrt)

	topologyAware := sets.New[v1.ResourceName]()
	for _, zone := range zones {
		for name := range zone.available {
			if denylist.Has(name) {
				continue
			}
			topologyAware.Insert(name)
		}
	}

	return &nodeTopology{
		policy:        policy,
		scope:         scope,
		zones:         zones,
		topologyAware: topologyAware,
	}
}

// buildZones keeps only NUMA-node zones (NRT Zone.Type == "Node") and their
// per-resource Available quantities.
//
// We deliberately model only the NUMA-node level and drop every other zone type
// the NRT API can express (sockets, dies, ...). This is not a simplification we
// chose freely: the kubelet Topology Manager — the actual enforcer at pod
// admission — aligns purely at NUMA-node granularity, and the upstream
// scheduler-plugins NRT plugin filters identically. Modeling finer levels here
// would be useless, because the kubelet could not act on them. The richer zone
// tree lives in the NRT API/exporters but is unused end-to-end today.
//
// References:
//   - kubelet builds NUMA-node bitmasks only:
//     https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/cm/topologymanager/numa_info.go (NewNUMAInfo)
//   - upstream plugin skips zone.Type != "Node":
//     sigs.k8s.io/scheduler-plugins/pkg/noderesourcetopology/pluginhelpers.go (createNUMANodeList)
//   - rationale and history: docs/developer/designs/numa-topology/README.md
func buildZones(nrtZones nrtv1alpha2.ZoneList) []*numaZone {
	var zones []*numaZone
	for i := range nrtZones {
		nrtZone := &nrtZones[i]
		if nrtZone.Type != zoneTypeNode {
			continue
		}

		available := make(map[v1.ResourceName]resource.Quantity, len(nrtZone.Resources))
		for _, ri := range nrtZone.Resources {
			available[v1.ResourceName(ri.Name)] = ri.Available.DeepCopy()
		}

		zones = append(zones, &numaZone{
			id:        nrtZone.Name,
			available: available,
		})
	}

	sortZones(zones)
	return zones
}

// sortZones imposes a deterministic NUMA-node ordering on the zones so that a zone's index
// is stable and meaningful for the lifetime of the cycle, and so that single-numa-node
// selection prefers the lowest NUMA node (matching the kubelet) and the restricted greedy
// split has a stable tie-break. Zones are ordered by the numeric NUMA-node id parsed from
// their name (e.g. "node-10" after "node-2"); names without a numeric suffix sort after, by
// name. The durable identity remains the zone id; the index is an in-cycle convenience.
func sortZones(zones []*numaZone) {
	sort.Slice(zones, func(i, j int) bool {
		iNum, iOK := numaNodeID(zones[i].id)
		jNum, jOK := numaNodeID(zones[j].id)
		if iOK && jOK && iNum != jNum {
			return iNum < jNum
		}
		if iOK != jOK {
			return iOK // numbered zones sort before unnumbered ones
		}
		return zones[i].id < zones[j].id
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
func parsePolicyAndScope(nrt *nrtv1alpha2.NodeResourceTopology) (tmPolicy, tmScope) {
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

func policyFromAttribute(value string) tmPolicy {
	switch value {
	case policyValueSingleNUMANode:
		return policySingleNUMANode
	case policyValueRestricted:
		return policyRestricted
	case policyValueBestEffort:
		return policyBestEffort
	case policyValueNone:
		return policyNone
	default:
		return policyNone
	}
}

func scopeFromAttribute(value string) tmScope {
	switch value {
	case scopeValuePod:
		return scopePod
	case scopeValueContainer:
		return scopeContainer
	default:
		return scopeContainer
	}
}

// policyAndScopeFromLegacy maps the deprecated combined TopologyPolicies enum (which
// encodes both policy and scope) onto the plugin's policy/scope pair.
func policyAndScopeFromLegacy(policies []string) (tmPolicy, tmScope) {
	if len(policies) == 0 {
		return policyNone, scopeContainer
	}

	switch nrtv1alpha2.TopologyManagerPolicy(policies[0]) {
	case nrtv1alpha2.SingleNUMANodePodLevel:
		return policySingleNUMANode, scopePod
	case nrtv1alpha2.SingleNUMANodeContainerLevel:
		return policySingleNUMANode, scopeContainer
	case nrtv1alpha2.RestrictedPodLevel:
		return policyRestricted, scopePod
	case nrtv1alpha2.Restricted, nrtv1alpha2.RestrictedContainerLevel:
		return policyRestricted, scopeContainer
	case nrtv1alpha2.BestEffortPodLevel:
		return policyBestEffort, scopePod
	case nrtv1alpha2.BestEffort, nrtv1alpha2.BestEffortContainerLevel:
		return policyBestEffort, scopeContainer
	default:
		return policyNone, scopeContainer
	}
}
