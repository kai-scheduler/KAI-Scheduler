// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_status

import (
	"testing"
)

// aliveStatuses           = Allocated | Pipelined | Binding | Bound | Running | Pending | Gated
func TestIsAliveStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   PodStatus
		expected bool
	}{
		{
			name:     "Allocated",
			status:   Allocated,
			expected: true,
		},
		{
			name:     "Pipelined",
			status:   Pipelined,
			expected: true,
		},
		{
			name:     "Binding",
			status:   Binding,
			expected: true,
		},
		{
			name:     "Bound",
			status:   Bound,
			expected: true,
		},
		{
			name:     "Running",
			status:   Running,
			expected: true,
		},
		{
			name:     "Pending",
			status:   Pending,
			expected: true,
		},
		{
			name:     "Gated",
			status:   Gated,
			expected: true,
		},
		{
			name:     "Succeeded",
			status:   Succeeded,
			expected: false,
		},
		{
			name:     "Failed",
			status:   Failed,
			expected: false,
		},
		{
			name:     "Deleted",
			status:   Deleted,
			expected: false,
		},
		{
			name:     "Unknown",
			status:   Unknown,
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := IsAliveStatus(test.status)
			if result != test.expected {
				t.Errorf("IsAliveStatus(%v) = %v, expected %v", test.status, result, test.expected)
			}
		})
	}
}
