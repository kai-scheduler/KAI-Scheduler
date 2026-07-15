// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_relations

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

const (
	podGroupAnnotationForPodName = "pod-group-name"
	PodGroupToPodsIndexer        = "PodGroupToPodsIndexer"
)

func PodGroupNameIndexerFunc(rawObj client.Object) []string {
	podGroupName, err := GetPodGroupName(rawObj)
	if err != nil {
		return nil
	}
	return []string{podGroupName}
}

func GetPodGroupName(rawObj client.Object) (string, error) {
	pod, conversionSuccess := rawObj.(*v1.Pod)
	if !conversionSuccess {
		return "", fmt.Errorf("failed to convert the object %v to pod", rawObj)
	}

	podGroupName, found := pod.Annotations[podGroupAnnotationForPodName]
	if !found {
		return "", fmt.Errorf("no podgroup annotation found for pod %s/%s", pod.Namespace, pod.Name)
	}
	return podGroupName, nil
}

func GetAllPodsOfPodGroup(ctx context.Context, podGroup *v2alpha2.PodGroup, kubeClient client.Client) (
	v1.PodList, error) {
	podGroupPods := v1.PodList{}
	err := kubeClient.List(ctx, &podGroupPods,
		client.MatchingFields{PodGroupToPodsIndexer: podGroup.Name},
		client.InNamespace(podGroup.Namespace),
	)
	if err != nil {
		return v1.PodList{}, err
	}
	return podGroupPods, nil
}
