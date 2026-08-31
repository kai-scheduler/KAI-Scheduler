// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

// TestGetPodMetadata_EffectiveRequests pins the aggregation semantics of allocated
// resources: KEP-753 init/sidecar formula plus KEP-1287 effective requests, matching
// the scheduler's internal accounting.
func TestGetPodMetadata_EffectiveRequests(t *testing.T) {
	restartAlways := v1.ContainerRestartPolicyAlways
	runningStatus := v1.PodStatus{
		Phase: v1.PodRunning,
		Conditions: []v1.PodCondition{
			{Type: v1.PodScheduled, Status: v1.ConditionTrue},
		},
	}

	tests := []struct {
		name        string
		pod         *v1.Pod
		expectedCPU string
	}{
		{
			name: "infeasible resize charges enacted, not spec",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "main", Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("64")},
						}},
					},
				},
				Status: func() v1.PodStatus {
					s := *runningStatus.DeepCopy()
					s.Conditions = append(s.Conditions, v1.PodCondition{
						Type: v1.PodResizePending, Status: v1.ConditionTrue, Reason: v1.PodReasonInfeasible,
					})
					s.ContainerStatuses = []v1.ContainerStatus{
						{
							Name: "main",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
							},
						},
					}
					return s
				}(),
			},
			expectedCPU: "500m",
		},
		{
			name: "in-progress downsize charges enacted over spec",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "main", Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
						}},
					},
				},
				Status: func() v1.PodStatus {
					s := *runningStatus.DeepCopy()
					s.ContainerStatuses = []v1.ContainerStatus{
						{
							Name: "main",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")},
							},
						},
					}
					return s
				}(),
			},
			expectedCPU: "2",
		},
		{
			name: "init-phase peak dominates steady state",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{Name: "init", Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("4")},
						}},
					},
					Containers: []v1.Container{
						{Name: "main", Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
						}},
					},
				},
				Status: *runningStatus.DeepCopy(),
			},
			expectedCPU: "4",
		},
		{
			name: "sidecar adds to steady state",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{Name: "sidecar", RestartPolicy: &restartAlways, Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
						}},
					},
					Containers: []v1.Container{
						{Name: "main", Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")},
						}},
					},
				},
				Status: *runningStatus.DeepCopy(),
			},
			expectedCPU: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(scheme))
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			meta, err := GetPodMetadata(context.Background(), tt.pod, kubeClient, "V1")
			assert.NoError(t, err)
			expected := resource.MustParse(tt.expectedCPU)
			got := meta.AllocatedResources[v1.ResourceCPU]
			assert.Zero(t, expected.Cmp(got), "allocated cpu: want %s got %s", expected.String(), got.String())
		})
	}
}
