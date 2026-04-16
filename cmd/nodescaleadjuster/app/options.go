// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/consts"
)

type Options struct {
	ScalingPodImage          string
	ScalingPodNamespace      string
	SchedulerName            string
	GPUMemoryToFractionRatio float64
	ScalingPodAppLabel       string
	ScalingPodServiceAccount string
}

// NewOptions creates a new Options
func NewOptions() *Options {
	s := Options{}
	return &s
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet
func (s *Options) AddFlags() {
	flag.StringVar(&s.ScalingPodImage,
		"scaling-pod-image", consts.DefaultScalingPodImage,
		"The image to use for the scaling pod, defaults to "+consts.DefaultScalingPodImage)
	flag.StringVar(&s.ScalingPodNamespace,
		"scale-adjust-namespace", "kai-scale-adjust",
		"The namespace to use for the scaling pods, defaults to kai-scale-adjust")
	flag.StringVar(&s.SchedulerName,
		"scheduler-name", "kai-scheduler",
		"Scheduler name, defaults to kai-scheduler")
	flag.StringVar(&s.ScalingPodAppLabel,
		"scaling-pod-app-label", "scaling-pod",
		"Scaling pod app label")
	flag.StringVar(&s.ScalingPodServiceAccount,
		"scaling-pod-service-account", "scaling-pod",
		"Scaling pod service account name")
	flag.Float64Var(&s.GPUMemoryToFractionRatio,
		"gpu-memory-to-fraction-ratio", consts.DefaultGPUMemoryToFractionRatio,
		fmt.Sprintf("The ratio of GPU memory to fraction, defaults to %f", consts.DefaultGPUMemoryToFractionRatio))
}
