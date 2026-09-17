// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestCalcGpuFractionAnnotationForContainer(t *testing.T) {
	got := CalcGpuFractionAnnotationForContainer("main")
	want := constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix
	if got != want {
		t.Fatalf("CalcGpuFractionAnnotationForContainer() = %q, want %q", got, want)
	}
}

func TestGpuMemoryAnnotationToNvFractionsMemoryRequest(t *testing.T) {
	got := GpuMemoryAnnotationToNvFractionsMemoryRequest(2048)
	want := resource.MustParse("2Gi")
	if got.Cmp(want) != 0 {
		t.Fatalf("GpuMemoryAnnotationToNvFractionsMemoryRequest() = %s, want %s", got.String(), want.String())
	}
}

func TestExtractNvFractionsData(t *testing.T) {
	tests := []struct {
		name              string
		annotations       map[string]string
		want              map[string]NvFractionsContainerRequest
		wantErrContaining string
	}{
		{
			name: "extracts request and limit by container",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix:   "2Gi",
				"other-annotation": "ignored",
			},
			want: map[string]NvFractionsContainerRequest{
				"main": {
					Request: quantityPtr("1Gi"),
					Limit:   quantityPtr("2Gi"),
				},
			},
		},
		{
			name: "extracts request and limit by container - ignore visible devices annotation",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix:  "1Gi",
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix:    "2Gi",
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsVisibleDevicesSuffix: "1,2",
				"other-annotation": "ignored",
			},
			want: map[string]NvFractionsContainerRequest{
				"main": {
					Request: quantityPtr("1Gi"),
					Limit:   quantityPtr("2Gi"),
				},
			},
		},
		{
			name: "defaults request from limit",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix: "2Gi",
			},
			want: map[string]NvFractionsContainerRequest{
				"main": {
					Request: quantityPtr("2Gi"),
					Limit:   quantityPtr("2Gi"),
				},
			},
		},
		{
			name: "extracts multiple containers",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix:    "1Gi",
				constants.NvFractionsAnnotationPrefix + "sidecar" + constants.NvFractionsMemoryRequestSuffix: "512Mi",
			},
			want: map[string]NvFractionsContainerRequest{
				"main": {
					Request: quantityPtr("1Gi"),
				},
				"sidecar": {
					Request: quantityPtr("512Mi"),
				},
			},
		},
		{
			name: "compute sharing mode annotation does not break extraction of a sibling container's request",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
				CalcGpuComputeSharingModeAnnotationForContainer("main"):                                   "sm-sharing",
			},
			want: map[string]NvFractionsContainerRequest{
				"main": {
					Request:     quantityPtr("1Gi"),
					ComputeMode: ptr.To(schedulingv1alpha2.GPUComputeSharingModeSMSharing),
				},
			},
		},
		{
			name: "skips device list annotation",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
				CalcGpuVisibleDevicesAnnotationForContainer("main"):                                       "gpu-0",
			},
			want: map[string]NvFractionsContainerRequest{
				"main": {
					Request: quantityPtr("1Gi"),
				},
			},
		},
		{
			name: "skips binder-written compute portion annotation",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
				CalcGpuComputePortionAnnotationForContainer("main"):                                       "0.5",
				CalcGpuVisibleDevicesAnnotationForContainer("main"):                                       "GPU-0",
				CalcGpuComputeSharingModeAnnotationForContainer("main"):                                   "sm-sharing",
			},
			want: map[string]NvFractionsContainerRequest{
				"main": {
					Request:     quantityPtr("1Gi"),
					ComputeMode: ptr.To(schedulingv1alpha2.GPUComputeSharingModeSMSharing),
				},
			},
		},
		{
			// A quantity-shaped portion value must not be mistaken for a memory request.
			name: "compute portion alone yields no request",
			annotations: map[string]string{
				CalcGpuComputePortionAnnotationForContainer("main"): "1Gi",
			},
			want: map[string]NvFractionsContainerRequest{},
		},
		{
			name: "rejects invalid annotation key",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main.unknown": "1Gi",
			},
			wantErrContaining: "invalid NvFractions annotation key",
		},
		{
			name: "rejects invalid quantity",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "bad",
			},
			wantErrContaining: "annotation value must be a valid Kubernetes memory quantity greater than 0",
		},
		{
			name: "rejects zero quantity",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "0Mi",
			},
			wantErrContaining: "annotation value must be a valid Kubernetes memory quantity greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractNvFractionsData(podWithAnnotations(tt.annotations))
			assertErrorContains(t, err, tt.wantErrContaining)
			if tt.wantErrContaining != "" {
				return
			}
			assertNvFractionsData(t, got, tt.want)
		})
	}
}

