// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group

import (
	"context"
	"fmt"

	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	pg "github.com/kai-scheduler/kai-scheduler/pkg/common/podgroup"
)

func IsPreemptible(ctx context.Context, podGroup *v2alpha2.PodGroup, kubeClient client.Client) (bool, error) {
	priority, err := getPodGroupPriority(ctx, podGroup, kubeClient)
	if err != nil {
		return false, fmt.Errorf("failed to determine podgroup's priority: %w", err)
	}
	preemptability := pg.CalculatePreemptibility(podGroup.Spec.Preemptibility, priority)

	return preemptability == v2alpha2.Preemptible, nil
}

func getPodGroupPriority(ctx context.Context, podGroup *v2alpha2.PodGroup, kubeClient client.Client) (int32, error) {
	logger := log.FromContext(ctx,
		"podGroup", podGroup.Name,
		"namespace", podGroup.Namespace,
		"priorityClassName", podGroup.Spec.PriorityClassName)

	// Try to get the specific priority class
	priority, err := getSpecificPriorityClass(ctx, podGroup.Spec.PriorityClassName, kubeClient)
	if err == nil {
		return priority, nil
	}
	if !errors.IsNotFound(err) {
		return -1, fmt.Errorf("failed to get priority class %s: %w", podGroup.Spec.PriorityClassName, err)
	}

	// Priority class not found, try to get global default
	logger.Info("Priority Class not found, trying to get global default priority class")
	priority, err = getGlobalDefaultPriorityClass(ctx, kubeClient)
	if err == nil {
		return priority, nil
	}
	if !errors.IsNotFound(err) {
		return -1, fmt.Errorf("failed to get global default priority class: %w", err)
	}

	// No global default found, use system default
	logger.Info("Global default priority class not found, using default priority")
	return constants.DefaultPodGroupPriority, nil
}

func getSpecificPriorityClass(ctx context.Context, priorityClassName string, kubeClient client.Client) (int32, error) {
	priorityClass := schedulingv1.PriorityClass{}
	err := kubeClient.Get(
		ctx,
		types.NamespacedName{Name: priorityClassName},
		&priorityClass,
	)
	if err != nil {
		return -1, err
	}

	return priorityClass.Value, nil
}

func getGlobalDefaultPriorityClass(ctx context.Context, kubeClient client.Client) (int32, error) {
	priorityClasses := schedulingv1.PriorityClassList{}
	err := kubeClient.List(ctx, &priorityClasses)
	if err != nil {
		return -1, fmt.Errorf("failed to list priority classes: %w", err)
	}

	for _, priorityClass := range priorityClasses.Items {
		if priorityClass.GlobalDefault {
			return priorityClass.Value, nil
		}
	}

	// No global default found
	return -1, errors.NewNotFound(schedulingv1.Resource("priorityclass"), "")
}
