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

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestPodSteadyStateResources(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want v1.ResourceList
	}{
		{
			name: "regular containers are summed",
			pod: &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{
				{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
				{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")}}},
			}}},
			want: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1500m")},
		},
		{
			name: "native sidecar is counted",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{{
					RestartPolicy: ptr.To(v1.ContainerRestartPolicyAlways),
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("250m")},
					},
				}},
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
				},
			}},
			want: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1250m")},
		},
		{
			name: "plain init container is not counted",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("8")},
					},
				}},
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
				},
			}},
			want: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
		},
		{
			name: "pod overhead is counted",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceMemory: resource.MustParse("1Gi")}}},
				},
				Overhead: v1.ResourceList{v1.ResourceMemory: resource.MustParse("128Mi")},
			}},
			want: v1.ResourceList{v1.ResourceMemory: resource.MustParse("1152Mi")},
		},
		{
			name: "gpu in pod overhead is not counted",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
				},
				Overhead: v1.ResourceList{
					v1.ResourceCPU:                            resource.MustParse("100m"),
					v1.ResourceName("nvidia.com/gpu"):         resource.MustParse("1"),
					v1.ResourceName("amd.com/gpu"):            resource.MustParse("1"),
					v1.ResourceName("somethingelse/gpu"):      resource.MustParse("1"),
					v1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1"),
				},
			}},
			want: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1100m")},
		},
		{
			// Any name the queue counts as a GPU, not just the two the scheduler names. getAllocatedGpus
			// matches on the "/gpu" suffix, so letting one of these in would move kai_queue_allocated_gpus.
			name: "gpu requested by a native sidecar is not counted",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{{
					RestartPolicy: ptr.To(v1.ContainerRestartPolicyAlways),
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:                       resource.MustParse("250m"),
							v1.ResourceName("nvidia.com/gpu"):    resource.MustParse("1"),
							v1.ResourceName("amd.com/gpu"):       resource.MustParse("1"),
							v1.ResourceName("somethingelse/gpu"): resource.MustParse("2"),
						},
					},
				}},
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
				},
			}},
			want: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1250m")},
		},
		{
			// A sidecar-only vendor GPU: before this change the queue reported 0 GPUs for such a pod, and it
			// still does. This is the invariant behind "kai_queue_allocated_gpus is unchanged".
			name: "a sidecar's vendor gpu never reaches the status",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{{
					RestartPolicy: ptr.To(v1.ContainerRestartPolicyAlways),
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceName("somethingelse/gpu"): resource.MustParse("2")},
					},
				}},
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
				},
			}},
			want: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
		},
		{
			name: "mig requested by a native sidecar is not counted",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{{
					RestartPolicy: ptr.To(v1.ContainerRestartPolicyAlways),
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("250m"),
							v1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1"),
						},
					},
				}},
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
				},
			}},
			want: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1250m")},
		},
		{
			// The invariant that keeps every existing whole-GPU workload reporting the same number.
			name: "a regular container's gpu is still counted",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("1"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
						},
					},
				}},
			}},
			want: v1.ResourceList{
				v1.ResourceCPU:                    resource.MustParse("1"),
				v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
			},
		},
		{
			name: "a sidecar's non-gpu extended resources are counted",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{{
					RestartPolicy: ptr.To(v1.ContainerRestartPolicyAlways),
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceEphemeralStorage:        resource.MustParse("1Gi"),
							v1.ResourceName("example.com/foo"): resource.MustParse("2"),
						},
					},
				}},
				Containers: []v1.Container{
					{Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}}},
				},
			}},
			want: v1.ResourceList{
				v1.ResourceCPU:                     resource.MustParse("1"),
				v1.ResourceEphemeralStorage:        resource.MustParse("1Gi"),
				v1.ResourceName("example.com/foo"): resource.MustParse("2"),
			},
		},
		{
			name: "pod without requests",
			pod:  &v1.Pod{},
			want: v1.ResourceList{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := podSteadyStateResources(tt.pod)
			assert.Len(t, got, len(tt.want))
			for name, want := range tt.want {
				gotQuantity := got[name]
				assert.Zero(t, gotQuantity.Cmp(want),
					"resource %s: got %s, want %s", name, gotQuantity.String(), want.String())
			}
		})
	}
}

