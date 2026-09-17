// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	bindercommon "github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

// noReservedGpuMemory keeps these cases' arithmetic on the node's full advertised
// GPU memory. Reserve behaviour is covered separately.
const noReservedGpuMemory uint64 = 0

func TestPreBindSetsAnnotationsForSharedAllocation(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
	}
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.NvidiaGpuMemory: "2000"}},
	}
	bindRequest := &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			ReceivedResourceType: bindercommon.ReceivedTypeFraction,
			ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: "0.5"},
		},
	}
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0", "1"}}

	err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod, node, bindRequest, bindingState)
	assert.NoError(t, err)

	memoryKey := resources.CalcGpuFractionAnnotationForContainer("container-0")
	visibleDevicesKey := resources.CalcGpuVisibleDevicesAnnotationForContainer("container-0")
	// 2000MiB * 0.5 with no reserve.
	assert.Equal(t, "1000Mi", bindingState.BindingPodAnnotations[memoryKey])
	assert.Equal(t, "0,1", bindingState.BindingPodAnnotations[visibleDevicesKey])
	assert.Equal(t, "0.5",
		bindingState.BindingPodAnnotations[resources.CalcGpuComputePortionAnnotationForContainer("container-0")])
}

func TestPreBindQualifiesVisibleDevicesAsCdiWhenEnabled(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
	}
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.NvidiaGpuMemory: "2000"}},
	}
	bindRequest := &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			ReceivedResourceType: bindercommon.ReceivedTypeFraction,
			ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: "0.5"},
		},
	}
	bindingState := &state.BindingState{ReservedGPUIds: []string{"GPU-abc", "GPU-def"}}

	err := New(true, noReservedGpuMemory).PreBind(context.Background(), pod, node, bindRequest, bindingState)
	assert.NoError(t, err)

	assert.Equal(t,
		"k8s.device-plugin.nvidia.com/gpu=GPU-abc,k8s.device-plugin.nvidia.com/gpu=GPU-def",
		bindingState.BindingPodAnnotations[resources.CalcGpuVisibleDevicesAnnotationForContainer("container-0")])
}

func TestPreBindSetsGpuMemoryPortionLimitAnnotation(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			constants.GpuFraction: "0.5",
			resources.CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.8",
		}},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
	}
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.NvidiaGpuMemory: "2000"}},
	}
	bindRequest := &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			ReceivedResourceType: bindercommon.ReceivedTypeFraction,
			ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: "0.5"},
		},
	}
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod, node, bindRequest, bindingState)
	assert.NoError(t, err)

	limitKey := resources.CalcGpuFractionLimitAnnotationForContainer("container-0")
	limitQuantity := resource.MustParse(bindingState.BindingPodAnnotations[limitKey])
	expectedQuantity := resource.MustParse("1600Mi")
	assert.Equal(t, expectedQuantity.Value(), limitQuantity.Value())

	sourceKey := resources.CalcGpuMemoryPortionLimitAnnotationForContainer("container-0")
	assert.Equal(t, "0.8", pod.Annotations[sourceKey])
}

func TestPreBindNoGpuMemoryPortionLimitAnnotationIsNoOp(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			constants.GpuFraction: "0.5",
		}},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
	}
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.NvidiaGpuMemory: "2000"}},
	}
	bindRequest := &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			ReceivedResourceType: bindercommon.ReceivedTypeFraction,
			ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: "0.5"},
		},
	}
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod, node, bindRequest, bindingState)
	assert.NoError(t, err)

	limitKey := resources.CalcGpuFractionLimitAnnotationForContainer("container-0")
	assert.NotContains(t, bindingState.BindingPodAnnotations, limitKey)
}

func TestPreBindNoOpForWholeGpuAllocation(t *testing.T) {
	pod := &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}}}
	node := &v1.Node{}
	bindRequest := &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{ReceivedResourceType: bindercommon.ReceivedTypeRegular},
	}
	bindingState := &state.BindingState{}

	err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod, node, bindRequest, bindingState)
	assert.NoError(t, err)
	assert.Empty(t, bindingState.BindingPodAnnotations)
}

// oneGiBInMiB matches the shipped reservedGpuMemory default of 1Gi.
const oneGiBInMiB uint64 = 1024

