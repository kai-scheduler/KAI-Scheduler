// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podaffinity

import (
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal/predicates"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/scores"
)

type skipOrderFn map[common_info.PodID]bool

func (sp skipOrderFn) add(podID common_info.PodID) {
	sp[podID] = true
}

func (sp skipOrderFn) shouldSkip(podID common_info.PodID) bool {
	return sp[podID]
}

type podAffinityPlugin struct {
	// Arguments given for the plugin
	pluginArguments map[string]string

	skipOrderFn skipOrderFn
}

func New(arguments map[string]string) framework.Plugin {
	return &podAffinityPlugin{pluginArguments: arguments}
}

func (pp *podAffinityPlugin) Name() string {
	return "podaffinity"
}

func (pp *podAffinityPlugin) OnSessionOpen(ssn *framework.Session) {
	k8sPluginScoreres := predicates.NewSessionScorePredicates(ssn)

	ssn.AddNodePreOrderFn(pp.nodePreOrderFn(k8sPluginScoreres))
	ssn.AddNodeOrderFn(pp.nodeOrderFn(k8sPluginScoreres))
	pp.skipOrderFn = make(skipOrderFn)
}

func (pp *podAffinityPlugin) nodePreOrderFn(k8sPlugins *k8s_internal.SessionScoreFns) api.NodePreOrderFn {
	return func(task *pod_info.PodInfo, fittingNodes []*node_info.NodeInfo) error {
		var nodes []ksf.NodeInfo
		for range fittingNodes {
			nodes = append(nodes, &k8sframework.NodeInfo{})
		}

		status := k8sPlugins.PrePodAffinity(task.Pod, nodes)
		if status.IsSkip() {
			pp.skipOrderFn.add(task.UID)
		}

		return status.AsError()
	}
}

func (pp *podAffinityPlugin) nodeOrderFn(k8sPlugins *k8s_internal.SessionScoreFns) api.NodeOrderFn {
	return func(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		if pp.skipOrderFn.shouldSkip(task.UID) {
			return 0, nil
		}

		k8sNodeInfo := node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo
		score, reasons, err := k8sPlugins.PodAffinity(task.Pod, k8sNodeInfo)
		if err != nil {
			log.InfraLogger.V(6).Infof(
				"Pod Affinity plugin failed to score on Task <%s/%s> on Node <%s>: reasons %v, err %v",
				task.Namespace, task.Name, node.Name, reasons, err)
			return 0, err
		}
		return scores.K8sPlugins * float64(score), nil
	}
}

func (pp *podAffinityPlugin) OnSessionClose(_ *framework.Session) {}
