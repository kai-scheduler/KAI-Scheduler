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

const missingResourcesError = "resources must be specified"

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

	if !v.enableQuotaValidation {
		return nil, nil
	}

	warnings := validateResourceValues(queue)

	if queue.Spec.ParentQueue != "" {
		parentWarnings, err := v.validateParentChildQuota(ctx, queue)
		if err != nil {
			return append(warnings, parentWarnings...), err
		}
		warnings = append(warnings, parentWarnings...)
	}

	return warnings, nil
}

func (v *queueValidator) ValidateUpdate(ctx context.Context, oldQueue, newQueue *v2.Queue) (admission.Warnings, error) {
	queueValidatorLog.Info("validate update", "name", newQueue.Name)

	if newQueue.Spec.Resources == nil {
		return []string{missingResourcesError}, fmt.Errorf(missingResourcesError)
	}

	if !v.enableQuotaValidation {
		return nil, nil
	}

	warnings := validateResourceValues(newQueue)

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

	return warnings, nil
}

func (v *queueValidator) ValidateDelete(ctx context.Context, queue *v2.Queue) (admission.Warnings, error) {
	queueValidatorLog.Info("validate delete", "name", queue.Name)

	if len(queue.Status.ChildQueues) > 0 {
		return nil, fmt.Errorf("cannot delete queue %s: it has child queues %v", queue.Name, queue.Status.ChildQueues)
	}

	return nil, nil
}

// isUnlimited reports whether a quota/limit value is the unlimited sentinel (-1).
func isUnlimited(value float64) bool {
	return value == constants.UnlimitedResourceQuantity
}

// fmtNum renders a numeric value without trailing zeros.
func fmtNum(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// fmtQuota renders a quota/limit value for a warning, showing the unlimited sentinel as "unlimited".
func fmtQuota(value float64) string {
	if isUnlimited(value) {
		return "unlimited"
	}
	return fmtNum(value)
}

// exceedsParent reports whether a child value exceeds the parent's, treating -1 as unlimited: an unlimited
// parent is never exceeded, and an unlimited child exceeds any bounded parent.
func exceedsParent(childValue, parentValue float64) bool {
	if isUnlimited(parentValue) {
		return false
	}
	if isUnlimited(childValue) {
		return true
	}
	return childValue > parentValue
}

// childrenSumExceedsParent reports whether the sum of child quotas exceeds the parent's, treating -1 as
// unlimited: an unlimited parent is never exceeded, and any unlimited child makes the whole sum unlimited
// (raw addition would let -1 cancel real usage). The returned string renders the offending sum for a warning.
func childrenSumExceedsParent(childValues []float64, parentValue float64) (bool, string) {
	if isUnlimited(parentValue) {
		return false, ""
	}
	var total float64
	for _, value := range childValues {
		if isUnlimited(value) {
			return true, "unlimited"
		}
		total += value
	}
	if total > parentValue {
		return true, fmtQuota(total)
	}
	return false, ""
}

// validateResourceValues checks a queue's own resource values: that a hard limit is not set below its
// guaranteed quota, and that quota, limit and overQuotaWeight hold valid values. All findings are warnings.
func validateResourceValues(queue *v2.Queue) admission.Warnings {
	var warnings admission.Warnings

	resources := queue.Spec.Resources
	perResource := []struct {
		name     string
		resource v2.QueueResource
	}{
		{"CPU", resources.CPU},
		{"GPU", resources.GPU},
		{"Memory", resources.Memory},
	}

	for _, r := range perResource {
		warnings = append(warnings, checkResourceValues(queue.Name, r.name, r.resource)...)
	}

	return warnings
}

func checkResourceValues(queueName, resourceName string, resource v2.QueueResource) admission.Warnings {
	var warnings admission.Warnings

	if resource.Quota < 0 && !isUnlimited(resource.Quota) {
		warnings = append(warnings, fmt.Sprintf("queue %s %s quota (%s) is invalid; must be -1 (unlimited) or non-negative",
			queueName, resourceName, fmtNum(resource.Quota)))
	}

	if resource.Limit < 0 && !isUnlimited(resource.Limit) {
		warnings = append(warnings, fmt.Sprintf("queue %s %s limit (%s) is invalid; must be -1 (unlimited) or non-negative",
			queueName, resourceName, fmtNum(resource.Limit)))
	}

	if resource.OverQuotaWeight < 0 {
		warnings = append(warnings, fmt.Sprintf("queue %s %s overQuotaWeight (%s) is invalid; must be non-negative",
			queueName, resourceName, fmtNum(resource.OverQuotaWeight)))
	}

	if limitBelowQuota(resource.Quota, resource.Limit) {
		warnings = append(warnings, fmt.Sprintf("queue %s %s limit (%s) is below its quota (%s)",
			queueName, resourceName, fmtQuota(resource.Limit), fmtQuota(resource.Quota)))
	}

	return warnings
}

// limitBelowQuota reports whether a valid hard limit is set below the guaranteed quota. -1 means unlimited on
// either side; a limit of 0 is treated as unset (capped at the quota); an invalid negative limit is reported
// by the value checks instead.
func limitBelowQuota(quota, limit float64) bool {
	if isUnlimited(limit) || limit == 0 || limit < 0 {
		return false
	}
	if isUnlimited(quota) {
		return true
	}
	return limit < quota
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

	child := childQueue.Spec.Resources
	parent := parentQueue.Spec.Resources

	warnings := childExceedsParentWarnings(parentQueue.Name, child, parent)

	cpuValues := []float64{child.CPU.Quota}
	gpuValues := []float64{child.GPU.Quota}
	memoryValues := []float64{child.Memory.Quota}
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
			cpuValues = append(cpuValues, existingChild.Spec.Resources.CPU.Quota)
			gpuValues = append(gpuValues, existingChild.Spec.Resources.GPU.Quota)
			memoryValues = append(memoryValues, existingChild.Spec.Resources.Memory.Quota)
		}
	}

	warnings = append(warnings, childrenSumWarnings(parentQueue.Name, parent, cpuValues, gpuValues, memoryValues)...)

	return warnings, nil
}

