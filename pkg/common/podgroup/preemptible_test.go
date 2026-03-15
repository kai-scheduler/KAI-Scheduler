// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_test

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	pg "github.com/kai-scheduler/KAI-scheduler/pkg/common/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/stretchr/testify/assert"
)

func TestCalculatePreemptibility(t *testing.T) {
	tests := []struct {
		name           string
		preemptibility v2alpha2.Preemptibility
		priority       int32
		expectedResult v2alpha2.Preemptibility
		expectedError  bool
	}{
		{
			name:           "explicitly preemptible",
			preemptibility: v2alpha2.Preemptible,
			priority:       1000,
			expectedResult: v2alpha2.Preemptible,
			expectedError:  false,
		},
		{
			name:           "explicitly non-preemptible",
			preemptibility: v2alpha2.NonPreemptible,
			priority:       50,
			expectedResult: v2alpha2.NonPreemptible,
			expectedError:  false,
		},
		{
			name:           "unspecified with high priority (non-preemptible)",
			preemptibility: "",
			priority:       1000,
			expectedResult: v2alpha2.NonPreemptible,
			expectedError:  false,
		},
		{
			name:           "unspecified with low priority (preemptible)",
			preemptibility: "",
			priority:       50,
			expectedResult: v2alpha2.Preemptible,
			expectedError:  false,
		},
		{
			name:           "unspecified with priority equal to build number (non-preemptible)",
			preemptibility: "",
			priority:       constants.PriorityBuildNumber,
			expectedResult: v2alpha2.NonPreemptible,
			expectedError:  false,
		},
		{
			name:           "unspecified with priority just below build number (preemptible)",
			preemptibility: "",
			priority:       constants.PriorityBuildNumber - 1,
			expectedResult: v2alpha2.Preemptible,
			expectedError:  false,
		},
		{
			name:           "unspecified with priority just above build number (non-preemptible)",
			preemptibility: "",
			priority:       constants.PriorityBuildNumber + 1,
			expectedResult: v2alpha2.NonPreemptible,
			expectedError:  false,
		},
		{
			name:           "unspecified with zero priority (preemptible)",
			preemptibility: "",
			priority:       0,
			expectedResult: v2alpha2.Preemptible,
			expectedError:  false,
		},
		{
			name:           "unspecified with negative priority (preemptible)",
			preemptibility: "",
			priority:       -100,
			expectedResult: v2alpha2.Preemptible,
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pg.CalculatePreemptibility(tt.preemptibility, tt.priority)

			if tt.expectedError {
				assert.True(t, result == tt.expectedResult)
			} else {
				assert.Equal(t, tt.expectedResult, result)
			}
		})
	}
}
