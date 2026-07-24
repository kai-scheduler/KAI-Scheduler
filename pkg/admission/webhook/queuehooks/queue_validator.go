// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queuehooks

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

var queueValidatorLog = logf.Log.WithName("queue-validator")

const missingResourcesError = "resources must be specified"

// OverSubscriptionMode controls how the validator reacts when a descendent
// queue's quota exceeds its parent's quota (or siblings oversubscribe it).
type OverSubscriptionMode string

const (
	// OverSubscriptionModeNone disables the descendent over-subscription checks.
	OverSubscriptionModeNone OverSubscriptionMode = "none"
	// OverSubscriptionModeWarning surfaces violations as admission warnings.
	OverSubscriptionModeWarning OverSubscriptionMode = "warning"
	// OverSubscriptionModeBlock rejects the request when violations are found.
	OverSubscriptionModeBlock OverSubscriptionMode = "block"
)

// ParseOverSubscriptionMode normalizes a raw flag value into an
// OverSubscriptionMode, defaulting to OverSubscriptionModeNone when empty and
// erroring on unknown values.
func ParseOverSubscriptionMode(value string) (OverSubscriptionMode, error) {
	mode := OverSubscriptionMode(value)
	if mode == "" {
		mode = OverSubscriptionModeNone
	}

	switch mode {
	case OverSubscriptionModeNone,
		OverSubscriptionModeWarning,
		OverSubscriptionModeBlock:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid over-subscription mode %q: must be one of none, warning, block", value)
	}
}

type QueueValidator interface {
	ValidateCreate(ctx context.Context, obj *v2.Queue) (warnings admission.Warnings, err error)
	ValidateUpdate(ctx context.Context, oldObj, newObj *v2.Queue) (warnings admission.Warnings, err error)
	ValidateDelete(ctx context.Context, obj *v2.Queue) (warnings admission.Warnings, err error)
}

type queueValidator struct {
	kubeClient           client.Client
	overSubscriptionMode OverSubscriptionMode
}

func NewQueueValidator(kubeClient client.Client, overSubscriptionMode OverSubscriptionMode) QueueValidator {
	return &queueValidator{
		kubeClient:           kubeClient,
		overSubscriptionMode: overSubscriptionMode,
	}
}

func (v *queueValidator) ValidateCreate(ctx context.Context, queue *v2.Queue) (admission.Warnings, error) {
	queueValidatorLog.Info("validate create", "name", queue.Name)

	if queue.Spec.Resources == nil {
		return []string{missingResourcesError}, fmt.Errorf(missingResourcesError)
	}

	if v.overSubscriptionMode == OverSubscriptionModeNone || queue.Spec.ParentQueue == "" {
		return nil, nil
	}

	violations, err := v.validateParentChildQuota(ctx, queue)
	if err != nil {
		return nil, err
	}

	return v.reportViolations(violations)
}

func (v *queueValidator) ValidateUpdate(ctx context.Context, oldQueue, newQueue *v2.Queue) (admission.Warnings, error) {
	queueValidatorLog.Info("validate update", "name", newQueue.Name)

	if newQueue.Spec.Resources == nil {
		return []string{missingResourcesError}, fmt.Errorf(missingResourcesError)
	}

	if v.overSubscriptionMode == OverSubscriptionModeNone {
		return nil, nil
	}

	var violations []string

	if newQueue.Spec.ParentQueue != "" {
		parentViolations, err := v.validateParentChildQuota(ctx, newQueue)
		if err != nil {
			return nil, err
		}
		violations = append(violations, parentViolations...)
	}

	if len(oldQueue.Status.ChildQueues) > 0 {
		childViolations, err := v.validateChildrenQuotaSum(ctx, newQueue)
		if err != nil {
			return nil, err
		}
		violations = append(violations, childViolations...)
	}

	return v.reportViolations(violations)
}

