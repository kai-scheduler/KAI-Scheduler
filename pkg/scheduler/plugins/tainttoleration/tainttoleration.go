// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package tainttoleration deprioritizes nodes with PreferNoSchedule taints the pod does not
// tolerate. Hard taint effects are filtered by the predicates plugin.
package tainttoleration

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/predicates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

type taintTolerationPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &taintTolerationPlugin{}
}

func (tp *taintTolerationPlugin) Name() string {
	return "tainttoleration"
}

func (tp *taintTolerationPlugin) OnSessionOpen(ssn *framework.Session) {
	k8sPluginScorers := predicates.NewSessionScorePredicates(ssn)

	ssn.AddNodePreOrderFn(nodePreOrderFn(k8sPluginScorers))
	ssn.AddNodeOrderFn(nodeOrderFn(k8sPluginScorers))
}

// Upstream Score reads the tolerations that PreScore writes to the session state.
func nodePreOrderFn(k8sPlugins *k8s_internal.SessionScoreFns) api.NodePreOrderFn {
	return func(task *pod_info.PodInfo, _ []*node_info.NodeInfo) error {
		return k8sPlugins.PreTaintToleration(task.Pod, nil).AsError()
	}
}

// Upstream Score counts intolerable PreferNoSchedule taints and relies on NormalizeScore to
// reverse it. KAI sums raw scores, so the count is negated into a penalty.
func nodeOrderFn(k8sPlugins *k8s_internal.SessionScoreFns) api.NodeOrderFn {
	return func(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		k8sNodeInfo := node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo
		intolerableTaints, reasons, err := k8sPlugins.TaintToleration(task.Pod, k8sNodeInfo)
		if err != nil {
			log.InfraLogger.V(6).Do(func() {
				log.InfraLogger.Infof(
					"Taint toleration plugin failed to score Task <%s/%s> on Node <%s>: reasons %v, err %v",
					task.Namespace, task.Name, node.Name, reasons, err)
			})
			return 0, err
		}
		return -scores.K8sPlugins * float64(intolerableTaints), nil
	}
}

func (tp *taintTolerationPlugin) OnSessionClose(_ *framework.Session) {}
