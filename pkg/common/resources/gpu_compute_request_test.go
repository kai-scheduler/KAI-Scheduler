// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func podWithComputeRequest(containerName, value string, extra map[string]string) *v1.Pod {
	annotations := map[string]string{constants.GpuFraction: "0.5"}
	for key, annotationValue := range extra {
		annotations[key] = annotationValue
	}
	if value != "" || containerName != "" {
		annotations[CalcGpuComputeRequestAnnotationForContainer(containerName)] = value
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default", Annotations: annotations},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
	}
}

// The compute request is user-settable, so every shape a user can type has to
// land on a definite answer: a usable portion or a rejection, never a silent
// reinterpretation.
func TestParsePodGPUFractionRequest_ComputeRequestBoundaries(t *testing.T) {
	tests := []struct {
		value       string
		wantPortion float64
		wantErr     bool
	}{
		{value: "1", wantPortion: 1},
		{value: "1.0", wantPortion: 1},
		{value: "0.5", wantPortion: 0.5},
		{value: ".5", wantPortion: 0.5},
		{value: "+0.5", wantPortion: 0.5},
		{value: "5e-1", wantPortion: 0.5},
		{value: "1e-9", wantPortion: 1e-9},
		{value: "0", wantErr: true},
		{value: "0.0", wantErr: true},
		{value: "-0.5", wantErr: true},
		{value: "-0", wantErr: true},
		{value: "1.0000001", wantErr: true},
		{value: "2", wantErr: true},
		{value: "100", wantErr: true},
		{value: "NaN", wantErr: true},
		{value: "nan", wantErr: true},
		{value: "Inf", wantErr: true},
		{value: "+Inf", wantErr: true},
		{value: "-Inf", wantErr: true},
		{value: "50%", wantErr: true},
		{value: "0.5 ", wantErr: true},
		{value: " 0.5", wantErr: true},
		{value: "half", wantErr: true},
		{value: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParsePodGPUFractionRequest(podWithComputeRequest("main", tt.value, nil))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParsePodGPUFractionRequest(%q) = %+v, want an error", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePodGPUFractionRequest(%q) returned %v", tt.value, err)
			}
			if got.ComputePortion != tt.wantPortion {
				t.Errorf("ComputePortion = %v, want %v", got.ComputePortion, tt.wantPortion)
			}
		})
	}
}

// Absent is not the same as invalid: the pod keeps its fraction request and the
// node derives compute from its memory share.
func TestParsePodGPUFractionRequest_ComputeRequestAbsent(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "p1",
			Namespace:   "default",
			Annotations: map[string]string{constants.GpuFraction: "0.5"},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "main"}}},
	}

	got, err := ParsePodGPUFractionRequest(pod)
	if err != nil {
		t.Fatalf("ParsePodGPUFractionRequest returned %v", err)
	}
	if got.ComputePortion != 0 {
		t.Errorf("ComputePortion = %v, want 0 for an absent annotation", got.ComputePortion)
	}
	if got.Portion != 0.5 {
		t.Errorf("Portion = %v, want 0.5", got.Portion)
	}
}

// A compute request naming a container the pod does not have is accepted, which
// matches how every other NvFractions annotation behaves.
func TestParsePodGPUFractionRequest_ComputeRequestForUnknownContainer(t *testing.T) {
	got, err := ParsePodGPUFractionRequest(podWithComputeRequest("does-not-exist", "0.25", nil))
	if err != nil {
		t.Fatalf("ParsePodGPUFractionRequest returned %v", err)
	}
	if got.ComputePortion != 0.25 {
		t.Errorf("ComputePortion = %v, want 0.25", got.ComputePortion)
	}
}

// A pod whose containers disagree must resolve to one answer, the same one every
// time: annotations live in a map, and picking whichever key iteration reaches
// first makes the scheduler charge a different amount on every snapshot rebuild.
func TestParsePodGPUFractionRequest_MultipleContainersAreDeterministic(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p1",
			Namespace: "default",
			Annotations: map[string]string{
				constants.GpuFraction:                            "0.5",
				CalcGpuComputeRequestAnnotationForContainer("a"): "0.1",
				CalcGpuComputeRequestAnnotationForContainer("b"): "0.2",
				CalcGpuComputeRequestAnnotationForContainer("c"): "0.3",
			},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "a"}, {Name: "b"}, {Name: "c"}}},
	}

	first, err := ParsePodGPUFractionRequest(pod)
	if err != nil {
		t.Fatalf("ParsePodGPUFractionRequest returned %v", err)
	}
	if first.ComputePortion != 0.1 {
		t.Errorf("ComputePortion = %v, want the lowest-named container's 0.1", first.ComputePortion)
	}
	for i := 0; i < 500; i++ {
		got, err := ParsePodGPUFractionRequest(pod)
		if err != nil {
			t.Fatalf("ParsePodGPUFractionRequest returned %v", err)
		}
		if got.ComputePortion != first.ComputePortion {
			t.Fatalf("ComputePortion changed between parses of the same pod: %v then %v",
				first.ComputePortion, got.ComputePortion)
		}
	}
}
