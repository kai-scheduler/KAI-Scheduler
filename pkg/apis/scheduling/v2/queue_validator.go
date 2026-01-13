// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var queueValidatorLog = logf.Log.WithName("queue-validator")

type QueueValidator interface {
	ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error)
	ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error)
	ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error)
}

type queueValidator struct {
	kubeClient client.Client
	enableQuotaValidation bool
}

func NewQueueValidator(kubeClient client.Client, enableQuotaValidation bool) QueueValidator {
	return &queueValidator{
		kubeClient: kubeClient,
		enableQuotaValidation: enableQuotaValidation,
	}
}

func (v *queueValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	queue, ok := obj.(*Queue)
	if !ok {
		return nil, fmt.Errorf("expected a Queue but got a %T", obj)
	}
	queueValidatorLog.Info("validate create", "name", queue.Name)

	if queue.Spec.Resources == nil {
		return []string{missingResourcesError}, fmt.Errorf(missingResourcesError)
	}

	// Validate parent-child CPU quota relationship if enabled
	if v.enableQuotaValidation && queue.Spec.ParentQueue != "" {
		return v.validateParentChildQuota(ctx, queue)
	}

	return nil, nil
}

func (v *queueValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldQueue, ok := oldObj.(*Queue)
	if !ok {
		return nil, fmt.Errorf("expected a Queue but got a %T", oldObj)
	}
	
	newQueue, ok := newObj.(*Queue)
	if !ok {
		return nil, fmt.Errorf("expected a Queue but got a %T", newObj)
	}
	queueValidatorLog.Info("validate update", "name", newQueue.Name)

	if newQueue.Spec.Resources == nil {
		return []string{missingResourcesError}, fmt.Errorf(missingResourcesError)
	}

	var warnings admission.Warnings

	// Validate parent-child CPU quota relationship if enabled and parent exists
	if v.enableQuotaValidation && newQueue.Spec.ParentQueue != "" {
		parentWarnings, err := v.validateParentChildQuota(ctx, newQueue)
		if err != nil {
			return parentWarnings, err
		}
		warnings = append(warnings, parentWarnings...)
	}

	// If enabled and this queue has children, validate that new quota >= sum of children quotas
	if v.enableQuotaValidation && len(oldQueue.Status.ChildQueues) > 0 {
		childWarnings, err := v.validateChildrenQuotaSum(ctx, newQueue)
		if err != nil {
			return childWarnings, err
		}
		warnings = append(warnings, childWarnings...)
	}

	return warnings, nil
}

func (v *queueValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	queue, ok := obj.(*Queue)
	if !ok {
		return nil, fmt.Errorf("expected a Queue but got a %T", obj)
	}
	queueValidatorLog.Info("validate delete", "name", queue.Name)
	
	// Prevent deletion if queue has children
	if len(queue.Status.ChildQueues) > 0 {
		return nil, fmt.Errorf("cannot delete queue %s: it has child queues %v", queue.Name, queue.Status.ChildQueues)
	}
	
	return nil, nil
}

