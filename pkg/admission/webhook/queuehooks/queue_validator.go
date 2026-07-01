// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queuehooks

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
)

var queueValidatorLog = logf.Log.WithName("queue-validator")

const missingResourcesError = "resources must be specified"

// unlimited is the sentinel value used in Queue resource quota/limit fields to mean "no bound".
const unlimited = -1.0

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

	if resource.Quota < unlimited {
		warnings = append(warnings, fmt.Sprintf("queue %s %s quota (%.2f) is invalid; must be -1 (unlimited) or non-negative",
			queueName, resourceName, resource.Quota))
	}

	if resource.Limit < unlimited {
		warnings = append(warnings, fmt.Sprintf("queue %s %s limit (%.2f) is invalid; must be -1 (unlimited) or non-negative",
			queueName, resourceName, resource.Limit))
	}

	if resource.OverQuotaWeight < 0 {
		warnings = append(warnings, fmt.Sprintf("queue %s %s overQuotaWeight (%.2f) is invalid; must be non-negative",
			queueName, resourceName, resource.OverQuotaWeight))
	}

	if limitBelowQuota(resource.Quota, resource.Limit) {
		warnings = append(warnings, fmt.Sprintf("queue %s %s limit (%.2f) is below its quota (%.2f)",
			queueName, resourceName, resource.Limit, resource.Quota))
	}

	return warnings
}

// limitBelowQuota reports whether a hard limit is set below the guaranteed quota. A value of -1 means
// unlimited on either side; a limit of 0 is treated as unset (capped at the quota) and is not reported.
func limitBelowQuota(quota, limit float64) bool {
	if limit == unlimited || limit == 0 {
		return false
	}
	if quota == unlimited {
		return true
	}
	return limit < quota
}

// exceedsParent reports whether a child value exceeds the parent's. A parent value of -1 (unlimited) is
// never exceeded.
func exceedsParent(childValue, parentValue float64) bool {
	if parentValue == unlimited {
		return false
	}
	return childValue > parentValue
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

	var warnings admission.Warnings
	child := childQueue.Spec.Resources
	parent := parentQueue.Spec.Resources

	if exceedsParent(child.CPU.Quota, parent.CPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue CPU quota (%.0f) exceeds parent queue %s CPU quota (%.0f)",
			child.CPU.Quota, parentQueue.Name, parent.CPU.Quota))
	}
	if exceedsParent(child.GPU.Quota, parent.GPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue GPU quota (%.2f) exceeds parent queue %s GPU quota (%.2f)",
			child.GPU.Quota, parentQueue.Name, parent.GPU.Quota))
	}
	if exceedsParent(child.Memory.Quota, parent.Memory.Quota) {
		warnings = append(warnings, fmt.Sprintf("child queue Memory quota (%.0f) exceeds parent queue %s Memory quota (%.0f)",
			child.Memory.Quota, parentQueue.Name, parent.Memory.Quota))
	}

	totalChildrenCPU := child.CPU.Quota
	totalChildrenGPU := child.GPU.Quota
	totalChildrenMemory := child.Memory.Quota
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
			totalChildrenGPU += existingChild.Spec.Resources.GPU.Quota
			totalChildrenMemory += existingChild.Spec.Resources.Memory.Quota
		}
	}

	if exceedsParent(totalChildrenCPU, parent.CPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%.0f) exceeds parent queue %s CPU quota (%.0f)",
			totalChildrenCPU, parentQueue.Name, parent.CPU.Quota))
	}
	if exceedsParent(totalChildrenGPU, parent.GPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%.2f) exceeds parent queue %s GPU quota (%.2f)",
			totalChildrenGPU, parentQueue.Name, parent.GPU.Quota))
	}
	if exceedsParent(totalChildrenMemory, parent.Memory.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%.0f) exceeds parent queue %s Memory quota (%.0f)",
			totalChildrenMemory, parentQueue.Name, parent.Memory.Quota))
	}

	return warnings, nil
}

func (v *queueValidator) validateChildrenQuotaSum(ctx context.Context, parentQueue *v2.Queue) (admission.Warnings, error) {
	if parentQueue.Spec.Resources == nil {
		return nil, fmt.Errorf("parent queue %s has no resources defined", parentQueue.Name)
	}

	var warnings admission.Warnings
	parent := parentQueue.Spec.Resources
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

		if exceedsParent(child.Spec.Resources.CPU.Quota, parent.CPU.Quota) {
			warnings = append(warnings, fmt.Sprintf("child queue %s CPU quota (%.0f) exceeds parent CPU quota (%.0f)",
				childName, child.Spec.Resources.CPU.Quota, parent.CPU.Quota))
		}
	}

	if exceedsParent(totalChildrenCPU, parent.CPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%.0f) exceeds parent CPU quota (%.0f)",
			totalChildrenCPU, parent.CPU.Quota))
	}

	if exceedsParent(totalChildrenGPU, parent.GPU.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%.2f) exceeds parent GPU quota (%.2f)",
			totalChildrenGPU, parent.GPU.Quota))
	}

	if exceedsParent(totalChildrenMemory, parent.Memory.Quota) {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%.0f) exceeds parent Memory quota (%.0f)",
			totalChildrenMemory, parent.Memory.Quota))
	}

	return warnings, nil
}
