// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

const (
	PodFitsHostPorts       = "PodFitsHostPorts"
	PodToleratesNodeTaints = "PodToleratesNodeTaints"
	NodeAffinity           = "NodeAffinity"
	PodAffinity            = "PodAffinity"
	VolumeBinding          = "VolumeBinding"
	DynamicResources       = "DynamicResources"
	NodeScheduler          = "NodeScheduler"
	MaxNodePoolResources   = "MaxNodePoolResources"
	ConfigMap              = "ConfigMap"
)

func predicateRequired(_ *v1.Pod) bool {
	return true
}

func predicateNotRequired(_ *v1.Pod) bool {
	return false
}

func emptyPredicatePreFilter(predicateName string) k8s_internal.FitPredicatePreFilter {
	return func(pod *v1.Pod) (sets.Set[string], *ksf.Status) {
		log.InfraLogger.V(6).Infof(
			"Checking pod %s/%s failed predicate: %s", pod.Namespace, pod.Name, predicateName)
		return nil, nil
	}
}

func emptyPredicateFilter(predicateName string) k8s_internal.FitPredicateFilter {
	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (bool, []string, error) {
		log.InfraLogger.V(6).Infof(
			"Checking pod %s/%s on node %s with failed predicate: %s",
			pod.Namespace, pod.Name, nodeInfo.Node().Name, predicateName)
		return true, nil, nil
	}
}

func emptyPredicate(predicateName string) k8s_internal.SessionPredicate {
	predicate := k8s_internal.SessionPredicate{
		IsPreFilterRequired: predicateRequired,
		PreFilter:           emptyPredicatePreFilter(predicateName),
		IsFilterRequired:    predicateRequired,
		Filter:              emptyPredicateFilter(predicateName),
		Name:                predicateName,
	}
	return predicate
}

