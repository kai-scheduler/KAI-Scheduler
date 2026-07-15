// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology_info

import (
	"crypto/sha256"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

type TopologyConstraintInfo struct {
	PreferredLevel string
	RequiredLevel  string
	Topology       string

	schedulingConstraintsSignature common_info.SchedulingConstraintsSignature
}

// ResolveAliases rewrites the constraint's level strings to the canonical node label keys using the
// given alias->nodeLabel map for this constraint's topology. An alias resolves to its label; any
// other string (including a raw node label) passes through unchanged. Resolving at the source means
// every downstream consumer reads canonical labels and never has to resolve aliases itself.
func (tc *TopologyConstraintInfo) ResolveAliases(aliases map[string]string) {
	if tc == nil || len(aliases) == 0 {
		return
	}
	tc.RequiredLevel = resolveAlias(aliases, tc.RequiredLevel)
	tc.PreferredLevel = resolveAlias(aliases, tc.PreferredLevel)
	// Invalidate the cached signature so it is recomputed from the canonical levels.
	tc.schedulingConstraintsSignature = ""
}

func resolveAlias(aliases map[string]string, level string) string {
	if label, ok := aliases[level]; ok {
		return label
	}
	return level
}

func (tc *TopologyConstraintInfo) GetSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	if tc == nil {
		return ""
	}

	if tc.schedulingConstraintsSignature == "" {
		tc.schedulingConstraintsSignature = tc.generateSchedulingConstraintsSignature()
	}

	return tc.schedulingConstraintsSignature
}

func (tc *TopologyConstraintInfo) generateSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	hash := sha256.New()
	hash.Write([]byte(fmt.Sprintf("%s:%s:%s", tc.Topology, tc.RequiredLevel, tc.PreferredLevel)))

	return common_info.SchedulingConstraintsSignature(fmt.Sprintf("%x", hash.Sum(nil)))
}
