// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

func TestIsPodAllocated(t *testing.T) {
	tests := []struct {
		name           string
		pod            *v1.Pod
		expectedResult bool
	}{
		{
			"pending pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodPending,
				},
			},
			false,
		},
		{
			"pending scheduled pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodPending,
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodScheduled,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			true,
		},
		{
			"running pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodScheduled,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			true,
		},
		{
			"succeeded pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodSucceeded,
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodScheduled,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			false,
		},
		{
			"failed pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodFailed,
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodScheduled,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAllocatedPod(tt.pod)
			if tt.expectedResult != result {
				t.Errorf("isAllocatedPod() failed. test name: %s, expected: %v, actual: %v",
					tt.name, tt.expectedResult, result)
			}
		})
	}
}

func TestIsActivePod(t *testing.T) {
	tests := []struct {
		name           string
		pod            *v1.Pod
		expectedResult bool
	}{
		{
			"pending pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodPending,
				},
			},
			true,
		},
		{
			"pending scheduled pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodPending,
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodScheduled,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			true,
		},
		{
			"running pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodScheduled,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			true,
		},
		{
			"succeeded pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodSucceeded,
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodScheduled,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			false,
		},
		{
			"failed pod",
			&v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodFailed,
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodScheduled,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isActivePod(tt.pod)
			if tt.expectedResult != result {
				t.Errorf("isAllocatedPod() failed. test name: %s, expected: %v, actual: %v",
					tt.name, tt.expectedResult, result)
			}
		})
	}
}
