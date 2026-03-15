// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package schedulerplugins

import (
	"sync"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

var plugin *FairnessTrackerPlugin

func GetFairnessTrackerPlugin() *FairnessTrackerPlugin {
	return plugin
}

// QueueFairShareSnapshot stores the fair share for a queue at a point in time
type QueueFairShareSnapshot struct {
	QueueID   common_info.QueueID
	FairShare *resource_info.ResourceRequirements
}

type FairnessTrackerPlugin struct {
	// snapshots stores the fair share for each queue when the session opens
	snapshots map[common_info.QueueID]resource_info.ResourceRequirements
	mu        sync.RWMutex
}

func New(arguments framework.PluginArguments) framework.Plugin {
	if plugin != nil {
		return plugin
	}
	plugin = &FairnessTrackerPlugin{
		snapshots: map[common_info.QueueID]resource_info.ResourceRequirements{},
		mu:        sync.RWMutex{},
	}
	return plugin
}

func (pp *FairnessTrackerPlugin) Name() string {
	return "fairnessTracker"
}

func (pp *FairnessTrackerPlugin) OnSessionOpen(ssn *framework.Session) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	// Clear previous snapshots
	pp.snapshots = map[common_info.QueueID]resource_info.ResourceRequirements{}

	// Copy fair shares for all queues
	for queueID, queueInfo := range ssn.ClusterInfo.Queues {
		fairShare := ssn.QueueFairShare(queueInfo)
		if fairShare != nil {
			pp.snapshots[queueID] = *fairShare.Clone()
		}
	}
}

func (pp *FairnessTrackerPlugin) OnSessionClose(_ *framework.Session) {}

// GetSnapshots returns a copy of the current snapshots (thread-safe)
func (pp *FairnessTrackerPlugin) GetSnapshots() map[common_info.QueueID]resource_info.ResourceRequirements {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	snapshots := make(map[common_info.QueueID]resource_info.ResourceRequirements)
	for queueID, fairShare := range pp.snapshots {
		snapshots[queueID] = *fairShare.Clone()
	}
	return snapshots
}