func TestPreBindSubtractsReservedGpuMemoryFromRequest(t *testing.T) {
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, oneGiBInMiB).PreBind(context.Background(), nvFractionsPod(nil),
		gpuNodeWithMemory("2000"), fractionBindRequest("0.5"), bindingState)
	assert.NoError(t, err)

	memoryKey := resources.CalcGpuFractionAnnotationForContainer("container-0")
	// (2000 - 1024) * 0.5, not 2000 * 0.5.
	assert.Equal(t, "488Mi", bindingState.BindingPodAnnotations[memoryKey])
	assert.Equal(t, map[string]string{
		memoryKey: "488Mi",
		resources.CalcGpuComputePortionAnnotationForContainer("container-0"): "0.5",
		resources.CalcGpuVisibleDevicesAnnotationForContainer("container-0"): "0",
	}, bindingState.BindingPodAnnotations)
}

// The reserve must not silently collapse a fraction into a zero-memory request.
func TestPreBindFailsWhenReserveLeavesLessThanTheRequestedFraction(t *testing.T) {
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, 1999).PreBind(context.Background(), nvFractionsPod(nil),
		gpuNodeWithMemory("2000"), fractionBindRequest("0.5"), bindingState)
	assert.ErrorContains(t, err, "calculated gpu memory request is zero")
	assert.Empty(t, bindingState.BindingPodAnnotations)
}

func TestPreBindSubtractsReservedGpuMemoryFromPortionLimit(t *testing.T) {
	pod := nvFractionsPod(map[string]string{
		resources.CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.8",
	})
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, oneGiBInMiB).PreBind(context.Background(), pod,
		gpuNodeWithMemory("2000"), fractionBindRequest("0.5"), bindingState)
	assert.NoError(t, err)

	limitKey := resources.CalcGpuFractionLimitAnnotationForContainer("container-0")
	// (2000 - 1024) * 0.8, not 2000 * 0.8.
	assert.Equal(t, "780Mi", bindingState.BindingPodAnnotations[limitKey])
}

// A reserve that swallows the GPU must fail the bind: the float subtraction goes
// negative, and converting that to uint64 would hand out a near-infinite request.
func TestPreBindFailsWhenReserveLeavesNoGpuMemory(t *testing.T) {
	tests := []struct {
		name                 string
		reservedGpuMemoryMiB uint64
		podAnnotations       map[string]string
	}{
		{
			name:                 "reserve equals gpu memory",
			reservedGpuMemoryMiB: 2000,
		},
		{
			name:                 "reserve exceeds gpu memory",
			reservedGpuMemoryMiB: 4096,
		},
		{
			name:                 "reserve exceeds gpu memory on the portion limit path",
			reservedGpuMemoryMiB: 4096,
			podAnnotations: map[string]string{
				resources.CalcGpuFractionAnnotationForContainer("container-0"):           "1Gi",
				resources.CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.8",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

			err := New(false, tt.reservedGpuMemoryMiB).PreBind(context.Background(), nvFractionsPod(tt.podAnnotations),
				gpuNodeWithMemory("2000"), fractionBindRequest("0.5"), bindingState)
			assert.ErrorContains(t, err, "leaves no allocatable memory")
			assert.Empty(t, bindingState.BindingPodAnnotations)
		})
	}
}

func TestPreBindWritesComputePortionFromReceivedPortion(t *testing.T) {
	tests := []struct {
		name          string
		portion       string
		nodeMemoryMiB string
		wantPortion   string
	}{
		{name: "half", portion: "0.5", nodeMemoryMiB: "2000", wantPortion: "0.5"},
		{name: "quarter", portion: "0.25", nodeMemoryMiB: "2000", wantPortion: "0.25"},
		{name: "whole", portion: "1", nodeMemoryMiB: "2000", wantPortion: "1"},
		{name: "tiny portion stays in decimal notation", portion: "0.00005", nodeMemoryMiB: "40000", wantPortion: "0.00005"},
		{name: "above one is clamped", portion: "1.5", nodeMemoryMiB: "2000", wantPortion: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

			err := New(false, noReservedGpuMemory).PreBind(context.Background(), nvFractionsPod(nil),
				gpuNodeWithMemory(tt.nodeMemoryMiB), fractionBindRequest(tt.portion), bindingState)
			assert.NoError(t, err)

			got := bindingState.BindingPodAnnotations[resources.CalcGpuComputePortionAnnotationForContainer("container-0")]
			assert.Equal(t, tt.wantPortion, got)
			assertParseableComputePortion(t, got)
		})
	}
}

