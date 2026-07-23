// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
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

	oldPodGroup := &schedulingv2alpha2.PodGroup{}
	key := types.NamespacedName{
		Namespace: pgMetadata.Namespace,
		Name:      pgMetadata.Name,
	}
	err := h.client.Get(ctx, key, oldPodGroup)
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
	// to avoid overriding the fields that external services are responsible for
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

	if newPodGroupCopy.Spec.TopologyConstraint.Topology == "" {
		newPodGroupCopy.Spec.TopologyConstraint = oldPodGroup.Spec.TopologyConstraint
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
			Queue:                podGroupMetadata.Queue,
			PriorityClassName:    podGroupMetadata.PriorityClassName,
			SubGroups:            []schedulingv2alpha2.SubGroup{},
			Preemptibility:       podGroupMetadata.Preemptibility,
			PreemptionDelay:      podGroupMetadata.PreemptionDelay,
			StalenessGracePeriod: podGroupMetadata.StalenessGracePeriod,
		},
	}
	if podGroupMetadata.MinSubGroup != nil {
		pg.Spec.MinSubGroup = podGroupMetadata.MinSubGroup
	} else {
		pg.Spec.MinMember = ptr.To(podGroupMetadata.MinAvailable)
	}

	for _, subGroup := range podGroupMetadata.SubGroups {
		var topologyConstraint *schedulingv2alpha2.TopologyConstraint
		if subGroup.TopologyConstraints != nil {
			topologyConstraint = &schedulingv2alpha2.TopologyConstraint{
				PreferredTopologyLevel: subGroup.TopologyConstraints.PreferredTopologyLevel,
				RequiredTopologyLevel:  subGroup.TopologyConstraints.RequiredTopologyLevel,
				Topology:               subGroup.TopologyConstraints.Topology,
			}
		}
		newSubGroup := schedulingv2alpha2.SubGroup{
			Name:               subGroup.Name,
			Parent:             subGroup.Parent,
			TopologyConstraint: topologyConstraint,
		}
		if subGroup.MinSubGroup != nil {
			newSubGroup.MinSubGroup = subGroup.MinSubGroup
		} else {
			newSubGroup.MinMember = ptr.To(subGroup.MinAvailable)
		}
		pg.Spec.SubGroups = append(pg.Spec.SubGroups, newSubGroup)
	}

	pg.Spec.TopologyConstraint = schedulingv2alpha2.TopologyConstraint{
		PreferredTopologyLevel: podGroupMetadata.PreferredTopologyLevel,
		RequiredTopologyLevel:  podGroupMetadata.RequiredTopologyLevel,
		Topology:               podGroupMetadata.Topology,
	}

	return pg
}
