// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func TestIsTerminalPod(t *testing.T) {
	tests := []struct {
		name           string
		pod            *v1.Pod
		expectedResult bool
	}{
		{
			"pending pod",
			&v1.Pod{Status: v1.PodStatus{Phase: v1.PodPending}},
			false,
		},
		{
			"running pod",
			&v1.Pod{Status: v1.PodStatus{Phase: v1.PodRunning}},
			false,
		},
		{
			"succeeded pod",
			&v1.Pod{Status: v1.PodStatus{Phase: v1.PodSucceeded}},
			true,
		},
		{
			"failed pod",
			&v1.Pod{Status: v1.PodStatus{Phase: v1.PodFailed}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTerminalPod(tt.pod)
			if tt.expectedResult != result {
				t.Errorf("isTerminalPod() failed. test name: %s, expected: %v, actual: %v",
					tt.name, tt.expectedResult, result)
			}
		})
	}
}

// TestGetPodMetadata_TerminalPodSkipsResourceClaimLookup verifies that pods
// in Succeeded/Failed phases do not trigger a ResourceClaim lookup. The DRA
// driver removes per-pod ResourceClaims when pods reach a terminal phase, so
// fetching them on every reconcile would always fail and produce spurious
// error logs (issue #1529).
func TestGetPodMetadata_TerminalPodSkipsResourceClaimLookup(t *testing.T) {
	tests := []struct {
		name  string
		phase v1.PodPhase
	}{
		{"succeeded pod with missing claim", v1.PodSucceeded},
		{"failed pod with missing claim", v1.PodFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{Name: "gpu", ResourceClaimName: ptr.To("missing-claim")},
					},
				},
				Status: v1.PodStatus{Phase: tt.phase},
			}

			scheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(scheme))
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			meta, err := GetPodMetadata(context.Background(), pod, kubeClient, "V1")
			assert.NoError(t, err)
			assert.NotNil(t, meta)
			assert.Empty(t, meta.RequestedResources)
			assert.Empty(t, meta.AllocatedResources)
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