func TestPreBindComputePortionFallsBackToMemoryRequest(t *testing.T) {
	tests := []struct {
		name        string
		bindRequest *v1alpha2.BindRequest
	}{
		{name: "no received gpu", bindRequest: fractionBindRequestWithoutGPU()},
		{name: "unparsable received portion", bindRequest: fractionBindRequest("not-a-number")},
		{name: "empty received portion", bindRequest: fractionBindRequest("")},
		{name: "zero received portion", bindRequest: fractionBindRequest("0")},
		{name: "negative received portion", bindRequest: fractionBindRequest("-0.5")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := nvFractionsPod(map[string]string{
				resources.CalcGpuFractionAnnotationForContainer("container-0"): "488Mi",
			})
			bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

			err := New(false, oneGiBInMiB).PreBind(context.Background(), pod,
				gpuNodeWithMemory("2000"), tt.bindRequest, bindingState)
			assert.NoError(t, err)

			// 488Mi of the 976MiB left after the reserve.
			got := bindingState.BindingPodAnnotations[resources.CalcGpuComputePortionAnnotationForContainer("container-0")]
			assert.Equal(t, "0.5", got)
		})
	}
}

// A memory request the workload declared itself must never buy more compute than
// the portion the scheduler handed out.
func TestPreBindComputePortionPrefersReceivedPortionOverMemoryRequest(t *testing.T) {
	pod := nvFractionsPod(map[string]string{
		resources.CalcGpuFractionAnnotationForContainer("container-0"): "1900Mi",
	})
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod,
		gpuNodeWithMemory("2000"), fractionBindRequest("0.1"), bindingState)
	assert.NoError(t, err)

	assert.Equal(t, "0.1",
		bindingState.BindingPodAnnotations[resources.CalcGpuComputePortionAnnotationForContainer("container-0")])
}

func TestPreBindComputePortionFallbackIsCappedAtOne(t *testing.T) {
	pod := nvFractionsPod(map[string]string{
		resources.CalcGpuFractionAnnotationForContainer("container-0"): "4000Mi",
	})
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod,
		gpuNodeWithMemory("2000"), fractionBindRequestWithoutGPU(), bindingState)
	assert.NoError(t, err)

	got := bindingState.BindingPodAnnotations[resources.CalcGpuComputePortionAnnotationForContainer("container-0")]
	assert.Equal(t, "1", got)
	assertParseableComputePortion(t, got)
}

func TestPreBindComputePortionFallbackStaysInDecimalNotation(t *testing.T) {
	pod := nvFractionsPod(map[string]string{
		resources.CalcGpuFractionAnnotationForContainer("container-0"): "1Mi",
	})
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod,
		gpuNodeWithMemory("40000"), fractionBindRequestWithoutGPU(), bindingState)
	assert.NoError(t, err)

	got := bindingState.BindingPodAnnotations[resources.CalcGpuComputePortionAnnotationForContainer("container-0")]
	assert.Equal(t, "0.000025", got)
	assertParseableComputePortion(t, got)
}

// A missing compute cap is preferable to a failed bind, so an unresolvable
// portion leaves the annotation out without failing PreBind.
func TestPreBindSkipsComputePortionWhenUnresolvable(t *testing.T) {
	tests := []struct {
		name          string
		node          *v1.Node
		memoryRequest string
	}{
		{
			name:          "node without gpu memory label",
			node:          &v1.Node{},
			memoryRequest: "488Mi",
		},
		{
			name:          "nil node",
			node:          nil,
			memoryRequest: "488Mi",
		},
		{
			name:          "memory request below one MiB",
			node:          gpuNodeWithMemory("2000"),
			memoryRequest: "512Ki",
		},
		{
			// A negative quantity would wrap through uint64 into a huge MiB count and
			// buy the pod a clamped full-GPU compute portion. Admission rejects such a
			// request, so reaching here means admission was bypassed or absent.
			name:          "negative memory request",
			node:          gpuNodeWithMemory("2000"),
			memoryRequest: "-1Gi",
		},
		{
			name:          "zero memory request",
			node:          gpuNodeWithMemory("2000"),
			memoryRequest: "0",
		},
		{
			name:          "memory request is not a quantity",
			node:          gpuNodeWithMemory("2000"),
			memoryRequest: "all-of-it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := nvFractionsPod(map[string]string{
				resources.CalcGpuFractionAnnotationForContainer("container-0"): tt.memoryRequest,
			})
			bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

			err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod,
				tt.node, fractionBindRequestWithoutGPU(), bindingState)
			assert.NoError(t, err)

			assert.NotContains(t, bindingState.BindingPodAnnotations,
				resources.CalcGpuComputePortionAnnotationForContainer("container-0"))
			assert.Equal(t, "0",
				bindingState.BindingPodAnnotations[resources.CalcGpuVisibleDevicesAnnotationForContainer("container-0")])
		})
	}
}

