// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package minmember

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		expectedError bool
		expected      int32
	}{
		{name: "single member", value: "1", expected: 1},
		{name: "multiple members", value: "12", expected: 12},
		{name: "zero", value: "0", expectedError: true},
		{name: "negative", value: "-1", expectedError: true},
		{name: "not a number", value: "all", expectedError: true},
		{name: "empty", value: "", expectedError: true},
		{name: "float", value: "1.5", expectedError: true},
		{name: "overflows int32", value: "2147483648", expectedError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			minMember, err := Parse(test.value, "JobSet")
			if test.expectedError {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), constants.MinMemberOverrideKey)
				assert.Contains(t, err.Error(), "JobSet")
				return
			}
			assert.Nil(t, err)
			assert.Equal(t, test.expected, minMember)
		})
	}
}

func TestFromAnnotations(t *testing.T) {
	tests := []struct {
		name          string
		annotations   map[string]string
		fallback      int32
		expectedError bool
		expected      int32
	}{
		{
			name:        "annotation absent",
			annotations: map[string]string{"other": "3"},
			fallback:    5,
			expected:    5,
		},
		{
			name:        "annotation overrides the fallback",
			annotations: map[string]string{constants.MinMemberOverrideKey: "3"},
			fallback:    5,
			expected:    3,
		},
		{
			name:          "invalid annotation",
			annotations:   map[string]string{constants.MinMemberOverrideKey: "0"},
			fallback:      5,
			expectedError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			obj := &metav1.ObjectMeta{Annotations: test.annotations}

			minMember, err := FromAnnotations(obj, "Deployment", test.fallback)
			if test.expectedError {
				assert.NotNil(t, err)
				return
			}
			assert.Nil(t, err)
			assert.Equal(t, test.expected, minMember)
		})
	}
}
