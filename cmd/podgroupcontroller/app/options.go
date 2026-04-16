// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

type Options struct {
	MetricsAddr             string
	EnableLeaderElection    bool
	ProbeAddr               string
	Qps                     int
	Burst                   int
	MaxConcurrentReconciles int
	LogLevel                int
	SchedulerName           string
}

func InitOptions() *Options {
	options := &Options{}

	flag.StringVar(&options.MetricsAddr, "metrics-bind-address", ":8080",
		"The address the metric endpoint binds to.")
	flag.StringVar(&options.ProbeAddr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")
	flag.BoolVar(&options.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&options.Qps, "qps", 50,
		"Queries per second to the K8s API server")
	flag.IntVar(&options.Burst, "burst", 300,
		"Burst to the K8s API server")
	flag.IntVar(&options.MaxConcurrentReconciles, "max-concurrent-reconciles", 10,
		"Max concurrent reconciles")
	flag.IntVar(&options.LogLevel, "log-level", 3,
		"Log level")
	flag.StringVar(&options.SchedulerName, "scheduler-name", constants.DefaultSchedulerName,
		"The name of the scheduler used to schedule pod groups")

	return options
}
