// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingnodevalidation

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"

	kaiv1common "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/gpu_sharing"
)

const (
	fractionalGPUReadyConditionType = v1.NodeConditionType(constants.NvFractionNodeReadyConditionType)
	fractionalGPUReadyReasonMissing = "ConditionNotFound"
)

func Validate(task *pod_info.PodInfo, node *node_info.NodeInfo, ssn *framework.Session) error {
	if err := checkMaxPodsWithGpuGroupReservation(task, node, ssn); err != nil {
		return err
	}

	if err := checkNvFractionalGPUReadyCondition(task, node, isNvFractionsMode(ssn)); err != nil {
		return err
	}

	return nil
}

// Check if the gpu-sharing operator set this node as ready. This is relevant only for NvFractions mode.
func checkNvFractionalGPUReadyCondition(task *pod_info.PodInfo, node *node_info.NodeInfo, nvFractionsMode bool) error {
	if !nvFractionsMode || !task.IsSharedGPURequest() {
		return nil
	}

	conditionStatus := v1.ConditionUnknown
	conditionReason := fractionalGPUReadyReasonMissing
	conditionMessage := ""
	for _, condition := range node.Node.Status.Conditions {
		if condition.Type != fractionalGPUReadyConditionType {
			continue
		}
		if condition.Status == v1.ConditionTrue {
			return nil
		}
		conditionStatus = condition.Status
		if condition.Reason != "" {
			conditionReason = condition.Reason
		}
		conditionMessage = condition.Message
		break
	}

	failureMessage := fmt.Sprintf(
		"node is not ready for fractional GPU scheduling. Condition %s is %s. Reason: %s. Message: %s",
		fractionalGPUReadyConditionType, conditionStatus, conditionReason, conditionMessage,
	)

	return common_info.NewFitError(task.Name, task.Namespace, node.Name, failureMessage)
}

func isNvFractionsMode(ssn *framework.Session) bool {
	return ssn != nil && ssn.SchedulerParams.GpuSharingMode != nil &&
		*ssn.SchedulerParams.GpuSharingMode == kaiv1common.GpuSharingModeNvFractions
}

func checkMaxPodsWithGpuGroupReservation(
	task *pod_info.PodInfo, node *node_info.NodeInfo, ssn *framework.Session) error {
	availablePods := node.IdleVector.Get(resource_info.PodsIndex) + node.ReleasingVector.Get(resource_info.PodsIndex)

	if !task.IsSharedGPURequest() {
		if availablePods > 0 {
			return nil
		}
		return common_info.NewFitError(task.Name, task.Namespace, node.Name, api.NodePodNumberExceeded)
	}

	needsNewGpuGroup := willCreateNewGpuGroup(task, node, ssn)
	if !needsNewGpuGroup {
		return nil
	}

	if availablePods < 2 {
		return common_info.NewFitError(task.Name, task.Namespace, node.Name, api.NodePodNumberExceeded)
	}

	return nil
}

// willCreateNewGpuGroup determines if allocating this task will create a new GPU group
// and thus require a new reservation pod.
func willCreateNewGpuGroup(task *pod_info.PodInfo, node *node_info.NodeInfo, ssn *framework.Session) bool {
	if ssn == nil {
		return true
	}

	fittingGPUs := ssn.FittingGPUs(node, task)
	gpuForSharingImmediate := gpu_sharing.GetNodePreferableGpuForSharing(fittingGPUs, node, task, false)

	if gpuForSharingImmediate != nil && !gpuForSharingImmediate.IsReleasing {
		return containsNewGpuGroup(gpuForSharingImmediate.Groups)
	}

	gpuForSharingPipelined := gpu_sharing.GetNodePreferableGpuForSharing(fittingGPUs, node, task, true)

	if gpuForSharingPipelined != nil {
		return containsNewGpuGroup(gpuForSharingPipelined.Groups)
	}

	// No GPU assignment possible - conservatively assume new group would be needed
	return true
}

// containsNewGpuGroup checks if any of the GPU groups is a newly created one (UUID format).
func containsNewGpuGroup(groups []string) bool {
	for _, gpuGroup := range groups {
		if isNewGpuGroup(gpuGroup) {
			return true
		}
	}
	return false
}

// isNewGpuGroup determines if a GPU group ID represents a new group (UUID) vs an existing one (numeric).
func isNewGpuGroup(gpuGroup string) bool {
	// New GPU groups are UUIDs (e.g., "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx")
	// Existing GPU groups are numeric strings ("0", "1", "2", etc.)
	_, err := strconv.Atoi(gpuGroup)
	return err != nil // If not a number, it's a UUID = new group
}
