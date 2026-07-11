// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

// IsMigResource reports whether the given resource name is an NVIDIA MIG device
// resource (e.g. "nvidia.com/mig-3g.20gb").
func IsMigResource(name string) bool {
	return strings.HasPrefix(name, constants.NvidiaMigResourcePrefix)
}

// ExtractGpuAndMemoryFromMigResourceName Returns memory in GB
func ExtractGpuAndMemoryFromMigResourceName(migResourceName string) (int, int, error) {
	return extractGpuAndMemoryFrom(`^nvidia.com/mig-(\d+)g\.(\d+)gb$`, migResourceName)
}

func extractGpuAndMemoryFrom(searchInPattern string, migString string) (int, int, error) {
	matches := regexp.MustCompile(searchInPattern).FindStringSubmatch(migString)
	if len(matches) < 3 {
		return -1, -1, fmt.Errorf("failed to extract gpu/memory from %v", migString)
	}

	gpu, err := strconv.Atoi(matches[1])
	if err != nil {
		return -1, -1, fmt.Errorf("failed parsing %v to integer", matches[2])
	}
	mem, err := strconv.Atoi(matches[2])
	if err != nil {
		return -1, -1, fmt.Errorf("failed parsing %v to integer", matches[2])
	}

	return gpu, mem, nil
}
