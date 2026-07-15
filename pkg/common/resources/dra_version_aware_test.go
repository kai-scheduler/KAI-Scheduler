// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(objects ...runtime.Object) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...)
}

func TestDRACacheObject(t *testing.T) {
	tests := []struct {
		name          string
		draAPIVersion string
		expectedNil   bool
	}{
		{"V1 returns v1 ResourceClaim", "V1", false},
		{"V1beta2 returns v1beta2 ResourceClaim", "V1beta2", false},
		{"V1beta1 returns v1beta1 ResourceClaim", "V1beta1", false},
		{"empty string returns nil", "", true},
		{"unknown returns nil", "V1alpha3", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := DRACacheObject(tt.draAPIVersion)
			if tt.expectedNil {
				assert.Nil(t, obj)
			} else {
				assert.NotNil(t, obj)
			}
		})
	}
}

func TestFetchPodResourceClaims_NoClaims(t *testing.T) {
	pod := &v1.Pod{Spec: v1.PodSpec{}}
	client := newFakeClient().Build()

	claims, err := FetchPodResourceClaims(context.Background(), pod, client, "V1")
	assert.NoError(t, err)
	assert.Nil(t, claims)
}

func TestFetchPodResourceClaims_DRADisabled(t *testing.T) {
	pod := &v1.Pod{
		Spec: v1.PodSpec{
			ResourceClaims: []v1.PodResourceClaim{{Name: "claim1", ResourceClaimName: ptr.To("claim1")}},
		},
	}
	client := newFakeClient().Build()

	claims, err := FetchPodResourceClaims(context.Background(), pod, client, "")
	assert.NoError(t, err)
	assert.Nil(t, claims)
}

func TestFetchPodResourceClaims_V1(t *testing.T) {
	claim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-claim", Namespace: "default"},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{
					{Name: "gpu", Exactly: &resourceapi.ExactDeviceRequest{
						DeviceClassName: "gpu.nvidia.com",
						AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
						Count:           2,
					}},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: v1.PodSpec{
			ResourceClaims: []v1.PodResourceClaim{
				{Name: "gpu", ResourceClaimName: ptr.To("gpu-claim")},
			},
		},
	}

	client := newFakeClient(claim).Build()
	claims, err := FetchPodResourceClaims(context.Background(), pod, client, "V1")
	assert.NoError(t, err)
	assert.Len(t, claims, 1)
	assert.Equal(t, "gpu-claim", claims[0].Name)
	assert.Equal(t, "gpu.nvidia.com", claims[0].Spec.Devices.Requests[0].Exactly.DeviceClassName)
	assert.Equal(t, int64(2), claims[0].Spec.Devices.Requests[0].Exactly.Count)
}

func TestFetchPodResourceClaims_ClaimNotFound(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: v1.PodSpec{
			ResourceClaims: []v1.PodResourceClaim{
				{Name: "gpu", ResourceClaimName: ptr.To("missing-claim")},
			},
		},
	}

	client := newFakeClient().Build()
	_, err := FetchPodResourceClaims(context.Background(), pod, client, "V1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing-claim")
}

func TestDRAGPUResourceListFromClaims(t *testing.T) {
	claims := []*resourceapi.ResourceClaim{
		{
			Spec: resourceapi.ResourceClaimSpec{
				Devices: resourceapi.DeviceClaim{
					Requests: []resourceapi.DeviceRequest{
						{Name: "gpu", Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: "gpu.nvidia.com",
							AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
							Count:           2,
						}},
					},
				},
			},
		},
		{
			Spec: resourceapi.ResourceClaimSpec{
				Devices: resourceapi.DeviceClaim{
					Requests: []resourceapi.DeviceRequest{
						{Name: "gpu2", Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: "gpu.nvidia.com",
							AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
							Count:           3,
						}},
					},
				},
			},
		},
	}

	result := DRAGPUResourceListFromClaims(claims)
	expected := resource.MustParse("5")
	actual := result["gpu.nvidia.com"]
	assert.True(t, expected.Equal(actual), "expected %v, got %v", expected, actual)
}

func TestDRAGPUResourceListFromClaims_NilClaims(t *testing.T) {
	result := DRAGPUResourceListFromClaims(nil)
	assert.Empty(t, result)
}
