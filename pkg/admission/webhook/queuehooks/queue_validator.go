// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queuehooks

import (
	"context"
	"fmt"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

var queueValidatorLog = logf.Log.WithName("queue-validator")

const (
	missingResourcesError = "resources must be specified"
	// quotaTolerance absorbs floating point drift when summing fractional GPU quotas.
	quotaTolerance = 1e-6
)

type QueueValidator interface {
	ValidateCreate(ctx context.Context, obj *v2.Queue) (warnings admission.Warnings, err error)
	ValidateUpdate(ctx context.Context, oldObj, newObj *v2.Queue) (warnings admission.Warnings, err error)
	ValidateDelete(ctx context.Context, obj *v2.Queue) (warnings admission.Warnings, err error)
}

type queueValidator struct {
	kubeClient            client.Client
	enableQuotaValidation bool
}

func NewQueueValidator(kubeClient client.Client, enableQuotaValidation bool) QueueValidator {
	return &queueValidator{
		kubeClient:            kubeClient,
		enableQuotaValidation: enableQuotaValidation,
	}
}

func (v *queueValidator) ValidateCreate(ctx context.Context, queue *v2.Queue) (admission.Warnings, error) {
	queueValidatorLog.Info("validate create", "name", queue.Name)

	if queue.Spec.Resources == nil {
		return []string{missingResourcesError}, fmt.Errorf(missingResourcesError)
	}

	if !v.enableQuotaValidation || queue.Spec.ParentQueue == "" {
		return nil, nil
	}

	return v.validateParentChildQuota(ctx, queue)
}

func (v *queueValidator) ValidateUpdate(ctx context.Context, oldQueue, newQueue *v2.Queue) (admission.Warnings, error) {
	queueValidatorLog.Info("validate update", "name", newQueue.Name)

	if newQueue.Spec.Resources == nil {
		return []string{missingResourcesError}, fmt.Errorf(missingResourcesError)
	}

	if !v.enableQuotaValidation {
		return nil, nil
	}

	var warnings admission.Warnings

	if newQueue.Spec.ParentQueue != "" {
		parentWarnings, err := v.validateParentChildQuota(ctx, newQueue)
		if err != nil {
			return parentWarnings, err
		}
		warnings = append(warnings, parentWarnings...)
	}

	if len(oldQueue.Status.ChildQueues) > 0 {
		childWarnings, err := v.validateChildrenQuotaSum(ctx, newQueue)
		if err != nil {
			return childWarnings, err
		}
		warnings = append(warnings, childWarnings...)
	}

	return warnings, nil
}

func (v *queueValidator) ValidateDelete(ctx context.Context, queue *v2.Queue) (admission.Warnings, error) {
	queueValidatorLog.Info("validate delete", "name", queue.Name)

	if len(queue.Status.ChildQueues) > 0 {
		return nil, fmt.Errorf("cannot delete queue %s: it has child queues %v", queue.Name, queue.Status.ChildQueues)
	}

	return nil, nil
}

func (v *queueValidator) validateParentChildQuota(ctx context.Context, childQueue *v2.Queue) (admission.Warnings, error) {
	parentQueue := &v2.Queue{}
	err := v.kubeClient.Get(ctx, client.ObjectKey{Name: childQueue.Spec.ParentQueue}, parentQueue)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent queue %s: %w", childQueue.Spec.ParentQueue, err)
	}

	if parentQueue.Spec.Resources == nil {
		return nil, fmt.Errorf("parent queue %s has no resources defined", parentQueue.Name)
	}

	var warnings []string

	childCPU := childQueue.Spec.Resources.CPU.Quota
	parentCPU := parentQueue.Spec.Resources.CPU.Quota
	childGPU := childQueue.Spec.Resources.GPU.Quota
	parentGPU := parentQueue.Spec.Resources.GPU.Quota
	childMemory := childQueue.Spec.Resources.Memory.Quota
	parentMemory := parentQueue.Spec.Resources.Memory.Quota

	if quotaExceeds(childCPU, parentCPU) {
		warnings = append(warnings, fmt.Sprintf("child queue CPU quota (%s) exceeds parent queue %s CPU quota (%s)",
			formatQuota(childCPU, 0), parentQueue.Name, formatQuota(parentCPU, 0)))
	}

	if quotaExceeds(childGPU, parentGPU) {
		warnings = append(warnings, fmt.Sprintf("child queue GPU quota (%s) exceeds parent queue %s GPU quota (%s)",
			formatQuota(childGPU, 2), parentQueue.Name, formatQuota(parentGPU, 2)))
	}

	if quotaExceeds(childMemory, parentMemory) {
		warnings = append(warnings, fmt.Sprintf("child queue Memory quota (%s) exceeds parent queue %s Memory quota (%s)",
			formatQuota(childMemory, 0), parentQueue.Name, formatQuota(parentMemory, 0)))
	}

	totalChildrenCPU := addQuota(0, childCPU)
	totalChildrenGPU := addQuota(0, childGPU)
	totalChildrenMemory := addQuota(0, childMemory)
	for _, childName := range parentQueue.Status.ChildQueues {
		if childName == childQueue.Name {
			continue
		}

		existingChild := &v2.Queue{}
		if err := v.kubeClient.Get(ctx, client.ObjectKey{Name: childName}, existingChild); err != nil {
			queueValidatorLog.Error(err, "failed to get child queue", "child", childName)
			continue
		}

		if existingChild.Spec.Resources != nil {
			totalChildrenCPU = addQuota(totalChildrenCPU, existingChild.Spec.Resources.CPU.Quota)
			totalChildrenGPU = addQuota(totalChildrenGPU, existingChild.Spec.Resources.GPU.Quota)
			totalChildrenMemory = addQuota(totalChildrenMemory, existingChild.Spec.Resources.Memory.Quota)
		}
	}

	if quotaExceeds(totalChildrenCPU, parentCPU) {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%s) exceeds parent queue %s CPU quota (%s)",
			formatQuota(totalChildrenCPU, 0), parentQueue.Name, formatQuota(parentCPU, 0)))
	}

	if quotaExceeds(totalChildrenGPU, parentGPU) {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%s) exceeds parent queue %s GPU quota (%s)",
			formatQuota(totalChildrenGPU, 2), parentQueue.Name, formatQuota(parentGPU, 2)))
	}

	if quotaExceeds(totalChildrenMemory, parentMemory) {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%s) exceeds parent queue %s Memory quota (%s)",
			formatQuota(totalChildrenMemory, 0), parentQueue.Name, formatQuota(parentMemory, 0)))
	}

	return warnings, nil
}

