// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"errors"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestParsePodGPUFractionRequest(t *testing.T) {
	tests := []struct {
		name              string
		annotations       map[string]string
		wantNil           bool
		wantFractionType  FractionType
		wantPortion       float64
		wantMemory        string
		wantLimit         string
		wantNumDevices    int64
		wantErrContaining string
	}{
		{
			name:    "no GPU fraction annotations",
			wantNil: true,
		},
		{
			name: "legacy portion request",
			annotations: map[string]string{
				constants.GpuFraction: "0.25",
			},
			wantFractionType: FractionTypePortion,
			wantPortion:      0.25,
			wantNumDevices:   1,
		},
		{
			name: "legacy memory request",
			annotations: map[string]string{
				constants.GpuMemory: "2048",
			},
			wantFractionType: FractionTypeMemory,
			wantMemory:       "2Gi",
			wantNumDevices:   1,
		},
		{
			name: "NvFractions request takes precedence over legacy annotations",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix:   "2Gi",
				constants.GpuFractionsNumDevices: "3",
				constants.GpuFraction:            "0.25",
				constants.GpuMemory:              "2048",
			},
			wantFractionType: FractionTypeNvFractions,
			wantMemory:       "1Gi",
			wantLimit:        "2Gi",
			wantNumDevices:   3,
		},
		{
			name: "gpu-compute.mode annotation does not break NvFractions parsing",
			annotations: map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
				CalcGpuComputeSharingModeAnnotationForContainer("main"):                                   "sm-sharing",
			},
			wantFractionType: FractionTypeNvFractions,
			wantMemory:       "1Gi",
			wantNumDevices:   1,
		},
		{
			name: "invalid legacy portion request",
			annotations: map[string]string{
				constants.GpuFraction: "1.2",
			},
			wantErrContaining: "gpu-fraction annotation value must be a positive number smaller than 1.0",
		},
		{
			name: "invalid legacy memory request",
			annotations: map[string]string{
				constants.GpuMemory: "0",
			},
			wantErrContaining: "gpu-memory annotation value must be a positive integer greater than 0",
		},
		{
			name: "invalid number of fractional devices",
			annotations: map[string]string{
				constants.GpuFractionsNumDevices: "0",
				constants.GpuFraction:            "0.5",
			},
			wantErrContaining: "fraction count annotation value must be a positive integer greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePodGPUFractionRequest(podWithAnnotations(tt.annotations))
			assertErrorContains(t, err, tt.wantErrContaining)
			if tt.wantErrContaining != "" {
				return
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("ParsePodGPUFractionRequest() = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParsePodGPUFractionRequest() = nil, want request")
			}
			if got.FractionType != tt.wantFractionType {
				t.Errorf("FractionType = %q, want %q", got.FractionType, tt.wantFractionType)
			}
			if got.Portion != tt.wantPortion {
				t.Errorf("Portion = %v, want %v", got.Portion, tt.wantPortion)
			}
			if got.NumDevices != tt.wantNumDevices {
				t.Errorf("NumDevices = %d, want %d", got.NumDevices, tt.wantNumDevices)
			}
			assertQuantityString(t, got.Memory, tt.wantMemory)
			assertQuantityString(t, got.Limit, tt.wantLimit)
		})
	}
}

func TestRequestsGPU(t *testing.T) {
	tests := []struct {
		name         string
		pod          *v1.Pod
		wantGPU      bool
		wantWhole    bool
		wantFraction bool
	}{
		{
			name: "legacy fraction annotation",
			pod: podWithAnnotations(map[string]string{
				constants.GpuFraction: "0.5",
			}),
			wantGPU:      true,
			wantFraction: true,
		},
		{
			name: "NvFractions request annotation",
			pod: podWithAnnotations(map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
			}),
			wantGPU:      true,
			wantFraction: true,
		},
		{
			name: "NvFractions limit-only annotation",
			pod: podWithAnnotations(map[string]string{
				constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix: "2Gi",
			}),
			wantGPU:      true,
			wantFraction: true,
		},
		{
			name: "whole GPU request",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								constants.NvidiaGpuResource: resource.MustParse("1"),
							},
						},
					}},
				},
			},
			wantGPU:   true,
			wantWhole: true,
		},
		{
			name: "no GPU request",
			pod:  &v1.Pod{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequestsGPU(tt.pod); got != tt.wantGPU {
				t.Errorf("RequestsGPU() = %t, want %t", got, tt.wantGPU)
			}
			if got := RequestsWholeGPU(tt.pod); got != tt.wantWhole {
				t.Errorf("RequestsWholeGPU() = %t, want %t", got, tt.wantWhole)
			}
			if got := RequestsGPUFraction(tt.pod); got != tt.wantFraction {
				t.Errorf("RequestsGPUFraction() = %t, want %t", got, tt.wantFraction)
			}
		})
	}
}

func TestFractionDeviceHelpers(t *testing.T) {
	multiFractionPod := podWithAnnotations(map[string]string{
		constants.GpuFractionsNumDevices: "2",
		constants.GpuFraction:            "0.5",
	})

	numDevices, err := GetNumGPUFractionDevices(multiFractionPod)
	if err != nil {
		t.Fatalf("GetNumGPUFractionDevices() error = %v", err)
	}
	if numDevices != 2 {
		t.Errorf("GetNumGPUFractionDevices() = %d, want 2", numDevices)
	}

	isMultiFraction, err := IsMultiFraction(multiFractionPod)
	if err != nil {
		t.Fatalf("IsMultiFraction() error = %v", err)
	}
	if !isMultiFraction {
		t.Errorf("IsMultiFraction() = false, want true")
	}

	isMultiFraction, err = IsMultiFraction(&v1.Pod{})
	if err != nil {
		t.Fatalf("IsMultiFraction() error = %v", err)
	}
	if isMultiFraction {
		t.Errorf("IsMultiFraction() = true, want false")
	}

	_, err = GetNumGPUFractionDevices(&v1.Pod{})
	if !errors.Is(err, fractionDevicesAnnotationNotFound) {
		t.Fatalf("GetNumGPUFractionDevices() error = %v, want fractionDevicesAnnotationNotFound", err)
	}
}

func TestGetGpuGroups(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				constants.GPUGroup: "group-a",
				constants.MultiGpuGroupLabelPrefix + "group-b": "group-b",
			},
		},
	}

	gpuGroups := GetGpuGroups(pod)
	if len(gpuGroups) != 2 {
		t.Fatalf("GetGpuGroups() = %v, want two groups", gpuGroups)
	}
	assertStringInSlice(t, gpuGroups, "group-a")
	assertStringInSlice(t, gpuGroups, "group-b")

	labelKey, labelValue := GetMultiFractionGpuGroupLabel("group-c")
	if labelKey != constants.MultiGpuGroupLabelPrefix+"group-c" || labelValue != "group-c" {
		t.Fatalf("GetMultiFractionGpuGroupLabel() = %q, %q", labelKey, labelValue)
	}
}

func podWithAnnotations(annotations map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: annotations,
		},
	}
}

func assertQuantityString(t *testing.T, got *resource.Quantity, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Fatalf("quantity = %s, want nil", got.String())
		}
		return
	}
	if got == nil {
		t.Fatalf("quantity = nil, want %s", want)
	}
	wantQuantity := resource.MustParse(want)
	if got.Cmp(wantQuantity) != 0 {
		t.Fatalf("quantity = %s, want %s", got.String(), want)
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("error = nil, want containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want containing %q", err.Error(), want)
	}
}

func assertStringInSlice(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%q not found in %v", want, values)
}
