// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package taskorder

import (
	"strconv"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants/labels"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
)

type taskOrderPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &taskOrderPlugin{}
}

func (pp *taskOrderPlugin) Name() string {
	return "taskorder"
}

func (pp *taskOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddTaskOrderFn(TaskOrderFn)
}

func TaskOrderFn(l, r interface{}) int {
	lv := l.(*pod_info.PodInfo)
	rv := r.(*pod_info.PodInfo)

	lPodPriorityString, lLabelExists := lv.Pod.Labels[labels.TaskOrderLabelKey]
	rPodPriorityString, rLabelExists := rv.Pod.Labels[labels.TaskOrderLabelKey]

	if lLabelExists && !rLabelExists {
		return -1
	}
	if !lLabelExists && rLabelExists {
		return 1
	}
	if !lLabelExists && !rLabelExists {
		return 0
	}

	lPodPriority, err := strconv.Atoi(lPodPriorityString)
	if err != nil {
		return 1
	}

	rPodPriority, err := strconv.Atoi(rPodPriorityString)
	if err != nil {
		return -1
	}

	if lPodPriority > rPodPriority {
		return -1
	}
	if lPodPriority < rPodPriority {
		return 1
	}

	return 0
}

func (pp *taskOrderPlugin) OnSessionClose(_ *framework.Session) {}
