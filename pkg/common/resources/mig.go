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

// migResourcePattern captures the GPU slice count and memory size of a MIG resource name.
var migResourcePattern = regexp.MustCompile(`^nvidia.com/mig-(\d+)g\.(\d+)gb$`)

// IsMigResource reports whether the given resource name is an NVIDIA MIG device
// resource (e.g. "nvidia.com/mig-3g.20gb").
func IsMigResource(name string) bool {
	return strings.HasPrefix(name, constants.NvidiaMigResourcePrefix)
}

// migGpuSlices returns the GPU slice count encoded in a MIG resource name (e.g. 3 for
// "nvidia.com/mig-3g.20gb"). Both numeric fields must parse, matching the scheduler's MIG name parsing, so
// the queue webhook does not depend on scheduler packages.
func migGpuSlices(name string) (int, error) {
	matches := migResourcePattern.FindStringSubmatch(name)
	if len(matches) < 3 {
		return 0, fmt.Errorf("not a MIG resource name: %v", name)
	}
	slices, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}
	if _, err := strconv.Atoi(matches[2]); err != nil {
		return 0, err
	}
	return slices, nil
}