// reportViolations maps collected quota violations to the configured
// enforcement mode: warnings surface them as admission warnings, block rejects
// the request with an aggregated error.
func (v *queueValidator) reportViolations(violations []string) (admission.Warnings, error) {
	if len(violations) == 0 {
		return nil, nil
	}

	if v.overSubscriptionMode == OverSubscriptionModeBlock {
		return nil, fmt.Errorf("queue quota over-subscription: %s", strings.Join(violations, "; "))
	}

	return violations, nil
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
	childGPU := childQueue.Spec.Resources.GPU.Quota
	childMemory := childQueue.Spec.Resources.Memory.Quota
	parentCPU := parentQueue.Spec.Resources.CPU.Quota
	parentGPU := parentQueue.Spec.Resources.GPU.Quota
	parentMemory := parentQueue.Spec.Resources.Memory.Quota

	if quotaExceeds(childCPU, parentCPU) {
		warnings = append(warnings, fmt.Sprintf("child queue CPU quota (%s) exceeds parent queue %s CPU quota (%s)",
			formatQuota(childCPU), parentQueue.Name, formatQuota(parentCPU)))
	}

	if quotaExceeds(childGPU, parentGPU) {
		warnings = append(warnings, fmt.Sprintf("child queue GPU quota (%s) exceeds parent queue %s GPU quota (%s)",
			formatQuota(childGPU), parentQueue.Name, formatQuota(parentGPU)))
	}

	if quotaExceeds(childMemory, parentMemory) {
		warnings = append(warnings, fmt.Sprintf("child queue Memory quota (%s) exceeds parent queue %s Memory quota (%s)",
			formatQuota(childMemory), parentQueue.Name, formatQuota(parentMemory)))
	}

	totalChildrenCPU := childCPU
	totalChildrenGPU := childGPU
	totalChildrenMemory := childMemory
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
			formatQuota(totalChildrenCPU), parentQueue.Name, formatQuota(parentCPU)))
	}

	if quotaExceeds(totalChildrenGPU, parentGPU) {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%s) exceeds parent queue %s GPU quota (%s)",
			formatQuota(totalChildrenGPU), parentQueue.Name, formatQuota(parentGPU)))
	}

	if quotaExceeds(totalChildrenMemory, parentMemory) {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%s) exceeds parent queue %s Memory quota (%s)",
			formatQuota(totalChildrenMemory), parentQueue.Name, formatQuota(parentMemory)))
	}

	return warnings, nil
}

func (v *queueValidator) validateChildrenQuotaSum(ctx context.Context, parentQueue *v2.Queue) (admission.Warnings, error) {
	if parentQueue.Spec.Resources == nil {
		return nil, fmt.Errorf("parent queue %s has no resources defined", parentQueue.Name)
	}

	var warnings []string
	var totalChildrenCPU, totalChildrenGPU, totalChildrenMemory float64

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

		if quotaExceeds(child.Spec.Resources.CPU.Quota, parentQueue.Spec.Resources.CPU.Quota) {
			warnings = append(warnings, fmt.Sprintf("child queue %s CPU quota (%s) exceeds parent CPU quota (%s)",
				childName, formatQuota(child.Spec.Resources.CPU.Quota), formatQuota(parentQueue.Spec.Resources.CPU.Quota)))
		}
	}

	if quotaExceeds(totalChildrenCPU, parentQueue.Spec.Resources.CPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%s) exceeds parent CPU quota (%s)",
			formatQuota(totalChildrenCPU), formatQuota(parentQueue.Spec.Resources.CPU.Quota)))
	}

	if quotaExceeds(totalChildrenGPU, parentQueue.Spec.Resources.GPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%s) exceeds parent GPU quota (%s)",
			formatQuota(totalChildrenGPU), formatQuota(parentQueue.Spec.Resources.GPU.Quota)))
	}

	if quotaExceeds(totalChildrenMemory, parentQueue.Spec.Resources.Memory.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%s) exceeds parent Memory quota (%s)",
			formatQuota(totalChildrenMemory), formatQuota(parentQueue.Spec.Resources.Memory.Quota)))
	}

	return warnings, nil
}

// quotaExceeds reports whether value exceeds limit, treating
// constants.UnlimitedResourceQuantity (-1) as unbounded: an unlimited value
// exceeds any finite limit, and nothing exceeds an unlimited limit.
func quotaExceeds(value, limit float64) bool {
	if limit == constants.UnlimitedResourceQuantity {
		return false
	}
	if value == constants.UnlimitedResourceQuantity {
		return true
	}
	return value > limit
}

// addQuota sums two quota values, treating constants.UnlimitedResourceQuantity
// (-1) as absorbing: any unlimited operand makes the total unlimited.
func addQuota(a, b float64) float64 {
	if a == constants.UnlimitedResourceQuantity || b == constants.UnlimitedResourceQuantity {
		return constants.UnlimitedResourceQuantity
	}
	return a + b
}

// formatQuota renders a quota value for warning messages, printing the
// unlimited sentinel as "unlimited" rather than a meaningless -1.
func formatQuota(value float64) string {
	if value == constants.UnlimitedResourceQuantity {
		return "unlimited"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