// TestCalculateRequestedResources_GpuAnnotations pins why a sidecar's GPU is left out. The scheduler rebuilds a
// pod's GPU requirement from a GPU-sharing or legacy MIG annotation and discards the container request, so
// adding a sidecar GPU on top would report a GPU that nothing reserved. Two of these pods reach a live cluster:
// getFirstGPULimit only looks for nvidia.com/gpu limits, so an amd.com/gpu sidecar is admitted, and it never
// looks at MIG annotations at all. The nvidia-on-fraction pods are rejected by admission today and are here as
// defense in depth.
func TestCalculateRequestedResources_GpuAnnotations(t *testing.T) {
	sidecarRequesting := func(requests v1.ResourceList) v1.Container {
		return v1.Container{
			Name:          "sidecar",
			RestartPolicy: ptr.To(v1.ContainerRestartPolicyAlways),
			Resources:     v1.ResourceRequirements{Requests: requests, Limits: requests},
		}
	}

	tests := []struct {
		name        string
		annotations map[string]string
		sidecar     v1.Container
		want        v1.ResourceList
	}{
		{
			name:        "amd gpu sidecar on a gpu-fraction pod",
			annotations: map[string]string{constants.GpuFraction: "0.5"},
			sidecar:     sidecarRequesting(v1.ResourceList{v1.ResourceName("amd.com/gpu"): resource.MustParse("1")}),
			want: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("1"),
				v1.ResourceName(constants.NvidiaGpuResource): resource.MustParse("0.5"),
			},
		},
		{
			name:        "nvidia gpu sidecar on a gpu-fraction pod",
			annotations: map[string]string{constants.GpuFraction: "0.5"},
			sidecar: sidecarRequesting(
				v1.ResourceList{v1.ResourceName(constants.NvidiaGpuResource): resource.MustParse("1")}),
			want: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("1"),
				v1.ResourceName(constants.NvidiaGpuResource): resource.MustParse("0.5"),
			},
		},
		{
			name:        "whole gpu sidecar on a gpu-memory pod",
			annotations: map[string]string{constants.GpuMemory: "2000"},
			sidecar: sidecarRequesting(
				v1.ResourceList{v1.ResourceName(constants.NvidiaGpuResource): resource.MustParse("1")}),
			want: v1.ResourceList{
				v1.ResourceCPU:                       resource.MustParse("1"),
				v1.ResourceName("run.ai/gpu.memory"): resource.MustParse("2000"),
			},
		},
		{
			// Admission never inspects MIG annotations, so this one is reachable.
			name:        "whole gpu sidecar on a legacy mig annotated pod",
			annotations: map[string]string{"nvidia.com/mig-1g.10gb": "1"},
			sidecar: sidecarRequesting(
				v1.ResourceList{v1.ResourceName(constants.NvidiaGpuResource): resource.MustParse("1")}),
			want: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "n", Annotations: tt.annotations},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{tt.sidecar},
					Containers: []v1.Container{{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
						},
					}},
				},
			}

			got, err := calculateRequestedResources(context.Background(), pod, nil, nil)
			assert.NoError(t, err)
			assert.Len(t, got, len(tt.want))
			for name, want := range tt.want {
				gotQuantity := got[name]
				assert.Zero(t, gotQuantity.Cmp(want),
					"resource %s: got %s, want %s", name, gotQuantity.String(), want.String())
			}
		})
	}
}

func TestGetPodMetadata_SidecarAndOverhead(t *testing.T) {
	podWithSidecar := func(phase v1.PodPhase, scheduled bool) *v1.Pod {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "n"},
			Spec: v1.PodSpec{
				InitContainers: []v1.Container{{
					Name:          "sidecar",
					RestartPolicy: ptr.To(v1.ContainerRestartPolicyAlways),
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
					},
				}},
				Containers: []v1.Container{{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					},
				}},
				Overhead: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
			},
			Status: v1.PodStatus{Phase: phase},
		}
		if scheduled {
			pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodScheduled, Status: v1.ConditionTrue}}
		}
		return pod
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	twoCores := resource.MustParse("2")

	t.Run("pending and unscheduled pod is requested but not allocated", func(t *testing.T) {
		meta, err := GetPodMetadata(context.Background(), podWithSidecar(v1.PodPending, false), kubeClient, "V1")
		assert.NoError(t, err)
		requested := meta.RequestedResources[v1.ResourceCPU]
		assert.Zero(t, requested.Cmp(twoCores), "requested cpu: got %s", requested.String())
		assert.Empty(t, meta.AllocatedResources)
	})

	t.Run("running pod is both requested and allocated", func(t *testing.T) {
		meta, err := GetPodMetadata(context.Background(), podWithSidecar(v1.PodRunning, true), kubeClient, "V1")
		assert.NoError(t, err)
		requested := meta.RequestedResources[v1.ResourceCPU]
		allocated := meta.AllocatedResources[v1.ResourceCPU]
		assert.Zero(t, requested.Cmp(twoCores), "requested cpu: got %s", requested.String())
		assert.Zero(t, allocated.Cmp(twoCores), "allocated cpu: got %s", allocated.String())
	})
}

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
