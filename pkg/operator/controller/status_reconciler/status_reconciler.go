// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_reconciler

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/deployable"
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
	isAvailable, availableErr := r.deployable.IsAvailable(ctx, r.Client)
	if err := r.reconcileCondition(ctx, object, r.getAvailableCondition(object.GetGeneration(), isAvailable, availableErr)); err != nil {
		return err
	}
	if err := r.reconcileCondition(ctx, object, r.getReadyCondition(object.GetGeneration(), isAvailable)); err != nil {
		return err
	}
	if err := r.reconcileCondition(ctx, object, r.getDependenciesFulfilledCondition(object.GetGeneration())); err != nil {
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

func (r *StatusReconciler) getReconcilingCondition(gen int64, isReconciling bool) metav1.Condition {
	status := metav1.ConditionFalse
	message := "Reconciliation completed"
	if isReconciling {
		status = metav1.ConditionTrue
		message = "Reconciliation in progress"
	}

	return metav1.Condition{
		Type:               string(kaiv1.ConditionTypeReconciling),
		Status:             status,
		Reason:             string(kaiv1.Reconciled),
		Message:            message,
		ObservedGeneration: gen,
		LastTransitionTime: metav1.Now(),
	}
}

func (r *StatusReconciler) getAvailableCondition(gen int64, isAvailable bool, availableErr error) metav1.Condition {
	if availableErr != nil {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionTypeAvailable),
			Status:             metav1.ConditionFalse,
			Reason:             string(kaiv1.Available),
			Message:            availableErr.Error(),
			ObservedGeneration: gen,
			LastTransitionTime: metav1.Now(),
		}
	}
	if isAvailable {
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

func (r *StatusReconciler) getDependenciesFulfilledCondition(gen int64) metav1.Condition {
	return metav1.Condition{
		Type:               string(kaiv1.ConditionDependenciesFulfilled),
		Status:             metav1.ConditionTrue,
		Reason:             string(kaiv1.DependenciesFulfilled),
		Message:            "Dependencies are fulfilled",
		ObservedGeneration: gen,
		LastTransitionTime: metav1.Now(),
	}
}

func (r *StatusReconciler) getReadyCondition(gen int64, isReady bool) metav1.Condition {
	if isReady {
		return metav1.Condition{
			Type:               string(kaiv1.ConditionTypeReady),
			Status:             metav1.ConditionTrue,
			Reason:             string(kaiv1.Ready),
			Message:            "System is ready",
			ObservedGeneration: gen,
			LastTransitionTime: metav1.Now(),
		}
	}
	return metav1.Condition{
		Type:               string(kaiv1.ConditionTypeReady),
		Status:             metav1.ConditionFalse,
		Reason:             string(kaiv1.Ready),
		Message:            "System is not ready",
		ObservedGeneration: gen,
		LastTransitionTime: metav1.Now(),
	}
}
