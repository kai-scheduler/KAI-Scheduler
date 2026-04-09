// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package eviction_info

import (
	"k8s.io/apimachinery/pkg/types"
)

const (
	// EvictionStrategySuspend patches spec.suspend=true on the workload owner.
	EvictionStrategySuspend = "suspend"

	// EvictionStrategyDelete deletes pods directly (default).
	EvictionStrategyDelete = "delete"
)

type EvictionMetadata struct {
	EvictionGangSize int
	Action           string
	Preemptor        *types.NamespacedName
	EvictionStrategy string
}