// A self-declared compute portion must not survive the bind. The annotation is a
// limit imposed on the workload, so honouring a value from the pod manifest would
// let a workload grant itself the whole GPU's SMs whenever admission is bypassed,
// not installed, or misconfigured.
func TestPreBindOverwritesSelfDeclaredComputePortionAnnotation(t *testing.T) {
	portionKey := resources.CalcGpuComputePortionAnnotationForContainer("container-0")
	pod := nvFractionsPod(map[string]string{portionKey: "1"})
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, noReservedGpuMemory).PreBind(context.Background(), pod,
		gpuNodeWithMemory("2000"), fractionBindRequest("0.5"), bindingState)
	assert.NoError(t, err)

	assert.Equal(t, "0.5", bindingState.BindingPodAnnotations[portionKey])
}

func TestPreBindWritesNoComputePortionForWholeGpuAllocation(t *testing.T) {
	bindRequest := &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			ReceivedResourceType: bindercommon.ReceivedTypeRegular,
			ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: "1"},
		},
	}
	bindingState := &state.BindingState{ReservedGPUIds: []string{"0"}}

	err := New(false, oneGiBInMiB).PreBind(context.Background(), nvFractionsPod(nil),
		gpuNodeWithMemory("2000"), bindRequest, bindingState)
	assert.NoError(t, err)
	assert.Empty(t, bindingState.BindingPodAnnotations)
}

func nvFractionsPod(annotations map[string]string) *v1.Pod {
	if annotations == nil {
		annotations = map[string]string{}
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: annotations},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
	}
}

func gpuNodeWithMemory(memoryMiB string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.NvidiaGpuMemory: memoryMiB}},
	}
}

func fractionBindRequest(portion string) *v1alpha2.BindRequest {
	return &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			ReceivedResourceType: bindercommon.ReceivedTypeFraction,
			ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: portion},
		},
	}
}

func fractionBindRequestWithoutGPU() *v1alpha2.BindRequest {
	return &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{ReceivedResourceType: bindercommon.ReceivedTypeFraction},
	}
}

func assertParseableComputePortion(t *testing.T, value string) {
	t.Helper()
	assert.NotContains(t, value, "e", "kai-gpu-fractioning parses the portion as a plain decimal")
	portion, err := strconv.ParseFloat(value, 64)
	assert.NoError(t, err)
	assert.Greater(t, portion, float64(0))
	assert.LessOrEqual(t, portion, float64(1))
}

// The freshly computed request must win over whatever the pod arrived with,
// otherwise a stale or self-declared memory annotation would size the compute
// cap. PreBind cannot reach this ordering today - it only falls back to the
// memory request when no portion was received, and no portion means nothing was
// computed - so pin the helper directly rather than leave the precedence
// unguarded for the next caller.
func TestResolvedMemoryRequestMiBPrefersComputedOverPodAnnotation(t *testing.T) {
	memoryKey := resources.CalcGpuFractionAnnotationForContainer("container-0")
	pod := nvFractionsPod(map[string]string{memoryKey: "1900Mi"})
	bindingState := &state.BindingState{BindingPodAnnotations: map[string]string{memoryKey: "488Mi"}}

	memoryMiB, found := resolvedMemoryRequestMiB(pod, "container-0", bindingState)
	assert.True(t, found)
	assert.Equal(t, uint64(488), memoryMiB)
}

func TestResolvedMemoryRequestMiBFallsBackToPodAnnotation(t *testing.T) {
	memoryKey := resources.CalcGpuFractionAnnotationForContainer("container-0")
	pod := nvFractionsPod(map[string]string{memoryKey: "1900Mi"})

	memoryMiB, found := resolvedMemoryRequestMiB(pod, "container-0", &state.BindingState{})
	assert.True(t, found)
	assert.Equal(t, uint64(1900), memoryMiB)
}
