// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group

import (
	"context"

	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

const (
	PriorityBuildNumber = 100
)

func IsPreemptible(ctx context.Context, podGroup *v2alpha2.PodGroup, kubeClient client.Client) (bool, error) {
	priority, err := getPodGroupPriority(ctx, podGroup, kubeClient)
	if errors.IsNotFound(err) {
		logger := log.FromContext(ctx)
		logger.Info(
			"Priority Class not found",
			"podGroup", podGroup.Name,
			"namespace", podGroup.Namespace,
			"priorityClass", podGroup.Spec.PriorityClassName,
		)
		return true, nil
	}
	if err != nil {
		return false, err
	}

	return priority < PriorityBuildNumber, nil
}

func getPodGroupPriority(ctx context.Context, podGroup *v2alpha2.PodGroup, kubeClient client.Client) (int32, error) {
	priorityClass := schedulingv1.PriorityClass{}
	err := kubeClient.Get(
		ctx,
		types.NamespacedName{Name: podGroup.Spec.PriorityClassName},
		&priorityClass,
	)
	if err != nil {
		return -1, err
	}
	return priorityClass.Value, nil
}
