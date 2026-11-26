// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package statefulset

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/minmember"
	"github.com/kai-scheduler/api/podgrouper/constants"
)

const (
	defaultMinMember = 1

	specPodManagementPolicy = "podManagementPolicy"
	policyParallel          = string(appsv1.ParallelPodManagement)
)

var logger = log.FromContext(context.Background())

type StatefulSetGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewStatefulSetGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *StatefulSetGrouper {
	return &StatefulSetGrouper{DefaultGrouper: defaultGrouper}
}

func (sg *StatefulSetGrouper) Name() string {
	return "StatefulSet Grouper"
}

func (sg *StatefulSetGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, allOwners ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	metadata, err := sg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod, allOwners...)
	if err != nil {
		return nil, err
	}

	desiredMinMember, err := minmember.FromAnnotations(topOwner, "StatefulSet", defaultMinMember)
	if err != nil {
		return nil, err
	}
	specReplicas, found, err := unstructured.NestedInt64(topOwner.Object, "spec", "replicas")
	if err != nil {
		return nil, err
	}
	if _, annotated := topOwner.GetAnnotations()[constants.MinMemberOverrideKey]; annotated && found &&
		int64(desiredMinMember) > specReplicas {
		logger.Info("Warning: min-member annotation is higher than the StatefulSet replicas, pods will not be scheduled until it is scaled up",
			"statefulset", fmt.Sprintf("%s/%s", topOwner.GetNamespace(), topOwner.GetName()),
			"annotation", constants.MinMemberOverrideKey, "minMember", desiredMinMember, "replicas", specReplicas)
	}
	metadata.MinAvailable = desiredMinMember

	// OrderedReady creates a pod only after the previous one is ready, so the gang is bounded by the pods the
	// controller reports in status.replicas. The default policy is OrderedReady.
	if policy, _, _ := unstructured.NestedString(topOwner.Object, "spec", specPodManagementPolicy); policy != policyParallel {
		statusReplicas, _, err := unstructured.NestedInt64(topOwner.Object, "status", "replicas")
		if err != nil {
			return nil, err
		}
		metadata.MinAvailable = min(desiredMinMember, int32(max(statusReplicas, defaultMinMember)))
	}

	return metadata, nil
}
