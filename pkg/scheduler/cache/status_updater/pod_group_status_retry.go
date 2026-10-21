// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"context"
	"errors"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/utils"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

var errStatusIntentStale = errors.New("pod group changed since the status update was queued")

// podGroupStatusIntent holds the scheduler-owned status fields of a pod group update.
type podGroupStatusIntent struct {
	uid        types.UID
	generation int64
	nodePool   string

	condition       *enginev2alpha2.SchedulingCondition
	schedulingState *enginev2alpha2.PodGroupSchedulingState
}

// retryPodGroupStatusOnConflict applies the intent to the latest pod group instead of resending the
// stale snapshot. Returns the written pod group and the resource versions read and written.
func (su *defaultStatusUpdater) retryPodGroupStatusOnConflict(
	ctx context.Context, key updatePayloadKey, updateData *inflightUpdate,
) (*enginev2alpha2.PodGroup, []string, error) {
	snapshot := updateData.object.(*enginev2alpha2.PodGroup)
	intent := updateData.statusIntent
	var written *enginev2alpha2.PodGroup
	var resourceVersions []string

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if current, found := su.inFlightPodGroups.Load(key); !found || current != updateData {
			return errStatusIntentStale
		}
		latest, err := su.kaiClient.SchedulingV2alpha2().PodGroups(snapshot.Namespace).Get(
			ctx, snapshot.Name, metav1.GetOptions{},
		)
		if err != nil {
			return err
		}
		if reason := intent.staleReason(latest, su.nodePoolLabelKey); reason != "" {
			log.StatusUpdaterLogger.V(5).Infof("Not retrying pod group status update %s/%s: %s",
				snapshot.Namespace, snapshot.Name, reason)
			return errStatusIntentStale
		}

		updated := latest.DeepCopy()
		intent.applyTo(updated)
		written, err = su.kaiClient.SchedulingV2alpha2().PodGroups(snapshot.Namespace).UpdateStatus(
			ctx, updated, metav1.UpdateOptions{},
		)
		if err != nil {
			return err
		}
		resourceVersions = append(resourceVersions, latest.ResourceVersion, written.ResourceVersion)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return written, resourceVersions, nil
}

func (intent *podGroupStatusIntent) staleReason(latest *enginev2alpha2.PodGroup, nodePoolLabelKey string) string {
	switch {
	case latest.UID != intent.uid:
		return "pod group was recreated"
	case latest.DeletionTimestamp != nil:
		return "pod group is being deleted"
	case latest.Generation != intent.generation:
		return "pod group spec changed"
	case utils.GetNodePoolNameFromLabels(latest.Labels, nodePoolLabelKey) != intent.nodePool:
		return "pod group node pool changed"
	case intent.hasNewerCondition(latest):
		return "newer scheduling condition for the node pool"
	}
	return ""
}

// Only this node pool's scheduler writes its condition, so an equal or higher transition ID means a
// later verdict was already written.
func (intent *podGroupStatusIntent) hasNewerCondition(latest *enginev2alpha2.PodGroup) bool {
	if intent.condition == nil {
		return false
	}
	index := utils.GetSchedulingConditionIndex(latest, intent.nodePool)
	if index < 0 {
		return false
	}
	latestID, err := strconv.Atoi(latest.Status.SchedulingConditions[index].TransitionID)
	if err != nil {
		return false
	}
	intentID, err := strconv.Atoi(intent.condition.TransitionID)
	if err != nil {
		return false
	}
	return latestID >= intentID
}

func (intent *podGroupStatusIntent) applyTo(podGroup *enginev2alpha2.PodGroup) {
	if intent.schedulingState != nil {
		podGroup.Status.SchedulingState = intent.schedulingState.DeepCopy()
	}
	if intent.condition != nil {
		condition := intent.condition.DeepCopy()
		setTransitionID(podGroup, condition, utils.GetLastSchedulingCondition(podGroup))
		squashAndAppendConditionsForNodepool(podGroup, condition)
	}
}
