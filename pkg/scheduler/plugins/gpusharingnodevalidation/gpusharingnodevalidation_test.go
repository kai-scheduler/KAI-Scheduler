// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingnodevalidation

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/api/constants"
	kaiv1common "github.com/kai-scheduler/api/kai/v1/common"
)

func TestCheckMaxPodsWithGpuGroupReservation(t *testing.T) {
	podSlotsErr := common_info.NewFitError("shared-pod", "ns", "node-a", api.NodePodNumberExceeded)
	tests := []struct {
		name        string
		gpuDevices  int64
		podSlots    int64
		expectedErr error
	}{
		{
			name:        "single device fraction with 1 free slot fails",
			gpuDevices:  1,
			podSlots:    1,
			expectedErr: podSlotsErr,
		},
		{
			name:       "single device fraction with 2 free slots passes",
			gpuDevices: 1,
			podSlots:   2,
		},
		{
			name:        "two device fraction with 2 free slots fails",
			gpuDevices:  2,
			podSlots:    2,
			expectedErr: podSlotsErr,
		},
		{
			name:       "two device fraction with 3 free slots passes",
			gpuDevices: 2,
			podSlots:   3,
		},
	}

	for _, tt := range tests {
		for _, withSession := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s (session: %v)", tt.name, withSession), func(t *testing.T) {
				vectorMap := resource_info.NewResourceVectorMap()
				node := newGpuNode("node-a", 2, tt.podSlots, vectorMap)
				task := newMultiFractionTask("shared-pod", tt.gpuDevices, 0.5, vectorMap)

				var ssn *framework.Session
				if withSession {
					ssn = &framework.Session{}
				}

				err := checkMaxPodsWithGpuGroupReservation(task, node, ssn)
				if errorString(err) != errorString(tt.expectedErr) {
					t.Fatalf("checkMaxPodsWithGpuGroupReservation() error:\n%q\nExpected:\n%q",
						errorString(err), errorString(tt.expectedErr))
				}
			})
		}
	}
}

