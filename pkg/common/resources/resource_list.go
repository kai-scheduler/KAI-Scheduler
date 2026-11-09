// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"strings"

	v1 "k8s.io/api/core/v1"
)

// IsMemoryResource includes ordinary memory and all hugepage sizes managed by Memory Manager.
func IsMemoryResource(name v1.ResourceName) bool {
	return name == v1.ResourceMemory || strings.HasPrefix(string(name), v1.ResourceHugePagesPrefix)
}
