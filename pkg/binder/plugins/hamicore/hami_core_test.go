// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package hamicore

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestPreBindFailsClosedWhenMemoryLimitCannotBeCalculated(t *testing.T) {
	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "c"}},
		},
	}
	node := &v1.Node{} // missing constants.NvidiaGpuMemory label
	bindRequest := &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			ReceivedResourceType: common.ReceivedTypeFraction,
			ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: "0.5"},
		},
	}

	plugin := New(nil)
	err := plugin.PreBind(t.Context(), pod, node, bindRequest, nil)

	if err == nil {
		t.Fatalf("expected PreBind to fail closed when node lacks %s label, got nil error",
			constants.NvidiaGpuMemory)
	}
}