func (v *queueValidator) validateParentChildQuota(ctx context.Context, childQueue *Queue) (admission.Warnings, error) {
	// Fetch parent queue
	parentQueue := &Queue{}
	err := v.kubeClient.Get(ctx, client.ObjectKey{Name: childQueue.Spec.ParentQueue}, parentQueue)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent queue %s: %w", childQueue.Spec.ParentQueue, err)
	}

	// Validate parent has resources defined
	if parentQueue.Spec.Resources == nil {
		return nil, fmt.Errorf("parent queue %s has no resources defined", parentQueue.Name)
	}

	warnings := []string{}

	// Validate child CPU quota <= parent CPU quota
	childCPU := childQueue.Spec.Resources.CPU.Quota
	parentCPU := parentQueue.Spec.Resources.CPU.Quota
	
	if childCPU > parentCPU {
		warnings = append(warnings, fmt.Sprintf("child queue CPU quota (%.0f) exceeds parent queue %s CPU quota (%.0f) - over-provisioning detected", 
			childCPU, parentQueue.Name, parentCPU))
	}

	// Calculate sum of all children's CPU quotas
	totalChildrenCPU := childCPU
	for _, childName := range parentQueue.Status.ChildQueues {
		// Skip if this is the current queue being validated (for updates)
		if childName == childQueue.Name {
			continue
		}
		
		existingChild := &Queue{}
		if err := v.kubeClient.Get(ctx, client.ObjectKey{Name: childName}, existingChild); err != nil {
			queueValidatorLog.Error(err, "failed to get child queue", "child", childName)
			continue
		}
		
		if existingChild.Spec.Resources != nil {
			totalChildrenCPU += existingChild.Spec.Resources.CPU.Quota
		}
	}

	// Check if total children CPU quotas exceed parent CPU quota
	if totalChildrenCPU > parentCPU {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%.0f) exceeds parent queue %s CPU quota (%.0f) - over-provisioning detected", 
			totalChildrenCPU, parentQueue.Name, parentCPU))
	}

	// Also check GPU and Memory quotas
	if childQueue.Spec.Resources.GPU.Quota > parentQueue.Spec.Resources.GPU.Quota {
		warnings = append(warnings, fmt.Sprintf("child queue GPU quota (%.2f) exceeds parent queue %s GPU quota (%.2f) - over-provisioning detected", 
			childQueue.Spec.Resources.GPU.Quota, parentQueue.Name, parentQueue.Spec.Resources.GPU.Quota))
	}

	if childQueue.Spec.Resources.Memory.Quota > parentQueue.Spec.Resources.Memory.Quota {
		warnings = append(warnings, fmt.Sprintf("child queue Memory quota (%.0f) exceeds parent queue %s Memory quota (%.0f) - over-provisioning detected", 
			childQueue.Spec.Resources.Memory.Quota, parentQueue.Name, parentQueue.Spec.Resources.Memory.Quota))
	}

	return warnings, nil
}

func (v *queueValidator) validateChildrenQuotaSum(ctx context.Context, parentQueue *Queue) (admission.Warnings, error) {
	warnings := []string{}
	
	if parentQueue.Spec.Resources == nil {
		return nil, fmt.Errorf("parent queue %s has no resources defined", parentQueue.Name)
	}

	totalChildrenCPU := float64(0)
	totalChildrenGPU := float64(0)
	totalChildrenMemory := float64(0)

	// Calculate sum of all children quotas
	for _, childName := range parentQueue.Status.ChildQueues {
		child := &Queue{}
		if err := v.kubeClient.Get(ctx, client.ObjectKey{Name: childName}, child); err != nil {
			queueValidatorLog.Error(err, "failed to get child queue", "child", childName)
			continue
		}
		
		if child.Spec.Resources != nil {
			totalChildrenCPU += child.Spec.Resources.CPU.Quota
			totalChildrenGPU += child.Spec.Resources.GPU.Quota
			totalChildrenMemory += child.Spec.Resources.Memory.Quota
			
			// Check if individual child exceeds parent
			if child.Spec.Resources.CPU.Quota > parentQueue.Spec.Resources.CPU.Quota {
				warnings = append(warnings, fmt.Sprintf("child queue %s CPU quota (%.0f) exceeds parent CPU quota (%.0f) - over-provisioning detected", 
					childName, child.Spec.Resources.CPU.Quota, parentQueue.Spec.Resources.CPU.Quota))
			}
		}
	}

	// Check if total children quotas exceed parent
	if totalChildrenCPU > parentQueue.Spec.Resources.CPU.Quota {
		warnings = append(warnings, fmt.Sprintf("total children CPU quota (%.0f) exceeds parent CPU quota (%.0f) - over-provisioning detected", 
			totalChildrenCPU, parentQueue.Spec.Resources.CPU.Quota))
	}

	if totalChildrenGPU > parentQueue.Spec.Resources.GPU.Quota {
		warnings = append(warnings, fmt.Sprintf("total children GPU quota (%.2f) exceeds parent GPU quota (%.2f) - over-provisioning detected", 
			totalChildrenGPU, parentQueue.Spec.Resources.GPU.Quota))
	}

	if totalChildrenMemory > parentQueue.Spec.Resources.Memory.Quota {
		warnings = append(warnings, fmt.Sprintf("total children Memory quota (%.0f) exceeds parent Memory quota (%.0f) - over-provisioning detected", 
			totalChildrenMemory, parentQueue.Spec.Resources.Memory.Quota))
	}

	return warnings, nil
}