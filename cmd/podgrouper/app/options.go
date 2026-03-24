// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	controllers "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper"
)

type Options struct {
	MetricsAddr                            string
	ProbeAddr                              string
	EnableLeaderElection                   bool
	NodePoolLabelKey                       string
	QPS                                    int
	Burst                                  int
	MaxConcurrentReconciles                int
	SearchForLegacyPodGroups               bool
	KnativeGangSchedule                    bool
	SchedulerName                          string
	SchedulingQueueLabelKey                string
	PodLabelSelectorStr                    string
	NamespaceLabelSelectorStr              string
	DefaultConfigPerTypeConfigMapName      string
	DefaultConfigPerTypeConfigMapNamespace string
}

func (o *Options) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&o.EnableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	fs.StringVar(&o.NodePoolLabelKey, "nodepool-label-key", constants.DefaultNodePoolLabelKey, "The label key for node pools")
	fs.IntVar(&o.QPS, "qps", 50, "Queries per second to the K8s API server")
	fs.IntVar(&o.Burst, "burst", 300, "Burst to the K8s API server")
	fs.IntVar(&o.MaxConcurrentReconciles, "max-concurrent-reconciles", 10, "Max concurrent reconciles")
	fs.BoolVar(&o.SearchForLegacyPodGroups, "search-legacy-pg", true, "If this flag is enabled, try to find pod groups with legacy name format. If they exist, use the found pod groups instead of creating new once with current name format")
	fs.BoolVar(&o.KnativeGangSchedule, "knative-gang-schedule", true, "Schedule knative revision as a gang. Defaults to true")
	fs.StringVar(&o.SchedulerName, "scheduler-name", constants.DefaultSchedulerName, "The name of the scheduler used to schedule pod groups")
	fs.StringVar(&o.SchedulingQueueLabelKey, "queue-label-key", constants.DefaultQueueLabel, "Scheduling queue label key name")
	fs.StringVar(&o.DefaultConfigPerTypeConfigMapName, "default-priorities-configmap-name", "", "The name of the configmap that contains default configs (priorities and preemptibility) for pod groups")
	fs.StringVar(&o.DefaultConfigPerTypeConfigMapNamespace, "default-priorities-configmap-namespace", "", "The namespace of the configmap that contains default configs (priorities and preemptibility) for pod groups")
	flag.StringVar(&o.PodLabelSelectorStr, "pod-label-selector", "", "Pod label selector in key=value comma-separated format")
	flag.StringVar(&o.NamespaceLabelSelectorStr, "namespace-label-selector", "", "Namespace label selector in key=value comma-separated format")
}

func (o *Options) Configs() controllers.Configs {
	return controllers.Configs{
		NodePoolLabelKey:                       o.NodePoolLabelKey,
		MaxConcurrentReconciles:                o.MaxConcurrentReconciles,
		SearchForLegacyPodGroups:               o.SearchForLegacyPodGroups,
		KnativeGangSchedule:                    o.KnativeGangSchedule,
		SchedulerName:                          o.SchedulerName,
		SchedulingQueueLabelKey:                o.SchedulingQueueLabelKey,
		PodLabelSelector:                       parseLabelSelector(o.PodLabelSelectorStr),
		NamespaceLabelSelector:                 parseLabelSelector(o.NamespaceLabelSelectorStr),
		DefaultConfigPerTypeConfigMapName:      o.DefaultConfigPerTypeConfigMapName,
		DefaultConfigPerTypeConfigMapNamespace: o.DefaultConfigPerTypeConfigMapNamespace,
	}
}

func parseLabelSelector(labelStr string) map[string]string {
	labels := map[string]string{}
	if labelStr == "" {
		return labels
	}
	pairs := strings.Split(labelStr, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			labels[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return labels
}
