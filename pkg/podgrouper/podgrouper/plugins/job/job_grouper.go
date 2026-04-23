// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package job

import (
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

type K8sJobGrouper struct {
	client                   client.Client
	searchForLegacyPodGroups bool
	*defaultgrouper.DefaultGrouper
}

var logger = log.FromContext(context.Background())

func NewK8sJobGrouper(
	client client.Client, defaultGrouper *defaultgrouper.DefaultGrouper, searchForLegacyPodGroups bool,
) *K8sJobGrouper {
	return &K8sJobGrouper{
		client:                   client,
		searchForLegacyPodGroups: searchForLegacyPodGroups,
		DefaultGrouper:           defaultGrouper,
	}
}

func (g *K8sJobGrouper) Name() string {
	return "BatchJob Grouper"
}

// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs/finalizers,verbs=patch;update;create

func (g *K8sJobGrouper) GetPodGroupMetadata(topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	podGroupMetadata, err := g.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}

	var legacy bool
	podGroupMetadata.Name, legacy, err = g.calcPodGroupName(topOwner, pod)
	if err != nil {
		return nil, err
	}

	minMember, err := calcMinMember(topOwner, pod, legacy)
	if err != nil {
		return nil, err
	}
	podGroupMetadata.MinAvailable = minMember

	return podGroupMetadata, nil
}

func (g *K8sJobGrouper) calcPodGroupName(topOwner *unstructured.Unstructured, pod *v1.Pod) (string, bool, error) {
	newName := fmt.Sprintf("%s-%s-%s", constants.PodGroupNamePrefix, topOwner.GetName(), topOwner.GetUID())

	// Prior versions named the podgroup after the pod (pg-<pod>-<uid>) so that
	// each pod of a multi-pod Job had its own podgroup. Keep using that name if
	// such a podgroup already exists, so running pods aren't re-parented during
	// an upgrade. During the upgrade window new pods of the same Job may join
	// the new unified podgroup while older pods remain on their legacy ones;
	// with the default MinAvailable=1 this is harmless.
	if !g.searchForLegacyPodGroups {
		return newName, false, nil
	}

	legacyName := fmt.Sprintf("%s-%s-%s", constants.PodGroupNamePrefix, pod.Name, topOwner.GetUID())
	if legacyName == newName {
		return newName, false, nil
	}

	legacyPodGroupObj := &v2alpha2.PodGroup{}
	err := g.client.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: legacyName},
		legacyPodGroupObj)
	if err == nil {
		logger.V(1).Info("Using legacy pod-group %s/%s", pod.Namespace, legacyName)
		return legacyName, true, nil
	}
	if !errors.IsNotFound(err) {
		logger.V(1).Error(err,
			"While searching for legacy pod group for pod %s/%s, an error has occurred.",
			pod.Namespace, legacyName)
		return "", false, err
	}
	return newName, false, nil
}

func calcMinMember(topOwner *unstructured.Unstructured, pod *v1.Pod, legacy bool) (int32, error) {
	if legacy {
		return 1, nil
	}

	override, found := topOwner.GetAnnotations()[constants.MinMemberOverrideKey]
	if !found {
		return 1, nil
	}

	minMember, err := strconv.ParseInt(override, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid min-member annotation value: %w", err)
	}

	return int32(minMember), nil
}
