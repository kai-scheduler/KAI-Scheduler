// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package constants

const (
	PodGroupNamePrefix     = "pg"
	ProjectLabelKey        = "project"
	PriorityLabelKey       = "priorityClassName"
	PreemptibilityLabelKey = "kai.scheduler/preemptibility"
	UserLabelKey           = "user"

	BuildPriorityClass     = "build"
	TrainPriorityClass     = "train"
	InferencePriorityClass = "inference"

	DefaultPrioritiesConfigMapTypesKey = "types"

	DefaultQueueName = "default-queue"

	TopologyKey                   = "kai.scheduler/topology"
	TopologyRequiredPlacementKey  = "kai.scheduler/topology-required-placement"
	TopologyPreferredPlacementKey = "kai.scheduler/topology-preferred-placement"

	SegmentSizeKey                       = "kai.scheduler/segment-size"
	SegmentTopologyRequiredPlacementKey  = "kai.scheduler/segment-topology-required-placement"
	SegmentTopologyPreferredPlacementKey = "kai.scheduler/segment-topology-preferred-placement"
)
