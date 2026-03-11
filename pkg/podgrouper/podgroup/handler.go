// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedulingv2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
)

type Handler struct {
	client        client.Client
	nodePoolKey   string
	queueLabelKey string
}

func NewHandler(client client.Client, nodePoolKey string, queueLabelKey string) *Handler {
	return &Handler{
		client:        client,
		nodePoolKey:   nodePoolKey,
		queueLabelKey: queueLabelKey,
	}
}

func (h *Handler) ApplyToCluster(ctx context.Context, pgMetadata Metadata) error {
	newPodGroup := h.createPodGroupForMetadata(pgMetadata)

	err := h.assignPodsToSubGroup(ctx, pgMetadata)
	if err != nil {
		return fmt.Errorf("error assigning pods to subgroup: %v", err)
	}

	oldPodGroup := &schedulingv2alpha2.PodGroup{}
	key := types.NamespacedName{
		Namespace: pgMetadata.Namespace,
		Name:      pgMetadata.Name,
	}
	err = h.client.Get(ctx, key, oldPodGroup)
	if err != nil {
		if errors.IsNotFound(err) {
			err = h.client.Create(ctx, newPodGroup)
			return err
		}
		return err
	}

	newPodGroup = h.ignoreFields(oldPodGroup, newPodGroup)

	// If we got here then oldPodGroup exists - update if necessary
	if podGroupsEqual(oldPodGroup, newPodGroup) {
		// The objects are equal - no need to update.
		return nil
	}

	updatePodGroup(oldPodGroup, newPodGroup)

	err = h.client.Update(ctx, oldPodGroup)
	return err
}

func (h *Handler) ignoreFields(oldPodGroup, newPodGroup *schedulingv2alpha2.PodGroup) *schedulingv2alpha2.PodGroup {
	// to avoid overriding the fields that the pod-group-assigner is responsible for
	newPodGroupCopy := newPodGroup.DeepCopy()

	newPodGroupCopy.Spec.MarkUnschedulable = oldPodGroup.Spec.MarkUnschedulable
	newPodGroupCopy.Spec.SchedulingBackoff = oldPodGroup.Spec.SchedulingBackoff
	newPodGroupCopy.Spec.Queue = oldPodGroup.Spec.Queue

	if newPodGroupCopy.Labels == nil {
		newPodGroupCopy.Labels = map[string]string{}
	}
	nodePoolName, found := oldPodGroup.Labels[h.nodePoolKey]
	if found {
		newPodGroupCopy.Labels[h.nodePoolKey] = nodePoolName
	} else {
		delete(newPodGroupCopy.Labels, h.nodePoolKey)
	}

	queueName, found := oldPodGroup.Labels[h.queueLabelKey]
	if found {
		newPodGroupCopy.Labels[h.queueLabelKey] = queueName
	}

	return newPodGroupCopy
}

func (h *Handler) createPodGroupForMetadata(podGroupMetadata Metadata) *schedulingv2alpha2.PodGroup {
	pg := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podGroupMetadata.Name,
			Namespace:   podGroupMetadata.Namespace,
			Labels:      podGroupMetadata.Labels,
			Annotations: podGroupMetadata.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				podGroupMetadata.Owner,
			},
		},
		Spec: schedulingv2alpha2.PodGroupSpec{
			MinMember:         podGroupMetadata.MinAvailable,
			Queue:             podGroupMetadata.Queue,
			PriorityClassName: podGroupMetadata.PriorityClassName,
			SubGroups:         []schedulingv2alpha2.SubGroup{},
			Preemptibility:    podGroupMetadata.Preemptibility,
		},
	}

	for _, subGroup := range podGroupMetadata.SubGroups {
		pg.Spec.SubGroups = append(pg.Spec.SubGroups,
			schedulingv2alpha2.SubGroup{
				Name:      subGroup.Name,
				MinMember: subGroup.MinAvailable,
				Parent:    subGroup.Parent,
			})
	}

	pg.Spec.TopologyConstraint = schedulingv2alpha2.TopologyConstraint{
		PreferredTopologyLevel: podGroupMetadata.PreferredTopologyLevel,
		RequiredTopologyLevel:  podGroupMetadata.RequiredTopologyLevel,
		Topology:               podGroupMetadata.Topology,
	}

	return pg
}

func (h *Handler) assignPodsToSubGroup(ctx context.Context, podGroupMetadata Metadata) error {
	for _, subGroup := range podGroupMetadata.SubGroups {
		for _, podName := range subGroup.PodsReferences {
			pod := &v1.Pod{}
			err := h.client.Get(ctx, types.NamespacedName{Namespace: podGroupMetadata.Namespace, Name: podName}, pod)
			if err != nil {
				return err
			}

			labeledPod := pod.DeepCopy()
			labeledPod.Labels[commonconstants.SubGroupLabelKey] = subGroup.Name

			err = h.client.Patch(ctx, labeledPod, client.MergeFrom(pod))
			if err != nil {
				return err
			}
		}
	}
	return nil
}
