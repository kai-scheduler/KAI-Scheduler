// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podWithResizeCondition(reason string, conditionGeneration, podGeneration int64) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Generation: podGeneration},
		Status: v1.PodStatus{
			Conditions: []v1.PodCondition{{
				Type:               v1.PodResizePending,
				Status:             v1.ConditionTrue,
				Reason:             reason,
				ObservedGeneration: conditionGeneration,
			}},
		},
	}
}

func TestIsResizeDeferred(t *testing.T) {
	tests := []struct {
		name     string
		pod      *v1.Pod
		expected bool
	}{
		{
			name:     "no pod",
			pod:      nil,
			expected: false,
		},
		{
			name:     "no conditions",
			pod:      &v1.Pod{},
			expected: false,
		},
		{
			name:     "deferred, current generation",
			pod:      podWithResizeCondition(v1.PodReasonDeferred, 3, 3),
			expected: true,
		},
		{
			name:     "deferred, no observed generation",
			pod:      podWithResizeCondition(v1.PodReasonDeferred, 0, 3),
			expected: true,
		},
		{
			name:     "deferred, stale generation",
			pod:      podWithResizeCondition(v1.PodReasonDeferred, 2, 3),
			expected: false,
		},
		{
			name:     "infeasible",
			pod:      podWithResizeCondition(v1.PodReasonInfeasible, 3, 3),
			expected: false,
		},
		{
			name: "deferred condition not true",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{{
						Type:   v1.PodResizePending,
						Status: v1.ConditionFalse,
						Reason: v1.PodReasonDeferred,
					}},
				},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pi := &PodInfo{Pod: test.pod}
			if actual := pi.IsResizeDeferred(); actual != test.expected {
				t.Errorf("IsResizeDeferred() = %v, expected %v", actual, test.expected)
			}
		})
	}
}

func TestDeferredResizeDelta(t *testing.T) {
	makePod := func(containers []v1.Container, statuses []v1.ContainerStatus) *v1.Pod {
		pod := podWithResizeCondition(v1.PodReasonDeferred, 0, 0)
		pod.Spec.Containers = containers
		pod.Status.ContainerStatuses = statuses
		return pod
	}

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected v1.ResourceList
	}{
		{
			name: "not deferred",
			pod: &v1.Pod{
				Spec: v1.PodSpec{Containers: []v1.Container{{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")},
					},
				}}},
			},
			expected: nil,
		},
		{
			name: "cpu upsize",
			pod: makePod(
				[]v1.Container{{
					Name: "main",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")},
					},
				}},
				[]v1.ContainerStatus{{
					Name: "main",
					Resources: &v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
					},
				}},
			),
			expected: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1500m")},
		},
		{
			name: "cpu upsize with memory downsize clamps memory",
			pod: makePod(
				[]v1.Container{{
					Name: "main",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				}},
				[]v1.ContainerStatus{{
					Name: "main",
					Resources: &v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("1"),
							v1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				}},
			),
			expected: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
		},
		{
			name: "sums over containers, falls back to allocated resources",
			pod: makePod(
				[]v1.Container{
					{
						Name: "a",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
						},
					},
					{
						Name: "b",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
						},
					},
				},
				[]v1.ContainerStatus{
					{
						Name: "a",
						Resources: &v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
						},
					},
					{
						Name:               "b",
						AllocatedResources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("250m")},
					},
				},
			),
			expected: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1250m")},
		},
		{
			name: "no growth returns nil",
			pod: makePod(
				[]v1.Container{{
					Name: "main",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					},
				}},
				[]v1.ContainerStatus{{
					Name: "main",
					Resources: &v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					},
				}},
			),
			expected: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pi := &PodInfo{Pod: test.pod}
			actual := pi.DeferredResizeDelta()
			if len(actual) != len(test.expected) {
				t.Fatalf("DeferredResizeDelta() = %v, expected %v", actual, test.expected)
			}
			for name, expectedQuantity := range test.expected {
				actualQuantity, found := actual[name]
				if !found || actualQuantity.Cmp(expectedQuantity) != 0 {
					t.Errorf("DeferredResizeDelta()[%s] = %v, expected %v", name, actualQuantity, expectedQuantity)
				}
			}
		})
	}
}
