// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package kubeflow

import (
	"k8s.io/kubernetes/pkg/util/slice"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const (
	podRoleLabelKey = "training.kubeflow.org/job-role"
)

var (
	masterRoleValues = []string{"master", "launcher"}
)

type kubeflowPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &kubeflowPlugin{}
}

func (pp *kubeflowPlugin) Name() string {
	return "kubeflow"
}

func (pp *kubeflowPlugin) OnSessionOpen(ssn *framework.Session) {
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

func (pp *kubeflowPlugin) OnSessionClose(_ *framework.Session) {}
