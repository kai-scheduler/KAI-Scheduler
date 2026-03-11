// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package csidriver_info

import "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"

type CSIDriverInfo struct {
	ID              common_info.CSIDriverID
	CapacityEnabled bool
}