func (v *queueValidator) validateChildrenQuotaSum(ctx context.Context, parentQueue *v2.Queue) (admission.Warnings, error) {
	if parentQueue.Spec.Resources == nil {
		return nil, fmt.Errorf("parent queue %s has no resources defined", parentQueue.Name)
	}

	parent := parentQueue.Spec.Resources
	var warnings admission.Warnings
	var cpuValues, gpuValues, memoryValues []float64

	for _, childName := range parentQueue.Status.ChildQueues {
		child := &v2.Queue{}
		if err := v.kubeClient.Get(ctx, client.ObjectKey{Name: childName}, child); err != nil {
			queueValidatorLog.Error(err, "failed to get child queue", "child", childName)
			continue
		}

		if child.Spec.Resources == nil {
			continue
		}

		cpuValues = append(cpuValues, child.Spec.Resources.CPU.Quota)
		gpuValues = append(gpuValues, child.Spec.Resources.GPU.Quota)
		memoryValues = append(memoryValues, child.Spec.Resources.Memory.Quota)

		warnings = append(warnings, namedChildExceedsParentWarnings(childName, child.Spec.Resources, parent)...)
	}

	warnings = append(warnings, childrenSumWarnings(parentQueue.Name, parent, cpuValues, gpuValues, memoryValues)...)

	return warnings, nil
}

// childExceedsParentWarnings returns per-resource warnings when a child's quota exceeds the parent's.
func childExceedsParentWarnings(parentName string, child, parent *v2.QueueResources) admission.Warnings {
	var warnings admission.Warnings
	if exceedsParent(child.CPU.Quota, parent.CPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue CPU quota (%s) exceeds parent queue %s CPU quota (%s)",
			fmtQuota(child.CPU.Quota), parentName, fmtQuota(parent.CPU.Quota)))
	}
	if exceedsParent(child.GPU.Quota, parent.GPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue GPU quota (%s) exceeds parent queue %s GPU quota (%s)",
			fmtQuota(child.GPU.Quota), parentName, fmtQuota(parent.GPU.Quota)))
	}
	if exceedsParent(child.Memory.Quota, parent.Memory.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue Memory quota (%s) exceeds parent queue %s Memory quota (%s)",
			fmtQuota(child.Memory.Quota), parentName, fmtQuota(parent.Memory.Quota)))
	}
	return warnings
}

// namedChildExceedsParentWarnings returns per-resource warnings, tagged with the child's name, when its quota
// exceeds the parent's.
func namedChildExceedsParentWarnings(childName string, child, parent *v2.QueueResources) admission.Warnings {
	var warnings admission.Warnings
	if exceedsParent(child.CPU.Quota, parent.CPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue %s CPU quota (%s) exceeds parent CPU quota (%s)",
			childName, fmtQuota(child.CPU.Quota), fmtQuota(parent.CPU.Quota)))
	}
	if exceedsParent(child.GPU.Quota, parent.GPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue %s GPU quota (%s) exceeds parent GPU quota (%s)",
			childName, fmtQuota(child.GPU.Quota), fmtQuota(parent.GPU.Quota)))
	}
	if exceedsParent(child.Memory.Quota, parent.Memory.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue %s Memory quota (%s) exceeds parent Memory quota (%s)",
			childName, fmtQuota(child.Memory.Quota), fmtQuota(parent.Memory.Quota)))
	}
	return warnings
}

// childrenSumWarnings returns per-resource warnings when the sum of children quotas exceeds the parent's.
func childrenSumWarnings(parentName string, parent *v2.QueueResources, cpuValues, gpuValues, memoryValues []float64) admission.Warnings {
	var warnings admission.Warnings
	if exceeds, sum := childrenSumExceedsParent(cpuValues, parent.CPU.Quota); exceeds {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%s) exceeds parent queue %s CPU quota (%s)",
			sum, parentName, fmtQuota(parent.CPU.Quota)))
	}
	if exceeds, sum := childrenSumExceedsParent(gpuValues, parent.GPU.Quota); exceeds {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%s) exceeds parent queue %s GPU quota (%s)",
			sum, parentName, fmtQuota(parent.GPU.Quota)))
	}
	if exceeds, sum := childrenSumExceedsParent(memoryValues, parent.Memory.Quota); exceeds {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%s) exceeds parent queue %s Memory quota (%s)",
			sum, parentName, fmtQuota(parent.Memory.Quota)))
	}
	return warnings
}
