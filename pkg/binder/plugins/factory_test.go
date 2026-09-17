// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1binder "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/binder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	bindercommon "github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

func TestQuantityMiBArgumentOrDefault(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]string
		wantMiB   uint64
		wantErr   bool
	}{
		{
			name:      "missing argument falls back to the default",
			arguments: map[string]string{},
			wantMiB:   1024,
		},
		{
			name:      "empty argument falls back to the default",
			arguments: map[string]string{ReservedGpuMemoryArgument: ""},
			wantMiB:   1024,
		},
		{
			name:      "binary quantity",
			arguments: map[string]string{ReservedGpuMemoryArgument: "1Gi"},
			wantMiB:   1024,
		},
		{
			name:      "decimal quantity",
			arguments: map[string]string{ReservedGpuMemoryArgument: "1G"},
			wantMiB:   953,
		},
		{
			name:      "mebibyte quantity",
			arguments: map[string]string{ReservedGpuMemoryArgument: "512Mi"},
			wantMiB:   512,
		},
		{
			name:      "explicitly disabled",
			arguments: map[string]string{ReservedGpuMemoryArgument: "0"},
			wantMiB:   0,
		},
		{
			name:      "sub-MiB quantity rounds down to no reserve",
			arguments: map[string]string{ReservedGpuMemoryArgument: "1048575"},
			wantMiB:   0,
		},
		{
			name:      "negative quantity",
			arguments: map[string]string{ReservedGpuMemoryArgument: "-1Gi"},
			wantErr:   true,
		},
		{
			// A typo in an operator's config must fail the binder, not silently
			// restore the default reserve.
			name:      "garbage quantity",
			arguments: map[string]string{ReservedGpuMemoryArgument: "one-gig"},
			wantErr:   true,
		},
		{
			name:      "quantity with a unit typo",
			arguments: map[string]string{ReservedGpuMemoryArgument: "1GiB"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := quantityMiBArgumentOrDefault(tt.arguments, ReservedGpuMemoryArgument, DefaultReservedGpuMemory)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %d MiB", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantMiB {
				t.Fatalf("quantityMiBArgumentOrDefault() = %d MiB, want %d MiB", got, tt.wantMiB)
			}
		})
	}
}

func TestNewNvFractionsPluginAppliesReservedGpuMemoryArgument(t *testing.T) {
	tests := []struct {
		name          string
		arguments     map[string]string
		wantMemory    string
		wantBuildFail bool
	}{
		{
			name:       "default plugin arguments",
			arguments:  defaultNvFractionsArguments(),
			wantMemory: "488Mi",
		},
		{
			// Arguments overrides replace the default map wholesale, so the
			// reserve must survive as the factory's fallback.
			name:       "user override drops the reserve argument",
			arguments:  map[string]string{CDIEnabledArgument: "false"},
			wantMemory: "488Mi",
		},
		{
			name:       "explicitly disabled reserve",
			arguments:  map[string]string{CDIEnabledArgument: "false", ReservedGpuMemoryArgument: "0"},
			wantMemory: "1000Mi",
		},
		{
			name:          "invalid reserve fails the build",
			arguments:     map[string]string{CDIEnabledArgument: "false", ReservedGpuMemoryArgument: "one-gig"},
			wantBuildFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin, err := newNvFractionsPlugin(PluginBuildContext{}, tt.arguments)
			if tt.wantBuildFail {
				if err == nil {
					t.Fatalf("expected plugin build to fail")
				}
				if !strings.Contains(err.Error(), ReservedGpuMemoryArgument) {
					t.Fatalf("build error = %v, want it to name %s", err, ReservedGpuMemoryArgument)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected build error: %v", err)
			}

			bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}
			if err := plugin.PreBind(context.Background(), fractionPodForFactoryTest(),
				gpuNodeForFactoryTest(), fractionBindRequestForFactoryTest(), bindingState); err != nil {
				t.Fatalf("unexpected PreBind error: %v", err)
			}

			memoryKey := resources.CalcGpuFractionAnnotationForContainer("container-0")
			if got := bindingState.BindingPodAnnotations[memoryKey]; got != tt.wantMemory {
				t.Fatalf("memory annotation = %q, want %q", got, tt.wantMemory)
			}
		})
	}
}

func defaultNvFractionsArguments() map[string]string {
	return kaiv1binder.DefaultPluginsConfig(DefaultBindTimeoutSeconds, DefaultCDIEnabled,
		false, false, true)[NvFractionsPluginName].Arguments
}

func fractionPodForFactoryTest() *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
	}
}

func gpuNodeForFactoryTest() *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.NvidiaGpuMemory: "2000"}},
	}
}

func fractionBindRequestForFactoryTest() *v1alpha2.BindRequest {
	return &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			ReceivedResourceType: bindercommon.ReceivedTypeFraction,
			ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: "0.5"},
		},
	}
}
