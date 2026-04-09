// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package eviction_info

import (
	"k8s.io/apimachinery/pkg/types"
)

type EvictionMetadata struct {
	EvictionGangSize int
	Action           string
	Preemptor        *types.NamespacedName
	// EvictionStrategy is "suspend" or "delete" (default). When "suspend",
	// the commit phase patches spec.suspend=true on the workload owner
	// instead of deleting the pod.
	EvictionStrategy string
}
