// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package elastic

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type elasticPlugin struct {
	// Arguments given for the plugin
	pluginArguments map[string]string
}

func New(arguments map[string]string) framework.Plugin {
	return &elasticPlugin{pluginArguments: arguments}
}

func (pp *elasticPlugin) Name() string {
	return "elastic"
}

func (pp *elasticPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddJobOrderFn(JobOrderFn)
}

func JobOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	lvAllocatedCount := int32(lv.GetActiveAllocatedTasksCount())
	rvAllocatedCount := int32(rv.GetActiveAllocatedTasksCount())

	if lvAllocatedCount < lv.MinAvailable && rvAllocatedCount >= rv.MinAvailable {
		return -1
	}

	if lvAllocatedCount == lv.MinAvailable && rvAllocatedCount > rv.MinAvailable {
		return -1
	}

	if lvAllocatedCount >= lv.MinAvailable && rvAllocatedCount < rv.MinAvailable {
		return 1
	}

	if lvAllocatedCount > lv.MinAvailable && rvAllocatedCount == rv.MinAvailable {
		return 1
	}

	// TODO: consider the number of extra pods for elastic jobs?

	return 0
}

func (pp *elasticPlugin) OnSessionClose(_ *framework.Session) {}