func TestCountNewGpuGroups(t *testing.T) {
	tests := []struct {
		name     string
		groups   []string
		expected int
	}{
		{name: "no groups", expected: 0},
		{name: "only existing groups", groups: []string{"0", "1"}, expected: 0},
		{name: "one new and one existing group", groups: []string{"0", "8f6a7b2e-1c3d-4e5f-9a0b-1c2d3e4f5a6b"}, expected: 1},
		{
			name:     "two new groups",
			groups:   []string{"8f6a7b2e-1c3d-4e5f-9a0b-1c2d3e4f5a6b", "1b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e"},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countNewGpuGroups(tt.groups); got != tt.expected {
				t.Fatalf("countNewGpuGroups(%v) = %d, want %d", tt.groups, got, tt.expected)
			}
		})
	}
}

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
			name: "shared gpu task in NvFractions mode with false ready condition fails with condition reason and message",
			task: &pod_info.PodInfo{
				Name:                "shared-pod",
				Namespace:           "ns",
				ResourceRequestType: pod_info.RequestTypeFraction,
			},
			mode: kaiv1common.GpuSharingModeNvFractions,
			conditions: []v1.NodeCondition{
				newFractionalGPUReadyCondition(v1.ConditionFalse, "DevicePluginNotReady", "device plugin is not ready"),
			},
			expectedErr: common_info.NewFitError("shared-pod", "ns", "node-a",
				"node is not ready for fractional GPU scheduling. Condition gpu-fractioning.nvidia.com/Ready is False. Reason: DevicePluginNotReady. Message: device plugin is not ready"),
		},
		{
			name: "shared gpu task in NvFractions mode with false ready condition and no reason fails without condition-not-found reason",
			task: &pod_info.PodInfo{
				Name:                "shared-pod",
				Namespace:           "ns",
				ResourceRequestType: pod_info.RequestTypeFraction,
			},
			mode: kaiv1common.GpuSharingModeNvFractions,
			conditions: []v1.NodeCondition{
				newFractionalGPUReadyCondition(v1.ConditionFalse, ""),
			},
			expectedErr: common_info.NewFitError("shared-pod", "ns", "node-a",
				"node is not ready for fractional GPU scheduling. Condition gpu-fractioning.nvidia.com/Ready is False. Reason: . Message: "),
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
				"node is not ready for fractional GPU scheduling. Condition gpu-fractioning.nvidia.com/Ready is Unknown. Reason: ConditionNotFound. Message: "),
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
	var nodeErrors []*common_info.TasksFitError
	for _, node := range nodes {
		err := checkNvFractionalGPUReadyCondition(
			task, node, isNvFractionsMode(newSessionWithMode(kaiv1common.GpuSharingModeNvFractions)),
		)
		if err == nil {
			t.Fatalf("checkNvFractionalGPUReadyCondition() on %s = nil, want error", node.Name)
		}
		fitErrors.AddNodeError(err)
		nodeError, ok := err.(*common_info.TasksFitError)
		if !ok {
			t.Fatalf("checkNvFractionalGPUReadyCondition() on %s = %T, want *TasksFitError", node.Name, err)
		}
		nodeErrors = append(nodeErrors, nodeError)
	}

	expectedMessage := "no nodes with enough resources were found: " +
		"1 node is not ready for fractional GPU scheduling. Condition gpu-fractioning.nvidia.com/Ready is False. Reason: DevicePluginNotReady. Message: . \n" +
		"1 node is not ready for fractional GPU scheduling. Condition gpu-fractioning.nvidia.com/Ready is False. Reason: NoConfigMap. Message: ."
	if fitErrors.Error() != expectedMessage {
		t.Fatalf("fitErrors.Error():\n%q\nExpected:\n%q", fitErrors.Error(), expectedMessage)
	}

	expectedDetailedMessage := "\n<node0>: node is not ready for fractional GPU scheduling. Condition gpu-fractioning.nvidia.com/Ready is False. Reason: DevicePluginNotReady. Message: ." +
		"\n<node1>: node is not ready for fractional GPU scheduling. Condition gpu-fractioning.nvidia.com/Ready is False. Reason: NoConfigMap. Message: ." +
		"\nno nodes with enough resources were found."
	if fitErrors.DetailedError(nodeErrors) != expectedDetailedMessage {
		t.Fatalf("fitErrors.DetailedError():\n%q\nExpected:\n%q", fitErrors.DetailedError(nodeErrors), expectedDetailedMessage)
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

func newGpuNode(name string, gpus, pods int64, vectorMap *resource_info.ResourceVectorMap) *node_info.NodeInfo {
	allocatable := v1.ResourceList{
		v1.ResourceCPU:                         resource.MustParse("8"),
		v1.ResourceMemory:                      resource.MustParse("16Gi"),
		v1.ResourceName(constants.GpuResource): *resource.NewQuantity(gpus, resource.DecimalSI),
		v1.ResourcePods:                        *resource.NewQuantity(pods, resource.DecimalSI),
	}
	return node_info.NewNodeInfo(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1.NodeStatus{
			Allocatable: allocatable,
			Capacity:    allocatable,
		},
	}, nil, vectorMap)
}

func newMultiFractionTask(
	name string, devices int64, portion float64, vectorMap *resource_info.ResourceVectorMap,
) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		Name:                name,
		Namespace:           "ns",
		ResourceRequestType: pod_info.RequestTypeFraction,
		GpuRequirement:      *resource_info.NewGpuResourceRequirementWithMultiFraction(devices, portion, 0),
		ResReqVector:        resource_info.NewResourceVector(vectorMap),
	}
}

func newFractionalGPUReadyCondition(status v1.ConditionStatus, reason string, message ...string) v1.NodeCondition {
	conditionMessage := ""
	if len(message) > 0 {
		conditionMessage = message[0]
	}
	return v1.NodeCondition{
		Type:    fractionalGPUReadyConditionType,
		Status:  status,
		Reason:  reason,
		Message: conditionMessage,
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
