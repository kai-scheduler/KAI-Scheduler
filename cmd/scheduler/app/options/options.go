/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"time"

	"github.com/spf13/pflag"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

const (
	defaultSchedulerPeriod             = time.Second
	defaultStalenessGracePeriod        = 60 * time.Second
	defaultListenAddress               = ":8080"
	defaultProfilerApiPort             = "8182"
	defaultVerbosityLevel              = 3
	defaultMaxConsolidationPreemptees  = 16
	defaultDetailedFitError            = false
	DefaultPyroscopeMutexProfilerRate  = 5
	DefaultPyroscopeBlockProfilerRate  = 5
	defaultNumOfStatusRecordingWorkers = 5
)

// ServerOption is the main context object for the controller manager.
type ServerOption struct {
	SchedulerName                     string
	ResourceReservationAppLabel       string
	SchedulerConf                     string
	SchedulePeriod                    time.Duration
	EnableLeaderElection              bool
	PrintVersion                      bool
	MetricsNamespace                  string
	RestrictSchedulingNodes           bool
	NodePoolLabelKey                  string
	NodePoolLabelValue                string
	ListenAddress                     string
	EnableProfiler                    bool
	ProfilerApiPort                   string
	PyroscopeAddress                  string
	PyroscopeMutexProfilerRate        int
	PyroscopeBlockProfilerRate        int
	Verbosity                         int
	MaxNumberConsolidationPreemptees  int
	DetailedFitErrors                 bool
	UpdatePodEvictionCondition        bool
	ScheduleCSIStorage                bool
	UseSchedulingSignatures           bool
	FullHierarchyFairness             bool
	AllowConsolidatingReclaim         bool
	NumOfStatusRecordingWorkers       int
	GlobalDefaultStalenessGracePeriod time.Duration
	PluginServerPort                  int
	CPUWorkerNodeLabelKey             string
	GPUWorkerNodeLabelKey             string
	MIGWorkerNodeLabelKey             string
	QueueLabelKey                     string
	Namspace                          string

	JSONLog bool

	QPS   int
	Burst int
}

