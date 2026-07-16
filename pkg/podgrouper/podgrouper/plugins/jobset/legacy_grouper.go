// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// This file contains the legacy JobSet podgrouper code path: detection of
// pre-existing flat PodGroups (no SubGroups) and the previous handler that
// emits the old shape (one PodGroup per replicatedJob in InOrder mode, a
// single flat PodGroup otherwise). The legacy path keeps in-flight workloads
// scheduling correctly across the upgrade to the hierarchical-SubGroup shape.
//
// Deprecated: remove this entire file in v0.17.

package jobset

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	schedulingv2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

// detectLegacyPodGroup returns true if any PodGroup in the JobSet's namespace
// belongs to this JobSet (by name prefix) and has no SubGroups — the shape
// produced by the previous handler. Lists once and filters client-side.
// Deprecated: remove in v0.17.
func (g *JobSetGrouper) detectLegacyPodGroup(jobsetObj *unstructured.Unstructured) (bool, error) {
	ns := jobsetObj.GetNamespace()
	prefix := fmt.Sprintf(jobSetPodGroupNameFormat, jobSetPodGroupNamePrefix, jobsetObj.GetName(), string(jobsetObj.GetUID()))

	pgList := &schedulingv2alpha2.PodGroupList{}
	if err := g.client.List(context.Background(), pgList, client.InNamespace(ns)); err != nil {
		return false, fmt.Errorf("failed to list PodGroups in namespace %s: %w", ns, err)
	}

	for _, pg := range pgList.Items {
		// Match the exact AnyOrder name or any per-replicatedJob InOrder name.
		if pg.Name != prefix && !strings.HasPrefix(pg.Name, prefix+"-") {
			continue
		}
		if len(pg.Spec.SubGroups) == 0 {
			return true, nil
		}
	}
	return false, nil
}

// legacyGetPodGroupMetadata reproduces the previous JobSet handler: one PodGroup
// per replicatedJob in InOrder mode, a single flat PodGroup otherwise.
// Deprecated: remove in v0.17.
func (g *JobSetGrouper) legacyGetPodGroupMetadata(jobsetObj *unstructured.Unstructured, pod *v1.Pod) (*podgroup.Metadata, error) {
	pgMeta, err := g.DefaultGrouper.GetPodGroupMetadata(jobsetObj, pod)
	if err != nil {
		return nil, err
	}

	jobSetName := jobsetObj.GetName()
	jobSetUID := jobsetObj.GetUID()

	replicatedJobName, ok := pod.Labels[jobSetLabelReplicatedJobName]
	if !ok || replicatedJobName == "" {
		return nil, fmt.Errorf("pod %s/%s missing required label %q", pod.Namespace, pod.Name, jobSetLabelReplicatedJobName)
	}

	startupPolicyOrder, err := getStartupPolicyOrder(jobsetObj)
	if err != nil {
		return nil, err
	}

	if startupPolicyOrder == startupPolicyOrderInOrder {
		pgMeta.Name = fmt.Sprintf("%s-%s-%s-%s", jobSetPodGroupNamePrefix, jobSetName, string(jobSetUID), replicatedJobName)
	} else {
		pgMeta.Name = fmt.Sprintf(jobSetPodGroupNameFormat, jobSetPodGroupNamePrefix, jobSetName, string(jobSetUID))
	}

	pgMeta.MinAvailable, err = legacyMinAvailable(jobsetObj)
	if err != nil {
		return nil, err
	}
	pgMeta.MinSubGroup = nil
	pgMeta.SubGroups = nil

	return pgMeta, nil
}

// legacyMinAvailable mirrors the previous handler's batch-min-member parsing.
// Deprecated: remove in v0.17.
func legacyMinAvailable(jobsetObj *unstructured.Unstructured) (int32, error) {
	override, found := jobsetObj.GetAnnotations()[constants.MinMemberOverrideKey]
	if !found {
		return 1, nil
	}
	minMember, err := strconv.ParseInt(override, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s annotation value: %w", constants.MinMemberOverrideKey, err)
	}
	return int32(minMember), nil
}
