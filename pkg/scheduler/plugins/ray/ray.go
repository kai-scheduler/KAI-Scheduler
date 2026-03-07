// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package ray

import (
	"k8s.io/kubernetes/pkg/util/slice"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
)

const (
	podRoleLabelKey = "ray.io/node-type"
)

var (
	masterRoleValues = []string{"head"}
)

type rayPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &rayPlugin{}
}

func (pp *rayPlugin) Name() string {
	return "ray"
}

func (pp *rayPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddTaskOrderFn(TaskOrderFn)
}

func TaskOrderFn(l, r interface{}) int {
	lv := l.(*pod_info.PodInfo)
	rv := r.(*pod_info.PodInfo)

	lPodRole, lLabelExists := lv.Pod.Labels[podRoleLabelKey]
	rPodRole, rLabelExists := rv.Pod.Labels[podRoleLabelKey]

	lPodMasterRole := lLabelExists && slice.ContainsString(masterRoleValues, lPodRole, nil)
	rPodMasterRole := rLabelExists && slice.ContainsString(masterRoleValues, rPodRole, nil)

	if lPodMasterRole && !rPodMasterRole {
		return -1
	}
	if !lPodMasterRole && rPodMasterRole {
		return 1
	}
	return 0
}

func (pp *rayPlugin) OnSessionClose(_ *framework.Session) {}
