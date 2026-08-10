// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingnodevalidation

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kaiv1common "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestCheckNvFractionalGPUReadyCondition(t *testing.T) {
	tests := []struct {
		name        string
		task        *pod_info.PodInfo
		mode        kaiv1common.GpuSharingMode
		conditions  []v1.NodeCondition
		expectedErr error
	}{
		{
			name: "shared gpu task in NvFractions mode with ready condition passes",
			task: &pod_info.PodInfo{
				Name:                "shared-pod",
				Namespace:           "ns",
				ResourceRequestType: pod_info.RequestTypeFraction,
			},
			mode: kaiv1common.GpuSharingModeNvFractions,
			conditions: []v1.NodeCondition{
				newFractionalGPUReadyCondition(v1.ConditionTrue, ""),
			},
		},
		{
			name: "shared gpu task in NvFractions mode with false ready condition fails with condition reason",
			task: &pod_info.PodInfo{
				Name:                "shared-pod",
				Namespace:           "ns",
				ResourceRequestType: pod_info.RequestTypeFraction,
			},
			mode: kaiv1common.GpuSharingModeNvFractions,
			conditions: []v1.NodeCondition{
				newFractionalGPUReadyCondition(v1.ConditionFalse, "DevicePluginNotReady"),
			},
			expectedErr: common_info.NewFitError("shared-pod", "ns", "node-a",
				"node is not ready for fractional GPU scheduling. Condition gpu-sharing.nvidia.com/Ready is False. Reason: DevicePluginNotReady"),
		},
		{
			name: "shared gpu task in NvFractions mode with missing ready condition fails",
			task: &pod_info.PodInfo{
				Name:                "shared-pod",
				Namespace:           "ns",
				ResourceRequestType: pod_info.RequestTypeFraction,
			},
			mode: kaiv1common.GpuSharingModeNvFractions,
			expectedErr: common_info.NewFitError("shared-pod", "ns", "node-a",
				"node is not ready for fractional GPU scheduling. Condition gpu-sharing.nvidia.com/Ready is Unknown. Reason: ConditionNotFound"),
		},
		{
			name: "NvFractions-looking pod ignores false ready condition when config is not NvFractions mode",
			task: &pod_info.PodInfo{
				Name:                "shared-pod",
				Namespace:           "ns",
				ResourceRequestType: pod_info.RequestTypeFraction,
			},
			mode: kaiv1common.GpuSharingModeNonMemoryEnforced,
			conditions: []v1.NodeCondition{
				newFractionalGPUReadyCondition(v1.ConditionFalse, "DevicePluginNotReady"),
			},
		},
		{
			name: "whole gpu task ignores false fractional ready condition",
			mode: kaiv1common.GpuSharingModeNvFractions,
			task: &pod_info.PodInfo{
				Name:                "whole-gpu-pod",
				Namespace:           "ns",
				ResourceRequestType: pod_info.RequestTypeRegular,
				GpuRequirement:      *resource_info.NewGpuResourceRequirementWithGpus(1, 0),
			},
			conditions: []v1.NodeCondition{
				newFractionalGPUReadyCondition(v1.ConditionFalse, "DevicePluginNotReady"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newNode("node-a", tt.conditions...)

			err := checkNvFractionalGPUReadyCondition(tt.task, node, isNvFractionsMode(newSessionWithMode(tt.mode)))
			if errorString(err) != errorString(tt.expectedErr) {
				t.Fatalf(
					"checkNvFractionalGPUReadyCondition() error:\n%q\nExpected:\n%q",
					errorString(err), errorString(tt.expectedErr),
				)
			}
		})
	}
}

func TestFractionalGPUReadyConditionUnschedulableMessageForTwoNodes(t *testing.T) {
	task := &pod_info.PodInfo{
		Name:                "shared-pod",
		Namespace:           "ns",
		ResourceRequestType: pod_info.RequestTypeFraction,
	}
	nodes := []*node_info.NodeInfo{
		newNode("node0", newFractionalGPUReadyCondition(v1.ConditionFalse, "DevicePluginNotReady")),
		newNode("node1", newFractionalGPUReadyCondition(v1.ConditionFalse, "NoConfigMap")),
	}

	fitErrors := common_info.NewFitErrors()
	for _, node := range nodes {
		err := checkNvFractionalGPUReadyCondition(
			task, node, isNvFractionsMode(newSessionWithMode(kaiv1common.GpuSharingModeNvFractions)),
		)
		if err == nil {
			t.Fatalf("checkNvFractionalGPUReadyCondition() on %s = nil, want error", node.Name)
		}
		fitErrors.SetNodeError(node.Name, err)
	}

	expectedMessage := "no nodes with enough resources were found: " +
		"1 node is not ready for fractional GPU scheduling. Condition gpu-sharing.nvidia.com/Ready is False. Reason: DevicePluginNotReady. \n" +
		"1 node is not ready for fractional GPU scheduling. Condition gpu-sharing.nvidia.com/Ready is False. Reason: NoConfigMap."
	if fitErrors.Error() != expectedMessage {
		t.Fatalf("fitErrors.Error():\n%q\nExpected:\n%q", fitErrors.Error(), expectedMessage)
	}

	expectedDetailedMessage := "\n<node0>: node is not ready for fractional GPU scheduling. Condition gpu-sharing.nvidia.com/Ready is False. Reason: DevicePluginNotReady." +
		"\n<node1>: node is not ready for fractional GPU scheduling. Condition gpu-sharing.nvidia.com/Ready is False. Reason: NoConfigMap." +
		"\nno nodes with enough resources were found."
	if fitErrors.DetailedError() != expectedDetailedMessage {
		t.Fatalf("fitErrors.DetailedError():\n%q\nExpected:\n%q", fitErrors.DetailedError(), expectedDetailedMessage)
	}
}

func newSessionWithMode(mode kaiv1common.GpuSharingMode) *framework.Session {
	return &framework.Session{
		SchedulerParams: conf.SchedulerParams{
			GpuSharingMode: ptr.To(mode),
		},
	}
}

func newNode(name string, conditions ...v1.NodeCondition) *node_info.NodeInfo {
	return &node_info.NodeInfo{
		Name: name,
		Node: &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: v1.NodeStatus{
				Conditions: conditions,
			},
		},
	}
}

func newFractionalGPUReadyCondition(status v1.ConditionStatus, reason string) v1.NodeCondition {
	return v1.NodeCondition{
		Type:   fractionalGPUReadyConditionType,
		Status: status,
		Reason: reason,
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
