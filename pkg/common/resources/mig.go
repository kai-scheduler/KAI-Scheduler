// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

// IsMigResource reports whether the given resource name is an NVIDIA MIG device
// resource (e.g. "nvidia.com/mig-3g.20gb").
func IsMigResource(name string) bool {
	return strings.HasPrefix(name, constants.NvidiaMigResourcePrefix)
}
