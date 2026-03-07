// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/spf13/pflag"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

type Options struct {
	SchedulerName               string
	QPS                         float64
	Burst                       int
	RateLimiterBaseDelaySeconds int
	RateLimiterMaxDelaySeconds  int
	EnableLeaderElection        bool
	MetricsAddr                 string
	ProbeAddr                   string
	WebhookPort                 int
	FakeGPUNodes                bool
	GPUSharingEnabled           bool
	GPUPodRuntimeClassName      string
}

func InitOptions() *Options {
	options := &Options{}

	fs := pflag.CommandLine

	fs.StringVar(&options.SchedulerName,
		"scheduler-name", constants.DefaultSchedulerName,
		"The scheduler name the workloads are scheduled with")
	fs.Float64Var(&options.QPS,
		"qps", 50,
		"Queries per second to the K8s API server")
	fs.IntVar(&options.Burst,
		"burst", 300,
		"Burst to the K8s API server")
	fs.IntVar(&options.RateLimiterBaseDelaySeconds,
		"rate-limiter-base-delay", 1,
		"Base delay in seconds for the ExponentialFailureRateLimiter")
	fs.IntVar(&options.RateLimiterMaxDelaySeconds,
		"rate-limiter-max-delay", 60,
		"Max delay in seconds for the ExponentialFailureRateLimiter")
	fs.BoolVar(&options.EnableLeaderElection,
		"leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.StringVar(&options.MetricsAddr,
		"metrics-bind-address", ":8080",
		"The address the metric endpoint binds to.")
	fs.StringVar(&options.ProbeAddr,
		"health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")
	fs.IntVar(&options.WebhookPort,
		"webhook-addr", 9443,
		"The port the webhook binds to.")
	fs.BoolVar(&options.FakeGPUNodes,
		"fake-gpu-nodes", false,
		"Enables running fractions on fake gpu nodes for testing")
	fs.BoolVar(&options.GPUSharingEnabled,
		"gpu-sharing-enabled", false,
		"Specifies if the GPU sharing is enabled")
	fs.StringVar(&options.GPUPodRuntimeClassName,
		"gpu-pod-runtime-class-name", constants.DefaultRuntimeClassName,
		fmt.Sprintf("Runtime class to be set for GPU pods (defaults to %s) Set to empty string to disable", constants.DefaultRuntimeClassName))

	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	return options
}
