// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestGetSchedulingBackoffValue(t *testing.T) {
	assert.Equal(t, int32(3), GetSchedulingBackoffValue(pointer.Int32(3)))
	assert.Equal(t, int32(DefaultSchedulingBackoff), GetSchedulingBackoffValue(nil))
}

func TestGetMarkUnschedulableValue(t *testing.T) {
	assert.True(t, GetMarkUnschedulableValue(pointer.Bool(true)))
	assert.Equal(t, DefaultMarkUnschedulable, GetMarkUnschedulableValue(nil))
}

type SchedulingConditionTest struct {
	name              string
	podGroup          enginev2alpha2.PodGroup
	expectedCondition *enginev2alpha2.SchedulingCondition
}

func TestGetLastSchedulingCondition(t *testing.T) {
	for i, test := range []SchedulingConditionTest{
		{
			name: "No conditions",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{},
			},
			expectedCondition: nil,
		},
		{
			name: "Single condition",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{
					SchedulingConditions: []enginev2alpha2.SchedulingCondition{
						{
							NodePool: commonconstants.DefaultNodePoolName,
						},
					},
				},
			},
			expectedCondition: &enginev2alpha2.SchedulingCondition{
				NodePool: commonconstants.DefaultNodePoolName,
			},
		},
		{
			name: "Multiple conditions #1",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{
					SchedulingConditions: []enginev2alpha2.SchedulingCondition{
						{
							NodePool:     commonconstants.DefaultNodePoolName,
							TransitionID: "0",
						},
						{
							NodePool:     "other",
							TransitionID: "1",
						},
					},
				},
			},
			expectedCondition: &enginev2alpha2.SchedulingCondition{
				NodePool:     "other",
				TransitionID: "1",
			},
		},
		{
			name: "Multiple conditions #2",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{
					SchedulingConditions: []enginev2alpha2.SchedulingCondition{
						{
							NodePool:     commonconstants.DefaultNodePoolName,
							TransitionID: "2",
						},
						{
							NodePool:     "other",
							TransitionID: "1",
						},
					},
				},
			},
			expectedCondition: &enginev2alpha2.SchedulingCondition{
				NodePool:     commonconstants.DefaultNodePoolName,
				TransitionID: "2",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Testing #%d %s", i, test.name)
			assert.Equal(t, test.expectedCondition, GetLastSchedulingCondition(&test.podGroup))
		})
	}
}

type GetSchedulingConditionForNodePoolIndexTest struct {
	name                   string
	podGroup               enginev2alpha2.PodGroup
	nodePool               string
	expectedConditionIndex int
}

func TestGetSchedulingConditionForNodePoolIndex(t *testing.T) {
	for i, test := range []GetSchedulingConditionForNodePoolIndexTest{
		{
			name: "No conditions",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{},
			},
			nodePool:               commonconstants.DefaultNodePoolName,
			expectedConditionIndex: -1,
		},
		{
			name: "No conditions of nodepool",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{
					SchedulingConditions: []enginev2alpha2.SchedulingCondition{
						{
							NodePool: "other",
						},
					},
				},
			},
			nodePool:               commonconstants.DefaultNodePoolName,
			expectedConditionIndex: -1,
		},
		{
			name: "Multiple conditions #1",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{
					SchedulingConditions: []enginev2alpha2.SchedulingCondition{
						{
							NodePool: "nodepool1",
						},
						{
							NodePool: "nodepool2",
						},
						{
							NodePool: "nodepool3",
						},
					},
				},
			},
			nodePool:               "nodepool1",
			expectedConditionIndex: 0,
		},
		{
			name: "Multiple conditions #2",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{
					SchedulingConditions: []enginev2alpha2.SchedulingCondition{
						{
							NodePool: "nodepool1",
						},
						{
							NodePool: "nodepool2",
						},
						{
							NodePool: "nodepool3",
						},
					},
				},
			},
			nodePool:               "nodepool2",
			expectedConditionIndex: 1,
		},
		{
			name: "Multiple conditions #3",
			podGroup: enginev2alpha2.PodGroup{
				Status: enginev2alpha2.PodGroupStatus{
					SchedulingConditions: []enginev2alpha2.SchedulingCondition{
						{
							NodePool: "nodepool1",
						},
						{
							NodePool: "nodepool2",
						},
						{
							NodePool: "nodepool3",
						},
					},
				},
			},
			nodePool:               "nodepool3",
			expectedConditionIndex: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Testing #%d %s", i, test.name)
			assert.Equal(t, test.expectedConditionIndex, GetSchedulingConditionIndex(&test.podGroup, test.nodePool))
		})
	}
}
