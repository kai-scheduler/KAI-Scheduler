// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
)

// ReleasingPods are the pods that were already terminating when the session opened. Pipeline
// predicates evaluate nodes as if these pods were gone, matching how their resources are counted
// as releasing. Victims evicted by this session are not included: they are released through the
// session's own eviction path.
type ReleasingPods struct {
	nodes sets.Set[string]
	uids  sets.Set[types.UID]
	// sharedDevices is set when a releasing pod holds a DRA device reachable from other nodes.
	sharedDevices bool
}

// ReleasingPods returns the terminating pods of the session, computed once on first use.
func (ssn *Session) ReleasingPods() *ReleasingPods {
	ssn.releasingPodsOnce.Do(func() {
		ssn.releasingPods = newReleasingPods(ssn)
	})
	return ssn.releasingPods
}

func newReleasingPods(ssn *Session) *ReleasingPods {
	releasing := &ReleasingPods{
		nodes: sets.New[string](),
		uids:  sets.New[types.UID](),
	}
	if ssn.ClusterInfo == nil {
		return releasing
	}
	for nodeName, node := range ssn.ClusterInfo.Nodes {
		for _, task := range node.PodInfos {
			if task.Status != pod_status.Releasing || task.IsVirtualStatus || task.Pod == nil {
				continue
			}
			releasing.nodes.Insert(nodeName)
			releasing.uids.Insert(task.Pod.UID)
		}
	}
	return releasing
}

func (r *ReleasingPods) IsEmpty() bool {
	return r == nil || r.uids.Len() == 0
}

func (r *ReleasingPods) Has(uid types.UID) bool {
	return r != nil && r.uids.Has(uid)
}

// SetSharedDevices records that a releasing pod holds a DRA device reachable from other nodes.
func (r *ReleasingPods) SetSharedDevices() {
	r.sharedDevices = true
}

// MayChangeFit reports whether removing the releasing pods can change a predicate result for the
// task on the node.
func (r *ReleasingPods) MayChangeFit(task *pod_info.PodInfo, nodeName string) bool {
	if r == nil {
		return false
	}
	if r.nodes.Has(nodeName) {
		return true
	}
	return r.sharedDevices && (len(task.Pod.Spec.ResourceClaims) > 0 || task.ExtendedResourceClaim != nil)
}
