// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"

	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	kaiflags "github.com/NVIDIA/KAI-scheduler/pkg/common/flags"
)

const (
	defaultMetricsAddress = ":8080"
)

type Options struct {
	EnableLeaderElection         bool
	EnableWebhook                bool
	SkipControllerNameValidation bool // Set true for env tests

	MetricsAddress                 string
	MetricsNamespace               string
	QueueLabelToMetricLabel        kaiflags.StringMapFlag
	QueueLabelToDefaultMetricValue kaiflags.StringMapFlag

	// k8s client options
	Qps   int
	Burst int
}

func InitOptions(fs *flag.FlagSet) *Options {
	o := &Options{}

	fs.BoolVar(&o.EnableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&o.EnableWebhook, "enable-webhook", true, "Enable webhook for controller manager.")
	fs.BoolVar(&o.SkipControllerNameValidation, "skip-controller-name-validation", false, "Skip controller name validation.")
	fs.StringVar(&o.MetricsAddress, "metrics-listen-address", defaultMetricsAddress, "The address the metrics endpoint binds to.")
	fs.StringVar(&o.MetricsNamespace, "metrics-namespace", constants.DefaultMetricsNamespace, "Metrics namespace.")
	fs.Var(&o.QueueLabelToMetricLabel, "queue-label-to-metric-label", "Map of queue label keys to metric label keys, e.g. 'foo=bar,baz=qux'.")
	fs.Var(&o.QueueLabelToDefaultMetricValue, "queue-label-to-default-metric-value", "Map of queue label keys to default metric values, in case the label doesn't exist on the queue, e.g. 'foo=1,baz=0'.")
	fs.IntVar(&o.Qps, "qps", 50, "Queries per second to the K8s API server")
	fs.IntVar(&o.Burst, "burst", 300, "Burst to the K8s API server")

	return o
}
