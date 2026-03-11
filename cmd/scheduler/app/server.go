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

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/NVIDIA/KAI-scheduler/cmd/scheduler/app/options"
	"github.com/NVIDIA/KAI-scheduler/cmd/scheduler/profiling"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/metrics"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/version"
)

const (
	leaseDuration = 15 * time.Second
	renewDeadline = 10 * time.Second
	retryPeriod   = 5 * time.Second
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func flushLogs() {
	if err := log.InfraLogger.Sync(); err != nil &&
		!errors.Is(err, syscall.ENOTTY) && // https://github.com/uber-go/zap/issues/991#issuecomment-962098428
		!errors.Is(err, syscall.EINVAL) { // https://github.com/uber-go/zap/issues/328
		fmt.Fprintf(os.Stderr, "failed to flush logs: %v\n", err)
	}
}

func BuildSchedulerParams(opt *options.ServerOption) *conf.SchedulerParams {
	schedulingPartitionParams := &conf.SchedulingNodePoolParams{
		NodePoolLabelKey:   opt.NodePoolLabelKey,
		NodePoolLabelValue: opt.NodePoolLabelValue,
	}

	return &conf.SchedulerParams{
		SchedulerName:                     opt.SchedulerName,
		RestrictSchedulingNodes:           opt.RestrictSchedulingNodes,
		PartitionParams:                   schedulingPartitionParams,
		MaxNumberConsolidationPreemptees:  opt.MaxNumberConsolidationPreemptees,
		ScheduleCSIStorage:                opt.ScheduleCSIStorage,
		UseSchedulingSignatures:           opt.UseSchedulingSignatures,
		FullHierarchyFairness:             opt.FullHierarchyFairness,
		AllowConsolidatingReclaim:         opt.AllowConsolidatingReclaim,
		NumOfStatusRecordingWorkers:       opt.NumOfStatusRecordingWorkers,
		GlobalDefaultStalenessGracePeriod: opt.GlobalDefaultStalenessGracePeriod,
		SchedulePeriod:                    opt.SchedulePeriod,
		DetailedFitErrors:                 opt.DetailedFitErrors,
		UpdatePodEvictionCondition:        opt.UpdatePodEvictionCondition,
	}
}

func RunApp() error {
	so := options.NewServerOption()
	so.AddFlags(pflag.CommandLine)
	if err := so.ValidateOptions(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	go func() {
		_ = http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", so.PluginServerPort), mux)
	}()

	setupProfiling(so)
	if err := setupLogging(so); err != nil {
		fmt.Printf("Failed to initialize loggers: %v", err)
	} else {
		defer flushLogs()
	}
	setConfig(so)

	config := clientconfig.GetConfigOrDie()
	config.QPS = float32(so.QPS)
	config.Burst = so.Burst

	return Run(so, config, mux)
}

func setupProfiling(so *options.ServerOption) {
	if so.EnableProfiler {
		go profiling.RegisterProfiler(so.ProfilerApiPort)
	}
	if so.PyroscopeAddress != "" {
		if err := profiling.EnablePyroscope(
			so.PyroscopeAddress, so.SchedulerName, so.PyroscopeMutexProfilerRate, so.PyroscopeBlockProfilerRate,
		); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
	}
}

func setupLogging(so *options.ServerOption) error {
	if err := log.InitLoggers(so.Verbosity); err != nil {
		return err
	}

	// The default glog flush interval is 30 seconds, which is frighteningly long.
	go wait.Until(flushLogs, *logFlushFreq, wait.NeverStop)
	return nil
}

func setConfig(so *options.ServerOption) {
	config := conf.GetConfig()
	config.ResourceReservationAppLabelValue = so.ResourceReservationAppLabel
	config.CPUWorkerNodeLabelKey = so.CPUWorkerNodeLabelKey
	config.GPUWorkerNodeLabelKey = so.GPUWorkerNodeLabelKey
	config.MIGWorkerNodeLabelKey = so.MIGWorkerNodeLabelKey
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func Run(opt *options.ServerOption, config *restclient.Config, mux *http.ServeMux) error {
	if opt.PrintVersion {
		version.PrintVersion()
	}
	metrics.InitMetrics(opt.MetricsNamespace)

	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	// Load configuration of scheduler
	schedConfig, err := conf_util.ResolveConfigurationFromFile(opt.SchedulerConf)
	if err != nil {
		return fmt.Errorf("error resolving configuration from file: %v", err)
	}

	scheduler, err := scheduler.NewScheduler(config,
		schedConfig,
		BuildSchedulerParams(opt),
		mux,
	)
	if err != nil {
		return err
	}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		glog.Fatalf("Prometheus Http Server failed %s", http.ListenAndServe(opt.ListenAddress, nil))
	}()

	run := func(ctx context.Context) {
		scheduler.Run(ctx.Done())
		<-ctx.Done()
	}

	if !opt.EnableLeaderElection {
		run(context.TODO())
		return fmt.Errorf("finished without leader elect")
	}

	leaderElectionClient, err := clientset.NewForConfig(restclient.AddUserAgent(config, "leader-election"))
	if err != nil {
		return err
	}

	// Prepare event clients.
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: leaderElectionClient.CoreV1().Events(opt.Namspace)})
	componentName := opt.SchedulerName
	if len(opt.NodePoolLabelValue) > 0 {
		componentName = fmt.Sprintf("%s-%s", opt.SchedulerName, opt.NodePoolLabelValue)
	}
	eventRecorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: componentName})

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %v", err)
	}
	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id := hostname + "_" + string(uuid.NewUUID())

	rl, err := resourcelock.New(resourcelock.LeasesResourceLock,
		opt.Namspace,
		componentName,
		leaderElectionClient.CoreV1(),
		leaderElectionClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: eventRecorder,
		})
	if err != nil {
		return fmt.Errorf("couldn't create resource lock: %v", err)
	}

	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				glog.Fatalf("leaderelection lost")
			},
		},
	})
	return fmt.Errorf("lost lease")
}
