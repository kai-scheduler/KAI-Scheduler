// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

type Options struct {
	MetricsAddr                  string
	EnableLeaderElection         bool
	SkipControllerNameValidation bool // Set true for env tests
	ProbeAddr                    string
	Qps                          int
	Burst                        int
	MaxConcurrentReconciles      int
	LogLevel                     int
	SchedulerName                string
	EnablePodGroupWebhook        bool
}

func InitOptions(fs *flag.FlagSet) *Options {
	options := &Options{}

	if fs == nil {
		fs = flag.CommandLine
	}

	fs.StringVar(&options.MetricsAddr, "metrics-bind-address", ":8080",
		"The address the metric endpoint binds to.")
	fs.StringVar(&options.ProbeAddr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")
	fs.BoolVar(&options.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&options.SkipControllerNameValidation, "skip-controller-name-validation", false, "Skip controller name validation.")
	fs.IntVar(&options.Qps, "qps", 50,
		"Queries per second to the K8s API server")
	fs.IntVar(&options.Burst, "burst", 300,
		"Burst to the K8s API server")
	fs.IntVar(&options.MaxConcurrentReconciles, "max-concurrent-reconciles", 10,
		"Max concurrent reconciles")
	fs.IntVar(&options.LogLevel, "log-level", 3,
		"Log level")
	fs.StringVar(&options.SchedulerName, "scheduler-name", constants.DefaultSchedulerName,
		"The name of the scheduler used to schedule pod groups")
	fs.BoolVar(&options.EnablePodGroupWebhook, "enable-podgroup-webhook", true,
		"Enable podgroup webhook")

	return options
}
