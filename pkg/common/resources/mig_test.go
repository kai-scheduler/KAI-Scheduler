// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	nonExistingResourceName = "nvidia.com/mig-1g.5gb111"
	migResourcePrefix       = "nvidia.com/mig-"
)

func TestExtractGpuAndMemoryFromMigDeviceName(t *testing.T) {
	gpu, memory, err := ExtractGpuAndMemoryFromMigResourceName(fmt.Sprintf("%s%s", migResourcePrefix, "1g.5gb"))
	assert.Equal(t, 1, gpu)
	assert.Equal(t, 5, memory)
	assert.Nil(t, err)
}

func TestExtractGpuAndMemoryFromMigDeviceNameError(t *testing.T) {
	gpu, memory, err := ExtractGpuAndMemoryFromMigResourceName(nonExistingResourceName)
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Sprintf("failed to extract gpu/memory from %s", nonExistingResourceName), err.Error())
	assert.Equal(t, -1, gpu)
	assert.Equal(t, -1, memory)
}
