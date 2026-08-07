// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package memorylimit

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRatio(t *testing.T) {
	for _, test := range []struct {
		name, value string
		valid       bool
		expected    float64
	}{
		{name: "default", valid: true, expected: .9},
		{name: "override", value: ".85", valid: true, expected: .85},
		{name: "zero", value: "0"},
		{name: "over one", value: "1.1"},
		{name: "not a number", value: "NaN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.value != "" {
				t.Setenv(RatioEnv, test.value)
			}
			ratio, err := resolveRatio(.9)
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expected, ratio)
		})
	}
}

func TestCalculate(t *testing.T) {
	assert.Equal(t, int64(5*1024*1024*1024), calculate(7*1024*1024*1024, 5.0/7.0))
	assert.Equal(t, int64(math.MaxInt64), calculate(math.MaxUint64, 1))
}
