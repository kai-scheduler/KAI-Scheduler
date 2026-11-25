// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"encoding/json"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/api/constants"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func schedulingConditionKey(nodePool string) string {
	key, _ := json.Marshal(map[string]string{"nodePool": nodePool})
	return "k:" + string(key)
}

func statusFields(entry metav1.ManagedFieldsEntry) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if entry.FieldsV1 == nil {
		return fields, nil, nil
	}
	if err := json.Unmarshal(entry.FieldsV1.Raw, &fields); err != nil {
		return nil, nil, err
	}
	var status map[string]json.RawMessage
	if raw, ok := fields["f:status"]; ok {
		if err := json.Unmarshal(raw, &status); err != nil {
			return nil, nil, err
		}
	}
	return fields, status, nil
}

// Legacy UpdateStatus writers owned whole lists. Remove only scheduler fields; retain other controllers' ownership.
func migrateSchedulingStatusFields(podGroup *enginev2alpha2.PodGroup, manager, nodePool string, includeState bool) (bool, error) {
	changed := false
	entries := make([]metav1.ManagedFieldsEntry, 0, len(podGroup.ManagedFields))
	for _, entry := range podGroup.ManagedFields {
		if entry.Subresource != "status" || (entry.Operation != metav1.ManagedFieldsOperationUpdate &&
			!(includeState && entry.Manager != manager && strings.HasPrefix(entry.Manager, schedulingStatusManagerPrefix))) {
			entries = append(entries, entry)
			continue
		}
		fields, status, err := statusFields(entry)
		if err != nil {
			return false, err
		}
		modified := false
		if raw, ok := status["f:schedulingConditions"]; ok && entry.Operation == metav1.ManagedFieldsOperationUpdate {
			var conditions map[string]json.RawMessage
			if err := json.Unmarshal(raw, &conditions); err != nil {
				return false, err
			}
			if len(conditions) == 0 {
				// Preserve other pools' ownership so omission cannot prune their historical entries.
				for _, condition := range podGroup.Status.SchedulingConditions {
					if condition.NodePool != nodePool {
						conditions[schedulingConditionKey(condition.NodePool)] = json.RawMessage(`{}`)
					}
				}
				if len(conditions) == 0 {
					delete(status, "f:schedulingConditions")
				} else {
					status["f:schedulingConditions"], err = json.Marshal(conditions)
					if err != nil {
						return false, err
					}
				}
				modified = true
			} else if _, ok := conditions[schedulingConditionKey(nodePool)]; ok {
				delete(conditions, schedulingConditionKey(nodePool))
				if len(conditions) == 0 {
					delete(status, "f:schedulingConditions")
				} else {
					status["f:schedulingConditions"], err = json.Marshal(conditions)
					if err != nil {
						return false, err
					}
				}
				modified = true
			}
		}
		if includeState {
			if _, ok := status["f:schedulingState"]; ok {
				delete(status, "f:schedulingState")
				modified = true
			}
		}
		if !modified {
			entries = append(entries, entry)
			continue
		}
		changed = true
		if len(status) == 0 {
			delete(fields, "f:status")
		} else {
			fields["f:status"], err = json.Marshal(status)
			if err != nil {
				return false, err
			}
		}
		if len(fields) == 0 {
			continue
		}
		entry.FieldsV1 = &metav1.FieldsV1{}
		entry.FieldsV1.Raw, err = json.Marshal(fields)
		if err != nil {
			return false, err
		}
		entries = append(entries, entry)
	}
	if changed {
		podGroup.ManagedFields = entries
	}
	return changed, nil
}

func ownsSchedulingCondition(podGroup *enginev2alpha2.PodGroup, manager, nodePool string) bool {
	for _, entry := range podGroup.ManagedFields {
		if entry.Manager != manager || entry.Operation != metav1.ManagedFieldsOperationApply || entry.Subresource != "status" {
			continue
		}
		_, status, err := statusFields(entry)
		if err != nil {
			continue
		}
		var conditions map[string]json.RawMessage
		if json.Unmarshal(status["f:schedulingConditions"], &conditions) == nil {
			if _, ok := conditions[schedulingConditionKey(nodePool)]; ok {
				return true
			}
		}
	}
	return false
}

func normalizeSchedulingConditions(podGroup *enginev2alpha2.PodGroup) bool {
	indices := make(map[string]int, len(podGroup.Status.SchedulingConditions))
	conditions := make([]enginev2alpha2.SchedulingCondition, 0, len(podGroup.Status.SchedulingConditions))
	changed := false
	for _, condition := range podGroup.Status.SchedulingConditions {
		if condition.NodePool == "" {
			condition.NodePool = commonconstants.DefaultNodePoolName
			changed = true
		}
		if index, found := indices[condition.NodePool]; found {
			previousID, _ := strconv.Atoi(conditions[index].TransitionID)
			currentID, _ := strconv.Atoi(condition.TransitionID)
			if currentID >= previousID {
				conditions[index] = condition
			}
			changed = true
		} else {
			indices[condition.NodePool] = len(conditions)
			conditions = append(conditions, condition)
		}
	}
	if changed {
		podGroup.Status.SchedulingConditions = conditions
	}
	return changed
}
