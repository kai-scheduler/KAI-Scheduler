// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/spf13/pflag"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

type Options struct {
	SchedulerName               string
	QPS                         float64
	Burst                       int
	EnableLeaderElection        bool
	MetricsAddr                 string
	ProbeAddr                   string
	WebhookPort                 int
	FakeGPUNodes                bool
	GPUSharingEnabled           bool
	HamiCoreEnabled             bool
	BlockNvidiaVisibleDevices   bool
	GPUPodRuntimeClassName      string
	GPUFractionRuntimeClassName string
	ValidatePodResizeQuota      bool
	BlockUpsizeOnBoundedQueues  bool
}

// ResolvedGPUFractionRuntimeClassName returns the effective runtime class name
// for fraction pods, preferring the new flag and falling back to the deprecated
// alias when only the deprecated one was set explicitly.
func (o *Options) ResolvedGPUFractionRuntimeClassName() string {
	if pflag.CommandLine.Changed("gpu-fraction-runtime-class-name") {
		return o.GPUFractionRuntimeClassName
	}
	if pflag.CommandLine.Changed("gpu-pod-runtime-class-name") {
		return o.GPUPodRuntimeClassName
	}
	return o.GPUFractionRuntimeClassName
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
	fs.BoolVar(&options.HamiCoreEnabled,
		"hami-core-enabled", false,
		"Specifies if the HAMI-core GPU memory limit injection is enabled")
	fs.BoolVar(&options.BlockNvidiaVisibleDevices,
		"block-nvidia-visible-devices", false,
		"Reject pods that set the NVIDIA_VISIBLE_DEVICES environment variable to values "+
			"that conflict with NVIDIA's device plugin (only 'void'/'none' are allowed)")
	fs.StringVar(&options.GPUPodRuntimeClassName,
		"gpu-pod-runtime-class-name", constants.DefaultRuntimeClassName,
		fmt.Sprintf("Deprecated: use --gpu-fraction-runtime-class-name. "+
			"Runtime class for GPU fraction pods (defaults to %s). "+
			"Set to empty string to disable.", constants.DefaultRuntimeClassName))
	fs.StringVar(&options.GPUFractionRuntimeClassName,
		"gpu-fraction-runtime-class-name", constants.DefaultRuntimeClassName,
		fmt.Sprintf("Runtime class to be set for GPU fraction pods (defaults to %s). "+
			"Whole-GPU pods are not affected. Set to empty string to disable.",
			constants.DefaultRuntimeClassName))
	fs.BoolVar(&options.ValidatePodResizeQuota,
		"validate-pod-resize-quota", true,
		"Enable queue limit/quota checks on pod resize requests. "+
			"Best-effort: if lookups fail, resize is admitted. "+
			"Ignored if false, disables --block-upsize-on-bounded-queues.")
	fs.BoolVar(&options.BlockUpsizeOnBoundedQueues,
		"block-upsize-on-bounded-queues", false,
		"Block pod upsize if queue or ancestor has a CPU/memory limit. "+
			"No effect if --validate-pod-resize-quota is false.")

	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	return options
}
