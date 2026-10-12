// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestIsAllocated(t *testing.T) {
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
			result := IsAllocated(tt.pod)
			if tt.expectedResult != result {
				t.Errorf("IsAllocated() failed. test name: %s, expected: %v, actual: %v",
					tt.name, tt.expectedResult, result)
			}
		})
	}
}

func TestIsGuaranteed(t *testing.T) {
	guaranteedResources := v1.ResourceList{v1.ResourceCPU: resource.MustParse("1"), v1.ResourceMemory: resource.MustParse("1Gi")}
	guaranteed := &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Requests: guaranteedResources, Limits: guaranteedResources.DeepCopy()}}}}}
	mixed := guaranteed.DeepCopy()
	mixed.Spec.Containers = append(mixed.Spec.Containers, v1.Container{})
	missingMemory := guaranteed.DeepCopy()
	delete(missingMemory.Spec.Containers[0].Resources.Requests, v1.ResourceMemory)
	persistedGuaranteed := mixed.DeepCopy()
	persistedGuaranteed.Status.QOSClass = v1.PodQOSGuaranteed
	persistedBurstable := guaranteed.DeepCopy()
	persistedBurstable.Status.QOSClass = v1.PodQOSBurstable
	tests := []struct {
		name string
		pod  *v1.Pod
		want bool
	}{
		{name: "nil pod"},
		{name: "best effort", pod: &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{}}}}},
		{name: "spec derived guaranteed", pod: guaranteed, want: true},
		{name: "mixed containers", pod: mixed},
		{name: "missing memory request", pod: missingMemory},
		{name: "persisted guaranteed overrides spec", pod: persistedGuaranteed, want: true},
		{name: "persisted burstable overrides spec", pod: persistedBurstable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsGuaranteed(test.pod); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
