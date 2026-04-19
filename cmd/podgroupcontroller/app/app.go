// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers"

	"go.uber.org/zap/zapcore"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	// +kubebuilder:scaffold:imports
)

const (
	schedulerNameField = "spec.schedulerName"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v2alpha2.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func Run() error {
	options := InitOptions()
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
		Level:       zapcore.Level(-1 * options.LogLevel),
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	clientConfig := ctrl.GetConfigOrDie()
	clientConfig.QPS = float32(options.Qps)
	clientConfig.Burst = options.Burst

	schedulerSelector := fields.Set{schedulerNameField: options.SchedulerName}.AsSelector()
	cacheOptions := cache.Options{}
	cacheOptions.ByObject = map[client.Object]cache.ByObject{
		&v1.Pod{}:                     {Field: schedulerSelector},
		&v1.Node{}:                    {}, // TODO: filter by strict/non-strict runai nodes
		&schedulingv1.PriorityClass{}: {},
		&v2alpha2.PodGroup{}:          {},
	}

	mgr, err := ctrl.NewManager(clientConfig, ctrl.Options{
		Scheme:                 scheme,
		Cache:                  cacheOptions,
		HealthProbeBindAddress: options.ProbeAddr,
		LeaderElection:         options.EnableLeaderElection,
		LeaderElectionID:       "3f770c00.run.ai",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	configs := controllers.Configs{
		MaxConcurrentReconciles: options.MaxConcurrentReconciles,
	}
	if err = (&controllers.PodGroupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, configs); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		return err
	}
	// +kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
