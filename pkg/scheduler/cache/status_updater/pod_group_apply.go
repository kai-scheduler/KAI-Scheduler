// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/utils"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

const schedulingStatusManagerPrefix = "kai-scheduler-status/"

var errSchedulingInputsChanged = errors.New("scheduling inputs changed")

func (su *defaultStatusUpdater) applyPodGroupStatus(ctx context.Context, desired *enginev2alpha2.PodGroup) (*enginev2alpha2.PodGroup, error) {
	desired = desired.DeepCopy()
	normalizeSchedulingConditions(desired)
	var updated *enginev2alpha2.PodGroup
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		updated, err = su.applyPodGroupStatusOnce(ctx, desired)
		return err
	})
	if errors.Is(err, errSchedulingInputsChanged) {
		err = apierrors.NewConflict(schema.GroupResource{Group: enginev2alpha2.SchemeGroupVersion.Group, Resource: "podgroups"}, desired.Name, err)
	}
	return updated, err
}

func (su *defaultStatusUpdater) applyPodGroupStatusOnce(ctx context.Context, desired *enginev2alpha2.PodGroup) (*enginev2alpha2.PodGroup, error) {
	client := su.kaiClient.SchedulingV2alpha2().PodGroups(desired.Namespace)
	live, err := client.Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	nodePool := utils.GetNodePoolNameFromLabels(desired.Labels, su.nodePoolLabelKey)
	if !schedulingInputsEqual(live, desired) {
		return nil, errSchedulingInputsChanged
	}
	if normalizeSchedulingConditions(live) {
		conditions := make([]map[string]any, 0, len(live.Status.SchedulingConditions))
		for i := range live.Status.SchedulingConditions {
			intent, err := schedulingConditionIntent(&live.Status.SchedulingConditions[i])
			if err != nil {
				return nil, err
			}
			conditions = append(conditions, intent)
		}
		data, err := json.Marshal(map[string]any{
			"metadata": map[string]any{"uid": live.UID, "resourceVersion": live.ResourceVersion},
			"status":   map[string]any{"schedulingConditions": conditions},
		})
		if err != nil {
			return nil, err
		}
		live, err = client.Patch(ctx, live.Name, types.MergePatchType, data, metav1.PatchOptions{FieldManager: "kai-scheduler-status-migration"}, "status")
		if err != nil {
			return nil, err
		}
	}
	if err := su.verifySchedulingStatusSchema(ctx, live, nodePool); err != nil {
		return nil, err
	}
	manager := schedulingStatusManagerPrefix + nodePool
	migrated, err := migrateSchedulingStatusFields(live, manager, nodePool, desired.Status.SchedulingState != nil)
	if err != nil {
		return nil, err
	}
	if migrated {
		data, err := json.Marshal(map[string]any{"metadata": map[string]any{
			"uid": live.UID, "resourceVersion": live.ResourceVersion, "managedFields": live.ManagedFields,
		}})
		if err != nil {
			return nil, err
		}
		live, err = client.Patch(ctx, live.Name, types.MergePatchType, data, metav1.PatchOptions{})
		if err != nil {
			return nil, err
		}
	}

	// Adopt historical entries before omitting them: omission only deletes fields previously applied by this manager.
	if conditionForNodePool(desired, nodePool) == nil && conditionForNodePool(live, nodePool) != nil && !ownsSchedulingCondition(live, manager, nodePool) {
		seed := live.DeepCopy()
		seed.Status.SchedulingState = desired.Status.SchedulingState
		live, err = su.sendSchedulingStatus(ctx, seed, nodePool, manager, live.ResourceVersion)
		if err != nil {
			return nil, err
		}
	}
	return su.sendSchedulingStatus(ctx, desired, nodePool, manager, live.ResourceVersion)
}

func (su *defaultStatusUpdater) sendSchedulingStatus(ctx context.Context, podGroup *enginev2alpha2.PodGroup, nodePool, manager, resourceVersion string) (*enginev2alpha2.PodGroup, error) {
	return su.patchSchedulingStatus(ctx, podGroup, nodePool, manager, resourceVersion, nil)
}

func (su *defaultStatusUpdater) patchSchedulingStatus(ctx context.Context, podGroup *enginev2alpha2.PodGroup, nodePool, manager, resourceVersion string, dryRun []string) (*enginev2alpha2.PodGroup, error) {
	status := map[string]any{}
	if condition := conditionForNodePool(podGroup, nodePool); condition != nil {
		intent, err := schedulingConditionIntent(condition)
		if err != nil {
			return nil, err
		}
		status["schedulingConditions"] = []any{intent}
	}
	if podGroup.Status.SchedulingState != nil {
		status["schedulingState"] = podGroup.Status.SchedulingState
	}
	metadata := map[string]any{"name": podGroup.Name, "namespace": podGroup.Namespace, "uid": podGroup.UID}
	if resourceVersion != "" {
		metadata["resourceVersion"] = resourceVersion
	}
	data, err := json.Marshal(map[string]any{
		"apiVersion": enginev2alpha2.SchemeGroupVersion.String(), "kind": "PodGroup", "metadata": metadata, "status": status,
	})
	if err != nil {
		return nil, err
	}
	force := true
	return su.kaiClient.SchedulingV2alpha2().PodGroups(podGroup.Namespace).Patch(ctx, podGroup.Name, types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: manager, Force: &force, DryRun: dryRun}, "status")
}

func conditionForNodePool(podGroup *enginev2alpha2.PodGroup, nodePool string) *enginev2alpha2.SchedulingCondition {
	index := utils.GetSchedulingConditionIndex(podGroup, nodePool)
	if index < 0 {
		return nil
	}
	return &podGroup.Status.SchedulingConditions[index]
}

func schedulingConditionIntent(condition *enginev2alpha2.SchedulingCondition) (map[string]any, error) {
	intent, err := runtime.DefaultUnstructuredConverter.ToUnstructured(condition)
	if err != nil {
		return nil, err
	}
	intent["nodePool"] = condition.NodePool
	if condition.LastTransitionTime.IsZero() {
		delete(intent, "lastTransitionTime")
	}
	return intent, nil
}

func (su *defaultStatusUpdater) verifySchedulingStatusSchema(ctx context.Context, live *enginev2alpha2.PodGroup, nodePool string) error {
	if su.statusSchemaVerified.Load() {
		return nil
	}
	probe := live.DeepCopy()
	probe.Status = enginev2alpha2.PodGroupStatus{SchedulingConditions: []enginev2alpha2.SchedulingCondition{{NodePool: nodePool, Type: enginev2alpha2.UnschedulableOnNodePool}}}
	const manager = "kai-scheduler-status-schema-check"
	applied, err := su.patchSchedulingStatus(ctx, probe, nodePool, manager, live.ResourceVersion, []string{metav1.DryRunAll})
	if err != nil {
		return err
	}
	if !ownsSchedulingCondition(applied, manager, nodePool) {
		return apierrors.NewBadRequest("PodGroup schedulingConditions requires a map list keyed by nodePool; upgrade the PodGroup CRD before enabling scheduler status apply")
	}
	su.statusSchemaVerified.Store(true)
	return nil
}

func schedulingInputsEqual(a, b *enginev2alpha2.PodGroup) bool {
	return a.UID == b.UID && reflect.DeepEqual(a.Spec, b.Spec) && reflect.DeepEqual(a.Labels, b.Labels)
}
