// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import "testing"

func TestLessOrEqualWithTolerance(t *testing.T) {
	fractionalSum := 0.1
	fractionalSum += 0.2

	tests := []struct {
		name     string
		quantity float64
		limit    float64
		want     bool
	}{
		{
			name:     "fractional sum exactly matches limit",
			quantity: fractionalSum,
			limit:    0.3,
			want:     true,
		},
		{
			name:     "material excess remains over limit",
			quantity: 0.30000001,
			limit:    0.3,
			want:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := LessOrEqualWithTolerance(test.quantity, test.limit); got != test.want {
				t.Errorf("LessOrEqualWithTolerance() = %v, want %v", got, test.want)
			}
		})
	}
}
