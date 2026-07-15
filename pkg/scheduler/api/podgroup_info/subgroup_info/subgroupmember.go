// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"

// SubGroupMember is implemented by both SubGroupSet and PodSet, which are the two types of direct children a SubGroupSet can have.
type SubGroupMember interface {
	GetName() string
	GetTopologyConstraint() *topology_info.TopologyConstraintInfo
	GetParent() *SubGroupSet
	IsReadyForScheduling() bool
	GetMinMembersToSatisfy() int
	GetNumActiveAllocatedMembers() int
}
