// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgroupcontroller/controllers/cluster_relations"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgroupcontroller/controllers/metadata"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgroupcontroller/controllers/patcher"
	utilities "github.com/kai-scheduler/kai-scheduler/pkg/podgroupcontroller/utilities/pod-group"
)

func (r *PodGroupReconciler) handlePodGroupStatus(ctx context.Context, podGroup *v2alpha2.PodGroup) (
	ctrl.Result, error) {

	logger := log.FromContext(ctx)
	relatedPods, err := cluster_relations.GetAllPodsOfPodGroup(ctx, podGroup, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get pods from podGroup <%s/%s>. Error: %w",
			podGroup.Namespace, podGroup.Name, err)
	}

	podGroupMetadata, err := r.calculatePodGroupMetadata(ctx, podGroup, relatedPods)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.updateStatusIfNecessary(ctx, podGroup, podGroupMetadata)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to update podgroup %s/%s with metadata",
			podGroup.Namespace, podGroup.Name))
	}
	return ctrl.Result{}, err
}

func (r *PodGroupReconciler) updateStatusIfNecessary(
	ctx context.Context, podGroup *v2alpha2.PodGroup, podGroupMetadata *metadata.PodGroupMetadata,
) error {
	logger := log.FromContext(ctx)

	if !patcher.ShouldUpdatePodGroupStatus(podGroup, podGroupMetadata) {
		return nil
	}

	logger.Info(fmt.Sprintf("Updated status for podgroup %s/%s with metadata %v",
		podGroup.Namespace, podGroup.Name, podGroupMetadata))
	err := patcher.UpdatePodGroupStatus(ctx, podGroup, podGroupMetadata, r.Client)
	return client.IgnoreNotFound(err)
}

func (r *PodGroupReconciler) calculatePodGroupMetadata(
	ctx context.Context, podGroup *v2alpha2.PodGroup, relatedPods v1.PodList,
) (*metadata.PodGroupMetadata, error) {
	var err error
	logger := log.FromContext(ctx)
	podGroupMetadata := metadata.NewPodGroupMetadata()

	podGroupMetadata.Preemptible, err = utilities.IsPreemptible(ctx, podGroup, r.Client)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to calculate preemtability for pod-group %s/%s",
			podGroup.Namespace, podGroup.Name))
		return nil, err
	}

	for _, relatedPod := range relatedPods.Items {
		if err := addPodMetadata(ctx, podGroupMetadata, relatedPod, r.Client); err != nil {
			return nil, err
		}
	}
	logger.V(3).Info(fmt.Sprintf("Pod-group calculated metadata %v", podGroupMetadata))
	return podGroupMetadata, nil
}

func addPodMetadata(
	ctx context.Context, podGroupMetadata *metadata.PodGroupMetadata, pod v1.Pod, kubeclient client.Client,
) error {
	logger := log.FromContext(ctx)

	podMetadata, err := metadata.GetPodMetadata(ctx, &pod, kubeclient)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to calculate metadata for pod %s/%s", pod.Namespace, pod.Name))
		return err
	}
	logger.V(3).Info(fmt.Sprintf("For pod %s/%s calculated metadata %v",
		pod.Namespace, pod.Name, podMetadata))

	podGroupMetadata.AddPodMetadata(podMetadata)
	return nil
}
