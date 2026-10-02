// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"context"
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

	err := New(false).PreBind(context.Background(), pod, node, bindRequest, bindingState)
	assert.NoError(t, err)

	memoryKey := resources.CalcGpuFractionAnnotationForContainer("container-0")
	visibleDevicesKey := resources.CalcGpuVisibleDevicesAnnotationForContainer("container-0")
	assert.NotEmpty(t, bindingState.BindingPodAnnotations[memoryKey])
	assert.Equal(t, "0,1", bindingState.BindingPodAnnotations[visibleDevicesKey])
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

	err := New(true).PreBind(context.Background(), pod, node, bindRequest, bindingState)
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

	err := New(false).PreBind(context.Background(), pod, node, bindRequest, bindingState)
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

	err := New(false).PreBind(context.Background(), pod, node, bindRequest, bindingState)
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

	err := New(false).PreBind(context.Background(), pod, node, bindRequest, bindingState)
	assert.NoError(t, err)
	assert.Empty(t, bindingState.BindingPodAnnotations)
}
