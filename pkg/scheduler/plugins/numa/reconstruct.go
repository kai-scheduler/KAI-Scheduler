// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// reconstructNodeAvailable discards each modeled node's NRT-reported per-zone Available and
// recomputes it as Allocatable minus the observed placements of the pods currently consuming the
// node. NRT Available lags across cycles (the exporter republishes on a delay, in both directions —
// missing a just-bound pod or still counting a just-deleted one); Allocatable is static and the pod
// set is read from the live snapshot, so the reconstructed Available reflects the node's real free
// capacity with no lag. Each pod's zone comes from its observed (agent-published) record; a pod with
// no observed record contributes nothing — never guess a zone. Gated by the reconstructAvailable
// flag, which the operator sets when the placement agent (the observed-placement source) is deployed.
func (pp *numaPlugin) reconstructNodeAvailable(ssn *framework.Session) {
	for _, node := range ssn.ClusterInfo.Nodes {
		topo := node.NumaTopology
		if topo == nil || !isModeledPolicy(topo.Policy) {
			continue
		}
		resetAvailableToAllocatable(topo)
		for _, task := range node.PodInfos {
			if !pod_status.IsActiveAllocatedStatus(task.Status) {
				continue
			}
			numaAllocate(topo, placementFromRecord(observedRecord(task.Pod), topo))
		}
	}
}

// resetAvailableToAllocatable sets every zone's Available to a fresh copy of its static Allocatable,
// the starting point from which reconstructNodeAvailable subtracts pod placements.
func resetAvailableToAllocatable(topo *node_info.NumaTopology) {
	for _, zone := range topo.Zones {
		available := make(map[v1.ResourceName]resource.Quantity, len(zone.Allocatable))
		for r, qty := range zone.Allocatable {
			available[r] = qty.DeepCopy()
		}
		zone.Available = available
	}
}
