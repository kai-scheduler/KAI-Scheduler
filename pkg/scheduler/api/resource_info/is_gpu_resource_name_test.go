// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func TestIsGpuResourceName(t *testing.T) {
	assert.True(t, IsGpuResourceName("nvidia.com/gpu"))
	assert.True(t, IsGpuResourceName("amd.com/gpu"))
	assert.False(t, IsGpuResourceName(v1.ResourceCPU))
	assert.False(t, IsGpuResourceName("example.com/foo"))
	assert.False(t, IsGpuResourceName("nvidia.com/mig-1g.5gb"))
}