// NewServerOption creates a new CMServer with a default config.
func NewServerOption() *ServerOption {
	s := ServerOption{}
	return &s
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet
func (s *ServerOption) AddFlags(fs *pflag.FlagSet) {
	// kai-scheduler will ignore pods with scheduler names other than specified with the option
	fs.StringVar(&s.SchedulerName, "scheduler-name", constants.DefaultSchedulerName, "The scheduler name in pod spec that handled by this scheduler")
	fs.StringVar(&s.Namspace, "namespace", constants.DefaultKAINamespace, "Scheduler service namespace")
	fs.StringVar(&s.MetricsNamespace, "metrics-namespace", constants.DefaultMetricsNamespace, "Metrics namespace")
	fs.StringVar(&s.ResourceReservationAppLabel, "resource-reservation-app-label", constants.DefaultResourceReservationName, "App label value of resource reservation pods")
	fs.BoolVar(&s.RestrictSchedulingNodes, "restrict-node-scheduling", false, "kai-scheduler will allocate jobs only to restricted nodes")
	fs.StringVar(&s.NodePoolLabelKey, "nodepool-label-key", constants.DefaultNodePoolLabelKey, "The label key by which to filter scheduling nodepool")
	fs.StringVar(&s.QueueLabelKey, "queue-label-key", constants.DefaultQueueLabel, "The label key for the queue")
	fs.StringVar(&s.NodePoolLabelValue, "partition-label-value", "", "The label value by which to filter scheduling partition")
	fs.StringVar(&s.SchedulerConf, "scheduler-conf", "", "The absolute path of scheduler configuration file")
	fs.DurationVar(&s.SchedulePeriod, "schedule-period", defaultSchedulerPeriod, "The period between each scheduling cycle")
	fs.BoolVar(&s.EnableLeaderElection, "leader-elect", false,
		"Start a leader election client and gain leadership before "+
			"executing the main loop. Enable this when running replicated kai-scheduler for high availability")
	fs.BoolVar(&s.PrintVersion, "version", true, "Show version")
	fs.StringVar(&s.ListenAddress, "listen-address", defaultListenAddress, "The address to listen on for HTTP requests")
	fs.BoolVar(&s.EnableProfiler, "enable-profiler", false, "Enable profiler")
	fs.StringVar(&s.ProfilerApiPort, "profiler-port", defaultProfilerApiPort, "The port to listen for profiler api requests")
	fs.StringVar(&s.PyroscopeAddress, "pyroscope-address", "", "The url of pyroscope")
	fs.IntVar(&s.PyroscopeMutexProfilerRate, "pyroscope-mutex-profiler-rate", DefaultPyroscopeMutexProfilerRate, "Mutex Profiler rate")
	fs.IntVar(&s.PyroscopeBlockProfilerRate, "pyroscope-block-profiler-rate", DefaultPyroscopeBlockProfilerRate, "Block Profiler rate")
	fs.IntVar(&s.Verbosity, "v", defaultVerbosityLevel, "Verbosity level")
	fs.BoolVar(&s.JSONLog, "log-json", false, "Output logs without ANSI color codes for log aggregation platforms")
	fs.IntVar(&s.MaxNumberConsolidationPreemptees, "max-consolidation-preemptees", defaultMaxConsolidationPreemptees, "Maximum number of consolidation preemptees. Defaults to 16")
	fs.IntVar(&s.QPS, "qps", 50, "Queries per second to the K8s API server")
	fs.IntVar(&s.Burst, "burst", 300, "Burst to the K8s API server")
	fs.BoolVar(&s.DetailedFitErrors, "detailed-fit-errors", defaultDetailedFitError, "Write detailed fit errors for every node on every podgroup")
	fs.BoolVar(&s.UpdatePodEvictionCondition, "update-pod-eviction-condition", false, "Update pod eviction condition to reflect the pod's eviction status")
	fs.BoolVar(&s.ScheduleCSIStorage, "schedule-csi-storage", false, "Enables advanced scheduling (preempt, reclaim) for csi storage objects")
	fs.BoolVar(&s.UseSchedulingSignatures, "use-scheduling-signatures", true, "Use scheduling signatures to avoid duplicate scheduling attempts for identical jobs")
	fs.BoolVar(&s.FullHierarchyFairness, "full-hierarchy-fairness", true, "Fairness across project and department levels")
	fs.BoolVar(&s.AllowConsolidatingReclaim, "allow-consolidating-reclaim", true, "Do not count pipelined pods towards 'reclaimed' resources")
	fs.IntVar(&s.NumOfStatusRecordingWorkers, "num-of-status-recording-workers", defaultNumOfStatusRecordingWorkers, "specifies the max number of go routines spawned to update pod and podgroups conditions and events. Defaults to 5")
	fs.DurationVar(&s.GlobalDefaultStalenessGracePeriod, "default-staleness-grace-period", defaultStalenessGracePeriod, "Global default staleness grace period duration. Negative values means infinite. Defaults to 60s")
	fs.IntVar(&s.PluginServerPort, "plugin-server-port", 8081, "The port to bind for plugin server requests")
	fs.StringVar(&s.CPUWorkerNodeLabelKey, "cpu-worker-node-label-key", constants.DefaultCPUWorkerNodeLabelKey, "The label key for CPU worker nodes")
	fs.StringVar(&s.GPUWorkerNodeLabelKey, "gpu-worker-node-label-key", constants.DefaultGPUWorkerNodeLabelKey, "The label key for GPU worker nodes")
	fs.StringVar(&s.MIGWorkerNodeLabelKey, "mig-worker-node-label-key", constants.DefaultMIGWorkerNodeLabelKey, "The label key for MIG enabled worker nodes")

	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)
}

func (so *ServerOption) ValidateOptions() error {
	pflag.Parse()
	pflag.VisitAll(func(flag *pflag.Flag) {
		log.InfraLogger.V(1).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
	})
	return nil
}
