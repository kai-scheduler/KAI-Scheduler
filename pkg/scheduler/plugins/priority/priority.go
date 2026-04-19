// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package priority

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type priorityPlugin struct {
	// Arguments given for the plugin
	pluginArguments map[string]string
}

func New(arguments map[string]string) framework.Plugin {
	return &priorityPlugin{pluginArguments: arguments}
}

func (pp *priorityPlugin) Name() string {
	return "priority"
}

func (pp *priorityPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddJobOrderFn(JobOrderFn)
}

func JobOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	if lv.Priority > rv.Priority {
		return -1
	}

	if lv.Priority < rv.Priority {
		return 1
	}

	return 0
}

func (pp *priorityPlugin) OnSessionClose(_ *framework.Session) {}
