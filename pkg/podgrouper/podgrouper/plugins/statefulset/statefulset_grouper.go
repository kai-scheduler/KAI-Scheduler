// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package statefulset

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/minmember"
)

// minMemberAnnotationKey is the annotation a StatefulSet owner can set to declare its minimum gang size.
// It is only honoured when podManagementPolicy: Parallel is configured; OrderedReady creates pods
// sequentially, which would deadlock gang scheduling.
const minMemberAnnotationKey = "kai.scheduler/min-member"

// StatefulSetGrouper handles apps/v1 StatefulSet workloads.
// It delegates common metadata construction to DefaultGrouper and adds
// StatefulSet-specific min-member support via the kai.scheduler/min-member annotation.
type StatefulSetGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewStatefulSetGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *StatefulSetGrouper {
	return &StatefulSetGrouper{DefaultGrouper: defaultGrouper}
}

func (g *StatefulSetGrouper) Name() string {
	return "StatefulSet Grouper"
}

func (g *StatefulSetGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, allOwners ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	metadata, err := g.DefaultGrouper.GetPodGroupMetadata(topOwner, pod, allOwners...)
	if err != nil {
		return nil, err
	}

	_, hasAnnotation := topOwner.GetAnnotations()[minMemberAnnotationKey]
	if !hasAnnotation {
		return metadata, nil
	}

	if err := requireParallelPolicy(topOwner); err != nil {
		return nil, err
	}

	minAvailable, err := minmember.FromAnnotationsWithKey(topOwner, "StatefulSet", minMemberAnnotationKey, 1)
	if err != nil {
		return nil, err
	}
	metadata.MinAvailable = minAvailable

	return metadata, nil
}

// requireParallelPolicy returns an error when the StatefulSet's podManagementPolicy is not Parallel.
// An absent field is treated as the Kubernetes default (OrderedReady).
func requireParallelPolicy(topOwner *unstructured.Unstructured) error {
	policy, _, _ := unstructured.NestedString(topOwner.Object, "spec", "podManagementPolicy")
	if policy == "Parallel" {
		return nil
	}
	displayPolicy := policy
	if displayPolicy == "" {
		displayPolicy = "OrderedReady (default)"
	}
	return fmt.Errorf(
		"%s annotation requires podManagementPolicy: Parallel on StatefulSet %s/%s "+
			"(current policy %q creates pods sequentially, which deadlocks gang scheduling)",
		minMemberAnnotationKey,
		topOwner.GetNamespace(), topOwner.GetName(),
		displayPolicy,
	)
}
