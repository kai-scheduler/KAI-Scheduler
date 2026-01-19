// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queuehooks

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
)

var queueValidatorLog = logf.Log.WithName("queue-validator")

const missingResourcesError = "resources must be specified"

type QueueValidator interface {
	ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error)
	ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error)
	ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error)
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

func (v *queueValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	queue, ok := obj.(*v2.Queue)
	if !ok {
		return nil, fmt.Errorf("expected a Queue but got a %T", obj)
	}
	queueValidatorLog.Info("validate create", "name", queue.Name)

	if queue.Spec.Resources == nil {
		return []string{missingResourcesError}, fmt.Errorf(missingResourcesError)
	}

	if !v.enableQuotaValidation || queue.Spec.ParentQueue == "" {
		return nil, nil
	}

	return v.validateParentChildQuota(ctx, queue)
}

func (v *queueValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldQueue, ok := oldObj.(*v2.Queue)
	if !ok {
		return nil, fmt.Errorf("expected a Queue but got a %T", oldObj)
	}

	newQueue, ok := newObj.(*v2.Queue)
	if !ok {
		return nil, fmt.Errorf("expected a Queue but got a %T", newObj)
	}
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

func (v *queueValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	queue, ok := obj.(*v2.Queue)
	if !ok {
		return nil, fmt.Errorf("expected a Queue but got a %T", obj)
	}
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
