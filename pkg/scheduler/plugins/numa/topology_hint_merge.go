// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"fmt"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

func topologyAffinity(mask pod_info.NUMAMask) bitmask.BitMask {
	affinity, _ := bitmask.NewBitMask(mask.Indices()...)
	return affinity
}

func numaMask(affinity bitmask.BitMask) pod_info.NUMAMask {
	if affinity == nil {
		return 0
	}
	mask, _ := pod_info.NewNUMAMask(affinity.GetBits())
	return mask
}

func mergeTopologyHints(topo *node_info.NumaTopology, providers []map[string][]topologymanager.TopologyHint) (topologymanager.TopologyHint, error) {
	numaInfo := &topologymanager.NUMAInfo{Nodes: allZoneIndices(topo)}
	options := topologymanager.PolicyOptions{}
	var policy topologymanager.Policy
	switch topo.Policy {
	case node_info.TopologyPolicyBestEffort:
		policy = topologymanager.NewBestEffortPolicy(numaInfo, options)
	case node_info.TopologyPolicyRestricted:
		policy = topologymanager.NewRestrictedPolicy(numaInfo, options)
	case node_info.TopologyPolicySingleNUMANode:
		policy = topologymanager.NewSingleNumaNodePolicy(numaInfo, options)
	default:
		return topologymanager.TopologyHint{}, fmt.Errorf("unsupported topology policy: %d", topo.Policy)
	}
	hint, admit := policy.Merge(klog.Background(), providers)
	if !admit {
		return hint, fmt.Errorf("%w: no preferred NUMA affinity for %s", errNotNumaAligned, policy.Name())
	}
	return hint, nil
}
