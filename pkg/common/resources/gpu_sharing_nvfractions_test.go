// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

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
