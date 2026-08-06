// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
)

func (r *Redactor) RedactSnapshot(snap *snapshot.Snapshot) error {
	if snap == nil || snap.RawObjects == nil {
		return nil
	}

	if snap.Config != nil && snap.Config.UsageDBConfig != nil {
		snap.Config.UsageDBConfig = nil
	}

	raw := snap.RawObjects

	for _, pod := range raw.Pods {
		if pod == nil {
			continue
		}
		r.redactPod(pod)
		r.mu.Lock()
		r.stats.PodsRedacted++
		r.mu.Unlock()
	}

	for _, node := range raw.Nodes {
		if node == nil {
			continue
		}
		r.redactNode(node)
		r.mu.Lock()
		r.stats.NodesRedacted++
		r.mu.Unlock()
	}

	r.redactQueues(raw)
	r.redactBindRequests(raw)
	r.redactPriorityClasses(raw)
	r.redactConfigMaps(raw)
	r.redactPersistentVolumes(raw)
	r.redactPersistentVolumeClaims(raw)
	r.redactCSIStorageCapacities(raw)
	r.redactStorageClasses(raw)
	r.redactCSIDrivers(raw)
	r.redactPodGroups(raw)
	r.redactResourceClaims(raw)
	r.redactResourceSlices(raw)
	r.redactDeviceClasses(raw)
	r.redactTopologies(raw)

	return nil
}