func TestParseNvFractionsAnnotationKey(t *testing.T) {
	tests := []struct {
		name              string
		annotationKey     string
		wantContainerName string
		wantType          nvFractionsAnnotationType
		wantErrContaining string
	}{
		{
			name:              "request annotation",
			annotationKey:     constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix,
			wantContainerName: "main",
			wantType:          nvFractionsRequestAnnotation,
		},
		{
			name:              "limit annotation",
			annotationKey:     constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix,
			wantContainerName: "main",
			wantType:          nvFractionsLimitAnnotation,
		},
		{
			name:              "visible devices annotation",
			annotationKey:     constants.NvFractionsAnnotationPrefix + "main2" + constants.NvFractionsVisibleDevicesSuffix,
			wantContainerName: "main2",
			wantType:          nvFractionsDevicesAnnotation,
		},
		{
			name:              "compute sharing mode annotation",
			annotationKey:     constants.NvFractionsAnnotationPrefix + "main" + constants.GpuComputeSharingModeSuffix,
			wantContainerName: "main",
			wantType:          nvFractionsComputeModeAnnotation,
		},
		{
			name:              "compute portion annotation",
			annotationKey:     constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsComputePortionSuffix,
			wantContainerName: "main",
			wantType:          nvFractionsComputePortionAnnotation,
		},
		{
			// The compute-mode branch is evaluated first; it must not swallow a portion key.
			name:              "compute portion on a container named after the compute mode suffix",
			annotationKey:     constants.NvFractionsAnnotationPrefix + "main" + constants.GpuComputeSharingModeSuffix + constants.NvFractionsComputePortionSuffix,
			wantContainerName: "main" + constants.GpuComputeSharingModeSuffix,
			wantType:          nvFractionsComputePortionAnnotation,
		},
		{
			name:              "compute portion with empty container name",
			annotationKey:     constants.NvFractionsAnnotationPrefix + constants.NvFractionsComputePortionSuffix,
			wantErrContaining: "invalid NvFractions annotation key",
		},
		{
			name:              "unknown gpu-compute suffix",
			annotationKey:     constants.NvFractionsAnnotationPrefix + "main.gpu-compute.percent",
			wantErrContaining: "invalid NvFractions annotation key",
		},
		{
			name:              "invalid annotation",
			annotationKey:     constants.NvFractionsAnnotationPrefix + "main",
			wantErrContaining: "invalid NvFractions annotation key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containerName, annotationType, err := parseNvFractionsAnnotationKey(tt.annotationKey)
			assertErrorContains(t, err, tt.wantErrContaining)
			if tt.wantErrContaining != "" {
				return
			}
			if containerName != tt.wantContainerName {
				t.Errorf("containerName = %q, want %q", containerName, tt.wantContainerName)
			}
			if annotationType != tt.wantType {
				t.Errorf("annotationType = %v, want %v", annotationType, tt.wantType)
			}
		})
	}
}

func assertNvFractionsData(
	t *testing.T, got map[string]NvFractionsContainerRequest, want map[string]NvFractionsContainerRequest,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ExtractNvFractionsData() returned %d containers, want %d: %#v", len(got), len(want), got)
	}
	for containerName, wantData := range want {
		gotData, ok := got[containerName]
		if !ok {
			t.Fatalf("ExtractNvFractionsData() missing container %q in %#v", containerName, got)
		}
		assertQuantityString(t, gotData.Request, quantityString(wantData.Request))
		assertQuantityString(t, gotData.Limit, quantityString(wantData.Limit))
		if gotData.ComputeMode == nil && wantData.ComputeMode == nil {
			continue
		}
		if gotData.ComputeMode == nil || wantData.ComputeMode == nil || *gotData.ComputeMode != *wantData.ComputeMode {
			t.Fatalf("ComputeMode = %v, want %v", gotData.ComputeMode, wantData.ComputeMode)
		}
	}
}

func quantityPtr(value string) *resource.Quantity {
	quantity := resource.MustParse(value)
	return &quantity
}

func quantityString(quantity *resource.Quantity) string {
	if quantity == nil {
		return ""
	}
	return quantity.String()
}

func TestCalcGpuComputePortionAnnotationForContainer(t *testing.T) {
	got := CalcGpuComputePortionAnnotationForContainer("main")
	want := constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsComputePortionSuffix
	if got != want {
		t.Fatalf("CalcGpuComputePortionAnnotationForContainer() = %q, want %q", got, want)
	}
	if got == CalcGpuComputeSharingModeAnnotationForContainer("main") {
		t.Fatalf("compute portion and compute mode annotation keys collide: %q", got)
	}
}

func TestIsBinderOwnedNvFractionsAnnotation(t *testing.T) {
	tests := []struct {
		name          string
		annotationKey string
		want          bool
	}{
		{
			name:          "visible devices",
			annotationKey: CalcGpuVisibleDevicesAnnotationForContainer("main"),
			want:          true,
		},
		{
			name:          "compute portion",
			annotationKey: CalcGpuComputePortionAnnotationForContainer("main"),
			want:          true,
		},
		{
			// Protection must not depend on the container name being valid.
			name:          "compute portion with empty container name",
			annotationKey: CalcGpuComputePortionAnnotationForContainer(""),
			want:          true,
		},
		{
			name:          "memory request",
			annotationKey: CalcGpuFractionAnnotationForContainer("main"),
			want:          false,
		},
		{
			name:          "memory limit",
			annotationKey: CalcGpuFractionLimitAnnotationForContainer("main"),
			want:          false,
		},
		{
			name:          "compute mode",
			annotationKey: CalcGpuComputeSharingModeAnnotationForContainer("main"),
			want:          false,
		},
		{
			name:          "compute portion suffix under a foreign prefix",
			annotationKey: constants.KaiFractionContainerAnnotationPrefix + "main" + constants.NvFractionsComputePortionSuffix,
			want:          false,
		},
		{
			name:          "unrelated annotation",
			annotationKey: "example.com/other",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBinderOwnedNvFractionsAnnotation(tt.annotationKey); got != tt.want {
				t.Fatalf("IsBinderOwnedNvFractionsAnnotation(%q) = %t, want %t", tt.annotationKey, got, tt.want)
			}
		})
	}
}
