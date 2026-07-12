// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queuehooks

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	commonresources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

var queueValidatorLog = logf.Log.WithName("queue-validator")

const (
	missingResourcesError = "resources must be specified"

	// Spec quotas/limits use millicpu for CPU and megabytes for Memory, while the queue status stores
	// allocation in cores and bytes; these convert spec values into the status units for comparison.
	milliCPUToCPU    = 1000
	megabytesToBytes = 1000000
)

// EnforcementMode selects how strictly the queue validator treats quota and limit violations.
type EnforcementMode string

const (
	// EnforcementNone disables allocation-reduction enforcement (default).
	EnforcementNone EnforcementMode = "None"
	// EnforcementWarning surfaces quota and limit violations as admission warnings.
	EnforcementWarning EnforcementMode = "Warning"
	// EnforcementBlock rejects updates that reduce a limit below the current allocation or a quota below the
	// non-preemptible allocation.
	EnforcementBlock EnforcementMode = "Block"
)

// ParseEnforcementMode normalizes a mode string (case-insensitive); an empty string maps to None.
func ParseEnforcementMode(s string) (EnforcementMode, error) {
	switch strings.ToLower(s) {
	case "", "none":
		return EnforcementNone, nil
	case "warning":
		return EnforcementWarning, nil
	case "block":
		return EnforcementBlock, nil
	default:
		return "", fmt.Errorf("invalid quota enforcement mode %q: must be one of None, Warning, Block", s)
	}
}

type QueueValidator interface {
	ValidateCreate(ctx context.Context, obj *v2.Queue) (warnings admission.Warnings, err error)
	ValidateUpdate(ctx context.Context, oldObj, newObj *v2.Queue) (warnings admission.Warnings, err error)
	ValidateDelete(ctx context.Context, obj *v2.Queue) (warnings admission.Warnings, err error)
}

type queueValidator struct {
	kubeClient            client.Client
	enableQuotaValidation bool
	quotaViolationMode    EnforcementMode
}

