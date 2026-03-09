// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/spf13/pflag"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

type Options struct {
	SchedulerName                             string
	ResourceReservationNamespace              string
	ResourceReservationServiceAccount         string
	ResourceReservationPodImage               string
	ResourceReservationAppLabel               string
	ResourceReservationAllocationTimeout      int
	ResourceReservationPodResourcesJSON       string
	ResourceReservationPodSecurityContextJSON string
	ResourceReservationContainerSecurityJSON  string
	ScalingPodNamespace                       string
	QPS                                       float64
	Burst                                     int
	MaxConcurrentReconciles                   int
	RateLimiterBaseDelaySeconds               int
	RateLimiterMaxDelaySeconds                int
	EnableLeaderElection                      bool
	MetricsAddr                               string
	ProbeAddr                                 string
	FakeGPUNodes                              bool
	GpuCdiEnabled                             bool
	VolumeBindingTimeoutSeconds               int
	RuntimeClassName                          string
}

func InitOptions(fs *pflag.FlagSet) *Options {
	options := &Options{}

	if fs == nil {
		fs = pflag.CommandLine
	}

	fs.StringVar(&options.SchedulerName,
		"scheduler-name", constants.DefaultSchedulerName,
		"The scheduler name the workloads are scheduled with")
	fs.StringVar(&options.ResourceReservationNamespace,
		"resource-reservation-namespace", constants.DefaultResourceReservationName,
		"Namespace for resource reservation pods")
	fs.StringVar(&options.ResourceReservationServiceAccount,
		"resource-reservation-service-account", constants.DefaultResourceReservationName,
		"Service account name for resource reservation pods")
	fs.StringVar(&options.ResourceReservationPodImage,
		"resource-reservation-pod-image", "registry/local/kai-scheduler/resource-reservation",
		"Container image for the resource reservation pod")
	fs.StringVar(&options.ResourceReservationAppLabel,
		"resource-reservation-app-label", constants.DefaultResourceReservationName,
		"App label value of resource reservation pods")
	fs.IntVar(&options.ResourceReservationAllocationTimeout,
		"resource-reservation-allocation-timeout", 40,
		"Resource reservation allocation timeout in seconds")
	fs.StringVar(&options.ResourceReservationPodResourcesJSON,
		"resource-reservation-pod-resources", "",
		"JSON-serialized ResourceRequirements for GPU reservation pods (optional, empty means not set)")
	fs.StringVar(&options.ResourceReservationPodSecurityContextJSON,
		"resource-reservation-pod-security-context", "",
		"JSON-serialized PodSecurityContext for GPU reservation pods")
	fs.StringVar(&options.ResourceReservationContainerSecurityJSON,
		"resource-reservation-container-security-context", "",
		"JSON-serialized SecurityContext for GPU reservation pod containers")
	fs.StringVar(&options.ScalingPodNamespace,
		"scale-adjust-namespace", constants.DefaultScaleAdjustName,
		"Scaling pods namespace")
	fs.Float64Var(&options.QPS,
		"qps", 50,
		"Queries per second to the K8s API server")
	fs.IntVar(&options.Burst,
		"burst", 300,
		"Burst to the K8s API server")
	fs.IntVar(&options.MaxConcurrentReconciles,
		"max-concurrent-reconciles", 10,
		"Max concurrent reconciles")
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
	fs.BoolVar(&options.FakeGPUNodes,
		"fake-gpu-nodes", false,
		"Enables running fractions on fake gpu nodes for testing")
	fs.BoolVar(&options.GpuCdiEnabled,
		"cdi-enabled", false,
		"Specifies if the gpu device plugin uses the cdi devices api to set gpu devices to the pods")
	fs.IntVar(&options.VolumeBindingTimeoutSeconds,
		"volume-binding-timeout-seconds", 120,
		"Volume binding timeout in seconds")
	fs.StringVar(&options.RuntimeClassName,
		"runtime-class-name", "",
		"Runtime class for reservation pods")

	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	return options
}