func (v *queueValidator) validateChildrenQuotaSum(ctx context.Context, parentQueue *v2.Queue) (admission.Warnings, error) {
	if parentQueue.Spec.Resources == nil {
		return nil, fmt.Errorf("parent queue %s has no resources defined", parentQueue.Name)
	}

	var warnings []string
	var totalChildrenCPU, totalChildrenGPU, totalChildrenMemory float64

	parentCPU := parentQueue.Spec.Resources.CPU.Quota
	parentGPU := parentQueue.Spec.Resources.GPU.Quota
	parentMemory := parentQueue.Spec.Resources.Memory.Quota

	for _, childName := range parentQueue.Status.ChildQueues {
		child := &v2.Queue{}
		if err := v.kubeClient.Get(ctx, client.ObjectKey{Name: childName}, child); err != nil {
			queueValidatorLog.Error(err, "failed to get child queue", "child", childName)
			continue
		}

		if child.Spec.Resources == nil {
			continue
		}

		totalChildrenCPU = addQuota(totalChildrenCPU, child.Spec.Resources.CPU.Quota)
		totalChildrenGPU = addQuota(totalChildrenGPU, child.Spec.Resources.GPU.Quota)
		totalChildrenMemory = addQuota(totalChildrenMemory, child.Spec.Resources.Memory.Quota)

		if quotaExceeds(child.Spec.Resources.CPU.Quota, parentCPU) {
			warnings = append(warnings, fmt.Sprintf("child queue %s CPU quota (%s) exceeds parent CPU quota (%s)",
				childName, formatQuota(child.Spec.Resources.CPU.Quota, 0), formatQuota(parentCPU, 0)))
		}

		if quotaExceeds(child.Spec.Resources.GPU.Quota, parentGPU) {
			warnings = append(warnings, fmt.Sprintf("child queue %s GPU quota (%s) exceeds parent GPU quota (%s)",
				childName, formatQuota(child.Spec.Resources.GPU.Quota, 2), formatQuota(parentGPU, 2)))
		}

		if quotaExceeds(child.Spec.Resources.Memory.Quota, parentMemory) {
			warnings = append(warnings, fmt.Sprintf("child queue %s Memory quota (%s) exceeds parent Memory quota (%s)",
				childName, formatQuota(child.Spec.Resources.Memory.Quota, 0), formatQuota(parentMemory, 0)))
		}
	}

	if quotaExceeds(totalChildrenCPU, parentCPU) {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%s) exceeds parent CPU quota (%s)",
			formatQuota(totalChildrenCPU, 0), formatQuota(parentCPU, 0)))
	}

	if quotaExceeds(totalChildrenGPU, parentGPU) {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%s) exceeds parent GPU quota (%s)",
			formatQuota(totalChildrenGPU, 2), formatQuota(parentGPU, 2)))
	}

	if quotaExceeds(totalChildrenMemory, parentMemory) {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%s) exceeds parent Memory quota (%s)",
			formatQuota(totalChildrenMemory, 0), formatQuota(parentMemory, 0)))
	}

	return warnings, nil
}

// quotaExceeds treats -1 as unlimited: nothing exceeds an unlimited parent,
// and an unlimited quota always exceeds a finite parent.
func quotaExceeds(quota, parentQuota float64) bool {
	if parentQuota == constants.UnlimitedResourceQuantity {
		return false
	}
	if quota == constants.UnlimitedResourceQuantity {
		return true
	}
	return quota-parentQuota > quotaTolerance
}

// addQuota is absorbing for -1: any unlimited term makes the total unlimited.
func addQuota(total, quota float64) float64 {
	if total == constants.UnlimitedResourceQuantity || quota == constants.UnlimitedResourceQuantity {
		return constants.UnlimitedResourceQuantity
	}
	return total + quota
}

func formatQuota(quota float64, precision int) string {
	if quota == constants.UnlimitedResourceQuantity {
		return "unlimited"
	}
	return strconv.FormatFloat(quota, 'f', precision, 64)
}