// NewQueueValidator builds a QueueValidator. enableQuotaValidation turns on the non-blocking parent/child
// quota-relationship warnings; quotaViolationMode governs the allocation-reduction check. Any mode other than
// Warning or Block, including the zero value, is normalized to None so that the check stays off by default.
func NewQueueValidator(kubeClient client.Client, enableQuotaValidation bool, quotaViolationMode EnforcementMode) QueueValidator {
	if quotaViolationMode != EnforcementWarning && quotaViolationMode != EnforcementBlock {
		quotaViolationMode = EnforcementNone
	}
	return &queueValidator{
		kubeClient:            kubeClient,
		enableQuotaValidation: enableQuotaValidation,
		quotaViolationMode:    quotaViolationMode,
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

	var warnings admission.Warnings

	if v.quotaViolationMode != EnforcementNone {
		if violations := allocationReductionViolations(oldQueue, newQueue); len(violations) > 0 {
			if v.quotaViolationMode == EnforcementBlock {
				return append(warnings, violations...), fmt.Errorf("queue %s update rejected: %s",
					newQueue.Name, strings.Join(violations, "; "))
			}
			warnings = append(warnings, violations...)
		}
	}

	if v.enableQuotaValidation {
		quotaWarnings, err := v.parentChildQuotaWarnings(ctx, oldQueue, newQueue)
		warnings = append(warnings, quotaWarnings...)
		if err != nil {
			return warnings, err
		}
	}

	return warnings, nil
}

func (v *queueValidator) parentChildQuotaWarnings(
	ctx context.Context, oldQueue, newQueue *v2.Queue,
) (admission.Warnings, error) {
	var warnings admission.Warnings

	if newQueue.Spec.ParentQueue != "" {
		parentWarnings, err := v.validateParentChildQuota(ctx, newQueue)
		warnings = append(warnings, parentWarnings...)
		if err != nil {
			return warnings, err
		}
	}

	if len(oldQueue.Status.ChildQueues) > 0 {
		childWarnings, err := v.validateChildrenQuotaSum(ctx, newQueue)
		warnings = append(warnings, childWarnings...)
		if err != nil {
			return warnings, err
		}
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

const (
	limitViolationFormat = "%s limit (%s%s) is below the currently allocated %s%s"
	quotaViolationFormat = "%s quota (%s%s) is below the non-preemptible allocation (%s%s)"
)

// allocationCheck is a single bound compared against the allocation it must not fall below. factor converts
// the spec value (millicpu, megabytes) into the status unit (cores, bytes).
type allocationCheck struct {
	resource  string
	unit      string
	format    string
	factor    float64
	oldValue  float64
	newValue  float64
	allocated float64
}

// allocationReductionViolations reports updates that lower a limit below the queue's current allocation, or a
// quota below its non-preemptible allocation. Only genuine reductions are flagged, and both sides are rounded
// to the queue-metrics precision so a value set equal to the allocation is not tripped by float rounding.
func allocationReductionViolations(oldQueue, newQueue *v2.Queue) admission.Warnings {
	newSpec := newQueue.Spec.Resources
	oldSpec := oldQueue.Spec.Resources
	if newSpec == nil || oldSpec == nil {
		return nil
	}
	status := oldQueue.Status

	checks := []allocationCheck{
		{"GPU", "", limitViolationFormat, 1,
			oldSpec.GPU.Limit, newSpec.GPU.Limit, gpuAllocated(status.Allocated)},
		{"GPU", "", quotaViolationFormat, 1,
			oldSpec.GPU.Quota, newSpec.GPU.Quota, gpuAllocated(status.AllocatedNonPreemptible)},
		{"CPU", " cores", limitViolationFormat, 1.0 / milliCPUToCPU,
			oldSpec.CPU.Limit, newSpec.CPU.Limit, cpuAllocated(status.Allocated)},
		{"CPU", " cores", quotaViolationFormat, 1.0 / milliCPUToCPU,
			oldSpec.CPU.Quota, newSpec.CPU.Quota, cpuAllocated(status.AllocatedNonPreemptible)},
		{"Memory", " bytes", limitViolationFormat, megabytesToBytes,
			oldSpec.Memory.Limit, newSpec.Memory.Limit, memoryAllocated(status.Allocated)},
		{"Memory", " bytes", quotaViolationFormat, megabytesToBytes,
			oldSpec.Memory.Quota, newSpec.Memory.Quota, memoryAllocated(status.AllocatedNonPreemptible)},
	}

	var violations admission.Warnings
	for _, c := range checks {
		if !isReduction(c.oldValue, c.newValue) {
			continue
		}
		newValue, allocated := round4(c.newValue*c.factor), round4(c.allocated)
		if newValue < allocated {
			violations = append(violations, fmt.Sprintf(c.format, c.resource,
				fmtNum(newValue), c.unit, fmtNum(allocated), c.unit))
		}
	}
	return violations
}

// isReduction reports whether newValue lowers a resource bound. Only -1
// (constants.UnlimitedResourceQuantity) is unbounded: a new -1 is never a reduction, an old -1 makes any
// finite new value one, and 0 is a real bound (so lowering a positive bound to 0 is a reduction).
func isReduction(oldValue, newValue float64) bool {
	if newValue == constants.UnlimitedResourceQuantity {
		return false
	}
	if oldValue == constants.UnlimitedResourceQuantity {
		return true
	}
	return newValue < oldValue
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func gpuAllocated(list v1.ResourceList) float64 {
	return commonresources.SumGpuAllocation(list)
}

func cpuAllocated(list v1.ResourceList) float64 {
	return quantityValue(list, v1.ResourceCPU)
}

func memoryAllocated(list v1.ResourceList) float64 {
	return quantityValue(list, v1.ResourceMemory)
}

func quantityValue(list v1.ResourceList, name v1.ResourceName) float64 {
	if quantity, ok := list[name]; ok {
		return quantity.AsApproximateFloat64()
	}
	return 0
}

func fmtNum(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
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

	if childCPU > parentCPU {
		warnings = append(warnings, fmt.Sprintf("child queue CPU quota (%.0f) exceeds parent queue %s CPU quota (%.0f)",
			childCPU, parentQueue.Name, parentCPU))
	}

	totalChildrenCPU := childCPU
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
			totalChildrenCPU += existingChild.Spec.Resources.CPU.Quota
		}
	}

	if totalChildrenCPU > parentCPU {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%.0f) exceeds parent queue %s CPU quota (%.0f)",
			totalChildrenCPU, parentQueue.Name, parentCPU))
	}

	if childQueue.Spec.Resources.GPU.Quota > parentQueue.Spec.Resources.GPU.Quota {
		warnings = append(warnings, fmt.Sprintf("child queue GPU quota (%.2f) exceeds parent queue %s GPU quota (%.2f)",
			childQueue.Spec.Resources.GPU.Quota, parentQueue.Name, parentQueue.Spec.Resources.GPU.Quota))
	}

	if childQueue.Spec.Resources.Memory.Quota > parentQueue.Spec.Resources.Memory.Quota {
		warnings = append(warnings, fmt.Sprintf("child queue Memory quota (%.0f) exceeds parent queue %s Memory quota (%.0f)",
			childQueue.Spec.Resources.Memory.Quota, parentQueue.Name, parentQueue.Spec.Resources.Memory.Quota))
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

		totalChildrenCPU += child.Spec.Resources.CPU.Quota
		totalChildrenGPU += child.Spec.Resources.GPU.Quota
		totalChildrenMemory += child.Spec.Resources.Memory.Quota

		if child.Spec.Resources.CPU.Quota > parentQueue.Spec.Resources.CPU.Quota {
			warnings = append(warnings, fmt.Sprintf("child queue %s CPU quota (%.0f) exceeds parent CPU quota (%.0f)",
				childName, child.Spec.Resources.CPU.Quota, parentQueue.Spec.Resources.CPU.Quota))
		}
	}

	if totalChildrenCPU > parentQueue.Spec.Resources.CPU.Quota {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%.0f) exceeds parent CPU quota (%.0f)",
			totalChildrenCPU, parentQueue.Spec.Resources.CPU.Quota))
	}

	if totalChildrenGPU > parentQueue.Spec.Resources.GPU.Quota {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%.2f) exceeds parent GPU quota (%.2f)",
			totalChildrenGPU, parentQueue.Spec.Resources.GPU.Quota))
	}

	if totalChildrenMemory > parentQueue.Spec.Resources.Memory.Quota {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%.0f) exceeds parent Memory quota (%.0f)",
			totalChildrenMemory, parentQueue.Spec.Resources.Memory.Quota))
	}

	return warnings, nil
}
