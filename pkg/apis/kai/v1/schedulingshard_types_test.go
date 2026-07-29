// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSchedulingShardDefaultsGoMemLimitRatio(t *testing.T) {
	shard := &SchedulingShardSpec{}
	shard.SetDefaultsWhereNeeded()

	assert.Equal(t, 0.9, *shard.GoMemLimitRatio)
}
