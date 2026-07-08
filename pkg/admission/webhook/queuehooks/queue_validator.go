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
	// EnforcementNone disables quota validation (default).
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

// enabled reports whether the mode enforces allocation-reduction checks (Warning or Block).
func (m EnforcementMode) enabled() bool {
	return m == EnforcementWarning || m == EnforcementBlock
}

type QueueValidator interface {
	ValidateCreate(ctx context.Context, obj *v2.Queue) (warnings admission.Warnings, err error)
	ValidateUpdate(ctx context.Context, oldObj, newObj *v2.Queue) (warnings admission.Warnings, err error)
	ValidateDelete(ctx context.Context, obj *v2.Queue) (warnings admission.Warnings, err error)
}

type queueValidator struct {
	kubeClient            client.Client
	enableQuotaValidation bool
	mode                  EnforcementMode
}

// NewQueueValidator builds a QueueValidator. enableQuotaValidation turns on non-blocking parent/child
// quota-relationship warnings. mode governs enforcement of updates that reduce a limit below the current
// allocation or a quota below the non-preemptible allocation: None skips the check, Warning reports it as
// an admission warning, and Block rejects the update.
func NewQueueValidator(kubeClient client.Client, enableQuotaValidation bool, mode EnforcementMode) QueueValidator {
	return &queueValidator{
		kubeClient:            kubeClient,
		enableQuotaValidation: enableQuotaValidation,
		mode:                  mode,
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

	// Enforce updates that reduce a limit below what the queue already has allocated, or a quota below its
	// non-preemptible allocation (which the scheduler cannot reclaim), governed by --enforce-quota-violation.
	// Block rejects such an update; Warning surfaces it as an admission warning.
	if v.mode.enabled() {
		if violations := allocationReductionViolations(oldQueue, newQueue); len(violations) > 0 {
			if v.mode == EnforcementBlock {
				return append(warnings, violations...), fmt.Errorf("queue %s update rejected: %s",
					newQueue.Name, strings.Join(violations, "; "))
			}
			warnings = append(warnings, violations...)
		}
	}

	// Opt-in parent/child quota-relationship warnings, governed by --enable-quota-validation.
	if v.enableQuotaValidation {
		if newQueue.Spec.ParentQueue != "" {
			parentWarnings, err := v.validateParentChildQuota(ctx, newQueue)
			if err != nil {
				return append(warnings, parentWarnings...), err
			}
			warnings = append(warnings, parentWarnings...)
		}

		if len(oldQueue.Status.ChildQueues) > 0 {
			childWarnings, err := v.validateChildrenQuotaSum(ctx, newQueue)
			if err != nil {
				return append(warnings, childWarnings...), err
			}
			warnings = append(warnings, childWarnings...)
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

// allocationReductionViolations reports where the update lowers a resource limit below the amount already
// allocated to the queue, or lowers a quota below the amount allocated to non-preemptible workloads (which the
// scheduler cannot reclaim). Only genuine reductions are flagged: an unchanged or increased value never trips
// the check, so edits to unrelated fields on an already-over-limit queue are left alone. Allocation is read
// from the queue's own status (last value persisted by the controller); a missing entry counts as zero, so
// freshly created or unreconciled queues are never flagged. Spec values (millicpu, megabytes, GPU fraction) are
// converted into the status units (cores, bytes, GPU fraction) and both sides are rounded to the queue-metrics
// precision before comparison, so a value set equal to the current allocation is not tripped by float rounding.
func allocationReductionViolations(oldQueue, newQueue *v2.Queue) admission.Warnings {
	newSpec := newQueue.Spec.Resources
	oldSpec := oldQueue.Spec.Resources
	if newSpec == nil || oldSpec == nil {
		return nil
	}
	status := oldQueue.Status

	perResource := []struct {
		name       string
		unit       string
		factor     float64
		newLimit   float64
		oldLimit   float64
		newQuota   float64
		oldQuota   float64
		allocated  float64
		nonPreempt float64
	}{
		{"GPU", "", 1, newSpec.GPU.Limit, oldSpec.GPU.Limit, newSpec.GPU.Quota, oldSpec.GPU.Quota,
			gpuAllocated(status.Allocated), gpuAllocated(status.AllocatedNonPreemptible)},
		{"CPU", " cores", 1.0 / milliCPUToCPU, newSpec.CPU.Limit, oldSpec.CPU.Limit, newSpec.CPU.Quota, oldSpec.CPU.Quota,
			cpuAllocated(status.Allocated), cpuAllocated(status.AllocatedNonPreemptible)},
		{"Memory", " bytes", megabytesToBytes, newSpec.Memory.Limit, oldSpec.Memory.Limit, newSpec.Memory.Quota, oldSpec.Memory.Quota,
			memoryAllocated(status.Allocated), memoryAllocated(status.AllocatedNonPreemptible)},
	}

	var violations admission.Warnings
	for _, r := range perResource {
		if isReduction(r.oldLimit, r.newLimit) {
			newLimit, allocated := round4(r.newLimit*r.factor), round4(r.allocated)
			if newLimit < allocated {
				violations = append(violations, fmt.Sprintf("%s limit (%s%s) is below the currently allocated %s%s",
					r.name, fmtNum(newLimit), r.unit, fmtNum(allocated), r.unit))
			}
		}
		if isReduction(r.oldQuota, r.newQuota) {
			newQuota, nonPreempt := round4(r.newQuota*r.factor), round4(r.nonPreempt)
			if newQuota < nonPreempt {
				violations = append(violations, fmt.Sprintf("%s quota (%s%s) is below the non-preemptible allocation (%s%s)",
					r.name, fmtNum(newQuota), r.unit, fmtNum(nonPreempt), r.unit))
			}
		}
	}
	return violations
}

// isReduction reports whether newValue lowers a resource bound relative to oldValue. Only -1
// (constants.UnlimitedResourceQuantity) is unbounded: setting newValue to -1 removes the bound and is never
// a reduction, while an old value of -1 makes any finite new bound a reduction. A value of 0 is a real bound
// (a hard zero cap / zero quota, matching the scheduler), so lowering a positive bound to 0 is a reduction;
// values below -1 are not unbounded and are compared as ordinary bounds.
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

// gpuAllocated sums every GPU resource in the list, matching by the shared constants.GpuResource suffix
// (e.g. "nvidia.com/gpu", "amd.com/gpu") the same way the scheduler's isGpuResource does. Summing rather
// than returning the first match keeps the value deterministic when a queue's allocation spans multiple
// GPU vendors, since Go map iteration order is randomized.
func gpuAllocated(list v1.ResourceList) float64 {
	var total float64
	for name, quantity := range list {
		if strings.HasSuffix(string(name), constants.GpuResource) {
			total += quantity.AsApproximateFloat64()
		}
	}
	return total
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
