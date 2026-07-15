// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"strconv"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/utils"
)

type podGroupStatusSyncResult string

const (
	snapshotStatusIsOlder podGroupStatusSyncResult = "snapshotStatusIsOlder"
	equalStatuses         podGroupStatusSyncResult = "equalStatuses"
	updateRequestIsOlder  podGroupStatusSyncResult = "updateRequestIsOlder"
)

func (su *defaultStatusUpdater) SyncPodGroupsWithPendingUpdates(podGroups []*enginev2alpha2.PodGroup) {
	usedKeys := make(map[updatePayloadKey]bool, len(podGroups))
	for i := range podGroups {
		key := su.keyForPodGroupPayload(podGroups[i].Name, podGroups[i].Namespace, podGroups[i].UID)
		usedKeys[key] = true
		pgLatestUpdate, inFlightUpdateFound, appliedUpdateFound := su.getLatestPgUpdate(key)
		if !inFlightUpdateFound && !appliedUpdateFound {
			continue
		}
		podGroup := pgLatestUpdate.object.(*enginev2alpha2.PodGroup)
		podGroupsyncResults := su.syncPodGroup(podGroup, podGroups[i])
		// Delete the inflight update if it was applied + the pod group in the lister matches the inFlight
		if podGroupsyncResults != snapshotStatusIsOlder && appliedUpdateFound {
			su.appliedPodGroupUpdates.Delete(key)
		} else if podGroupsyncResults == updateRequestIsOlder && inFlightUpdateFound {
			su.inFlightPodGroups.Delete(key)
		}
	}

	// Cleanup podGroups that don't comeup anymore
	su.cleanUpdatesForNonSeenPodGroups(usedKeys)
}

func (su *defaultStatusUpdater) getLatestPgUpdate(key updatePayloadKey) (*inflightUpdate, bool, bool) {
	inflightPgUpdate, inFlightUpdateFound := su.inFlightPodGroups.Load(key)
	appliedPgUpdate, appliedUpdateFound := su.appliedPodGroupUpdates.Load(key)
	var pgLatestUpdate *inflightUpdate
	if inFlightUpdateFound {
		pgLatestUpdate = inflightPgUpdate.(*inflightUpdate)
	} else if appliedUpdateFound {
		pgLatestUpdate = appliedPgUpdate.(*inflightUpdate)
	}
	return pgLatestUpdate, inFlightUpdateFound, appliedUpdateFound
}

func (su *defaultStatusUpdater) cleanUpdatesForNonSeenPodGroups(usedKeys map[updatePayloadKey]bool) {
	su.inFlightPodGroups.Range(func(key any, _ any) bool {
		if _, found := usedKeys[key.(updatePayloadKey)]; !found {
			su.inFlightPodGroups.Delete(key)
		}
		return true
	})
	su.appliedPodGroupUpdates.Range(func(key any, _ any) bool {
		if _, found := usedKeys[key.(updatePayloadKey)]; !found {
			su.appliedPodGroupUpdates.Delete(key)
		}
		return true
	})
}

func (su *defaultStatusUpdater) syncPodGroup(inFlightPodGroup, snapshotPodGroup *enginev2alpha2.PodGroup) podGroupStatusSyncResult {
	staleTimeStampUpdated := false
	if snapshotPodGroup.Annotations[commonconstants.StalePodgroupTimeStamp] == inFlightPodGroup.Annotations[commonconstants.StalePodgroupTimeStamp] {
		staleTimeStampUpdated = true
	} else {
		if snapshotPodGroup.Annotations == nil {
			snapshotPodGroup.Annotations = make(map[string]string)
		}
		snapshotPodGroup.Annotations[commonconstants.StalePodgroupTimeStamp] = inFlightPodGroup.Annotations[commonconstants.StalePodgroupTimeStamp]
	}
	lastStartTimestampUpdated := false
	if snapshotPodGroup.Annotations[commonconstants.LastStartTimeStamp] == inFlightPodGroup.Annotations[commonconstants.LastStartTimeStamp] {
		lastStartTimestampUpdated = true
	} else {
		if snapshotPodGroup.Annotations == nil {
			snapshotPodGroup.Annotations = make(map[string]string)
		}
		snapshotPodGroup.Annotations[commonconstants.LastStartTimeStamp] = inFlightPodGroup.Annotations[commonconstants.LastStartTimeStamp]
	}

	statusComparison := compareSchedulingConditions(inFlightPodGroup, snapshotPodGroup)

	if statusComparison == equalStatuses || statusComparison == snapshotStatusIsOlder {
		snapshotPodGroup.Status.SchedulingConditions = inFlightPodGroup.Status.SchedulingConditions
	}
	if statusComparison == equalStatuses && (!lastStartTimestampUpdated || !staleTimeStampUpdated) {
		statusComparison = snapshotStatusIsOlder
	}

	return statusComparison
}

func compareSchedulingConditions(inFlightPodGroup, snapshotPodGroup *enginev2alpha2.PodGroup) podGroupStatusSyncResult {
	lastSchedulingCondition := utils.GetLastSchedulingCondition(inFlightPodGroup)
	currentLastSchedulingCondition := utils.GetLastSchedulingCondition(snapshotPodGroup)

	if currentLastSchedulingCondition == nil && lastSchedulingCondition == nil {
		return equalStatuses
	}
	if currentLastSchedulingCondition == nil {
		return snapshotStatusIsOlder
	}
	if lastSchedulingCondition == nil {
		return updateRequestIsOlder
	}

	currentID, currentErr := strconv.Atoi(currentLastSchedulingCondition.TransitionID)
	lastID, lastErr := strconv.Atoi(lastSchedulingCondition.TransitionID)

	// If an ID is invalid, use the other condition as the "newer" one
	if currentErr != nil {
		return snapshotStatusIsOlder
	} else if lastErr != nil {
		return updateRequestIsOlder
	}

	// Compare valid IDs
	switch {
	case lastID > currentID || currentID == 0:
		return snapshotStatusIsOlder
	case lastID == currentID:
		return equalStatuses
	default: // lastID < currentID
		return updateRequestIsOlder
	}
}
