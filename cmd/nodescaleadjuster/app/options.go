// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/consts"
)

type Options struct {
	ScalingPodImage          string
	ScalingPodNamespace      string
	SchedulerName            string
	GPUMemoryToFractionRatio float64
	ScalingPodAppLabel       string
	ScalingPodServiceAccount string
	EnableLeaderElection     bool

	// k8s client options
	Qps   int
	Burst int
}

// NewOptions creates a new Options
func NewOptions() *Options {
	s := Options{}
	return &s
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet
func (o *Options) AddFlags() {
	flag.StringVar(&o.ScalingPodImage,
		"scaling-pod-image", consts.DefaultScalingPodImage,
		"The image to use for the scaling pod, defaults to "+consts.DefaultScalingPodImage)
	flag.StringVar(&o.ScalingPodNamespace,
		"scale-adjust-namespace", constants.DefaultScaleAdjustName,
		"The namespace to use for the scaling pods, defaults to kai-scale-adjust")
	flag.StringVar(&o.SchedulerName,
		"scheduler-name", constants.DefaultSchedulerName,
		"Scheduler name, defaults to kai-scheduler")
	flag.StringVar(&o.ScalingPodAppLabel,
		"scaling-pod-app-label", "scaling-pod",
		"Scaling pod app label")
	flag.StringVar(&o.ScalingPodServiceAccount,
		"scaling-pod-service-account", "scaling-pod",
		"Scaling pod service account name")
	flag.Float64Var(&o.GPUMemoryToFractionRatio,
		"gpu-memory-to-fraction-ratio", consts.DefaultGPUMemoryToFractionRatio,
		fmt.Sprintf("The ratio of GPU memory to fraction, defaults to %f", consts.DefaultGPUMemoryToFractionRatio))
	flag.BoolVar(&o.EnableLeaderElection,
		"leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&o.Qps, "qps", 50, "Queries per second to the K8s API server")
	flag.IntVar(&o.Burst, "burst", 300, "Burst to the K8s API server")
}
