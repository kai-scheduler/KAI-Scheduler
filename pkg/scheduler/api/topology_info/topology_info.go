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
