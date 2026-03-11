// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_reconciler

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/operator/operands/deployable"
)

type objectWithConditions interface {
	client.Object
	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
	DeepCopy() objectWithConditions
	GetInternalObject() client.Object
}

type StatusReconciler struct {
	client.Client
	deployable deployable.Deployable
}

func New(client client.Client, deployable *deployable.DeployableOperands) *StatusReconciler {
	return &StatusReconciler{
		deployable: deployable,
		Client:     client,
	}
}

func (r *StatusReconciler) UpdateStartReconcileStatus(ctx context.Context, object objectWithConditions) error {
	if err := r.reconcileCondition(ctx, object, r.getReconcilingCondition(object.GetGeneration(), true)); err != nil {
		return err
	}

	return r.reconcileCondition(ctx, object, r.getDeployedCondition(ctx, object.GetGeneration()))
}

func (r *StatusReconciler) ReconcileStatus(ctx context.Context, object objectWithConditions) error {
	if err := r.reconcileCondition(ctx, object, r.getDeployedCondition(ctx, object.GetGeneration())); err != nil {
		return err
	}
	if err := r.reconcileCondition(ctx, object, r.getAvailableCondition(ctx, object.GetGeneration())); err != nil {
		return err
	}
	if err := r.reconcileCondition(ctx, object, r.getDependenciesFulfilledCondition(ctx, object)); err != nil {
		return err
	}
	if err := r.reconcileCondition(ctx, object, r.getReadyCondition(ctx, object.GetGeneration())); err != nil {
		return err
	}
	return r.reconcileCondition(ctx, object, r.getReconcilingCondition(object.GetGeneration(), false))
}

func (r *StatusReconciler) reconcileCondition(ctx context.Context, object objectWithConditions, condition metav1.Condition) error {
	configToUpdate := object.DeepCopy()
	patch := client.MergeFrom(configToUpdate.DeepCopy().GetInternalObject())
	found := false

	updatedConditions := configToUpdate.DeepCopy().GetConditions()
	for index, existingCondition := range configToUpdate.GetConditions() {
		if existingCondition.Type == condition.Type {
			if existingCondition.ObservedGeneration == condition.ObservedGeneration &&
				existingCondition.Status == condition.Status &&
				existingCondition.Message == condition.Message {
				return nil
			}
			found = true
			updatedConditions[index] = condition
			break
		}
	}

	if !found {
		updatedConditions = append(updatedConditions, condition)
	}

	configToUpdate.SetConditions(updatedConditions)
	object.SetConditions(updatedConditions)

	return r.Status().Patch(ctx, configToUpdate.GetInternalObject(), patch)
}

func (r *StatusReconciler) getDeployedCondition(ctx context.Context, gen int64) metav1.Condition {
	deployed, err := r.deployable.IsDeployed(ctx, r.Client)
	if err != nil {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionTypeDeployed),
			Status:             metav1.ConditionFalse,
			Reason:             string(kaiv1.Deployed),
			Message:            err.Error(),
			ObservedGeneration: gen,
			LastTransitionTime: metav1.Now(),
		}
	}
	if deployed {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionTypeDeployed),
			Status:             metav1.ConditionTrue,
			Reason:             string(kaiv1.Deployed),
			Message:            "Resources deployed",
			ObservedGeneration: gen,
			LastTransitionTime: metav1.Now(),
		}
	}
	return metav1.Condition{
		Type:               string(kaiv1.ConditionTypeDeployed),
		Status:             metav1.ConditionFalse,
		Reason:             string(kaiv1.Deployed),
		Message:            "Resources not deployed yet",
		ObservedGeneration: gen,
		LastTransitionTime: metav1.Now(),
	}
}

func (r *StatusReconciler) getReconcilingCondition(gen int64, reconciling bool) metav1.Condition {
	if reconciling {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionTypeReconciling),
			Status:             metav1.ConditionTrue,
			Reason:             string(kaiv1.Reconciling),
			Message:            "Reconciliation in progress",
			ObservedGeneration: gen,
			LastTransitionTime: metav1.Now(),
		}
	}
	return metav1.Condition{
		Type:               string(kaiv1.ConditionTypeReconciling),
		Status:             metav1.ConditionFalse,
		Reason:             string(kaiv1.Reconciled),
		Message:            "Reconciliation completed successfully",
		ObservedGeneration: gen,
		LastTransitionTime: metav1.Now(),
	}
}

func (r *StatusReconciler) getAvailableCondition(ctx context.Context, gen int64) metav1.Condition {
	available, err := r.deployable.IsAvailable(ctx, r.Client)
	if err != nil {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionTypeAvailable),
			Status:             metav1.ConditionFalse,
			Reason:             string(kaiv1.Available),
			Message:            err.Error(),
			ObservedGeneration: gen,
			LastTransitionTime: metav1.Now(),
		}
	}
	if available {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionTypeAvailable),
			Status:             metav1.ConditionTrue,
			Reason:             string(kaiv1.Available),
			Message:            "System available",
			ObservedGeneration: gen,
			LastTransitionTime: metav1.Now(),
		}
	}
	return metav1.Condition{
		Type:               string(kaiv1.ConditionTypeAvailable),
		Status:             metav1.ConditionFalse,
		Reason:             string(kaiv1.Available),
		Message:            "System not available",
		ObservedGeneration: gen,
		LastTransitionTime: metav1.Now(),
	}
}

func (r *StatusReconciler) getDependenciesFulfilledCondition(ctx context.Context, object objectWithConditions) metav1.Condition {
	missingDependencies, err := r.deployable.HasMissingDependencies(ctx, r.Client, object.GetInternalObject())
	if err != nil {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionDependenciesFulfilled),
			Status:             metav1.ConditionFalse,
			Reason:             string(kaiv1.DependenciesMissing),
			Message:            err.Error(),
			ObservedGeneration: object.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		}
	}

	if len(missingDependencies) > 0 {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionDependenciesFulfilled),
			Status:             metav1.ConditionFalse,
			Reason:             string(kaiv1.DependenciesMissing),
			Message:            missingDependencies,
			ObservedGeneration: object.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		}
	}

	return metav1.Condition{
		Type:               string(kaiv1.ConditionDependenciesFulfilled),
		Status:             metav1.ConditionTrue,
		Reason:             string(kaiv1.DependenciesFulfilled),
		Message:            "Dependencies are fulfilled",
		ObservedGeneration: object.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}
}

func (r *StatusReconciler) getReadyCondition(ctx context.Context, gen int64) metav1.Condition {
	condition := r.getAvailableCondition(ctx, gen)
	condition.Type = string(kaiv1.ConditionTypeReady)
	if condition.Status == metav1.ConditionTrue {
		condition.Reason = string(kaiv1.Ready)
		condition.Message = "System is ready"
	} else {
		condition.Reason = string(kaiv1.NotReady)
		condition.Message = "System not ready"
	}
	return condition
}