func NewSessionPredicates(ssn *framework.Session) k8s_internal.SessionPredicates {
	initiatedPlugins := ssn.InternalK8sPlugins()
	predicates := k8s_internal.SessionPredicates{}

	if plugin := initiatedPlugins.NodePorts; plugin == nil {
		predicates[PodFitsHostPorts] = emptyPredicate(PodFitsHostPorts)
	} else {
		predicates[PodFitsHostPorts] = k8s_internal.SessionPredicate{
			Name:                PodFitsHostPorts,
			IsPreFilterRequired: predicateRequired,
			PreFilter:           k8s_internal.FitPrePredicateConverter(ssn, ssn, plugin.(*nodeports.NodePorts)),
			IsFilterRequired:    predicateRequired,
			Filter:              k8s_internal.FitPredicateConverter(ssn, plugin.(*nodeports.NodePorts)),
		}
	}

	if plugin := initiatedPlugins.TaintToleration; plugin == nil {
		predicates[PodToleratesNodeTaints] = emptyPredicate(PodToleratesNodeTaints)
	} else {
		predicates[PodToleratesNodeTaints] = k8s_internal.SessionPredicate{
			Name:                PodToleratesNodeTaints,
			IsPreFilterRequired: predicateNotRequired,
			PreFilter:           nil,
			IsFilterRequired:    predicateRequired,
			Filter:              k8s_internal.FitPredicateConverter(ssn, plugin.(*tainttoleration.TaintToleration)),
		}
	}

	if plugin := initiatedPlugins.NodeAffinity; plugin == nil {
		predicates[NodeAffinity] = emptyPredicate(NodeAffinity)
	} else {
		predicates[NodeAffinity] = k8s_internal.SessionPredicate{
			Name:                NodeAffinity,
			IsPreFilterRequired: predicateRequired,
			PreFilter:           k8s_internal.FitPrePredicateConverter(ssn, ssn, plugin.(*nodeaffinity.NodeAffinity)),
			IsFilterRequired:    predicateRequired,
			Filter:              k8s_internal.FitPredicateConverter(ssn, plugin.(*nodeaffinity.NodeAffinity)),
		}
	}

	if plugin := initiatedPlugins.PodAffinity; plugin == nil {
		predicates[PodAffinity] = emptyPredicate(PodAffinity)
	} else {
		predicates[PodAffinity] = k8s_internal.SessionPredicate{
			Name:                PodAffinity,
			IsPreFilterRequired: predicateRequired,
			PreFilter:           k8s_internal.FitPrePredicateConverter(ssn, ssn, plugin.(*interpodaffinity.InterPodAffinity)),
			IsFilterRequired:    predicateRequired,
			Filter:              k8s_internal.FitPredicateConverter(ssn, plugin.(*interpodaffinity.InterPodAffinity)),
		}
	}

	if plugin := initiatedPlugins.VolumeBinding; plugin == nil {
		predicates[VolumeBinding] = emptyPredicate(VolumeBinding)
	} else {
		predicates[VolumeBinding] = k8s_internal.SessionPredicate{
			Name:                VolumeBinding,
			IsPreFilterRequired: predicateRequired,
			PreFilter:           k8s_internal.FitPrePredicateConverter(ssn, ssn, plugin.(*volumebinding.VolumeBinding)),
			IsFilterRequired:    predicateRequired,
			Filter:              NewVolumeBindingFilter(ssn, plugin, ssn.ScheduleCSIStorage()),
		}
	}

	if plugin := initiatedPlugins.DynamicResources; plugin == nil {
		predicates[DynamicResources] = emptyPredicate(DynamicResources)
	} else {
		predicates[DynamicResources] = k8s_internal.SessionPredicate{
			Name:                DynamicResources,
			IsPreFilterRequired: predicateRequired,
			PreFilter:           k8s_internal.FitPrePredicateConverter(ssn, ssn, plugin.(k8sframework.PreFilterPlugin)),
			IsFilterRequired:    predicateRequired,
			Filter:              k8s_internal.FitPredicateConverter(ssn, plugin.(k8sframework.FilterPlugin)),
		}
	}

	mnrPredicate := NewMaxNodeResourcesPredicate(ssn.Nodes, ssn.NodePoolName())
	predicates[MaxNodePoolResources] = k8s_internal.SessionPredicate{
		Name:                NodeScheduler,
		IsPreFilterRequired: mnrPredicate.isPreFilterRequired,
		PreFilter:           k8s_internal.FitPrePredicateConverter(ssn, ssn, mnrPredicate),
		IsFilterRequired:    mnrPredicate.isFilterRequired,
		Filter:              nil,
	}

	cmPredicate := NewConfigMapPredicate(ssn.ConfigMaps)
	predicates[ConfigMap] = k8s_internal.SessionPredicate{
		Name:                ConfigMap,
		IsPreFilterRequired: cmPredicate.isPreFilterRequired,
		PreFilter:           k8s_internal.FitPrePredicateConverter(ssn, ssn, cmPredicate),
		IsFilterRequired:    cmPredicate.isFilterRequired,
		Filter:              nil,
	}

	return predicates
}

func emptyScoreFn(pluginName string) k8s_internal.ScorePredicate {
	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (int64, []string, error) {
		log.InfraLogger.V(6).Infof(
			"Scoring pod %s/%s on node %s with failed plugin: %s",
			pod.Namespace, pod.Name, nodeInfo.Node().Name, pluginName)
		return 0, nil, nil
	}
}

func emptyPreScoreFn(_ string) k8s_internal.PreScoreFn {
	return func(pod *v1.Pod, _ []ksf.NodeInfo) *ksf.Status {
		return nil
	}
}

func NewSessionScorePredicates(ssn *framework.Session) *k8s_internal.SessionScoreFns {
	initiatedPlugins := ssn.InternalK8sPlugins()
	scoreFns := &k8s_internal.SessionScoreFns{}

	if plugin := initiatedPlugins.PodAffinity; plugin == nil {
		scoreFns.PrePodAffinity = emptyPreScoreFn("PodAffinity")
		scoreFns.PodAffinity = emptyScoreFn("PodAffinity")
	} else {
		scoreFns.PrePodAffinity = k8s_internal.PreScorePluginConverter(
			ssn, plugin.(*interpodaffinity.InterPodAffinity))
		scoreFns.PodAffinity = k8s_internal.ScorePluginConverter(
			ssn, plugin.(*interpodaffinity.InterPodAffinity))
	}

	return scoreFns
}
