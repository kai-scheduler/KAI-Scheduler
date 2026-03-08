// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"
	"log"

	"go.uber.org/zap/zapcore"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/consts"
	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/controller"
	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/scale_adjuster"
	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/scaler"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch;update

func Run() error {
	options := NewOptions()
	options.AddFlags()

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	log.Println("Node scale adjuster started")

	clientConfig := ctrl.GetConfigOrDie()
	clientConfig.QPS = float32(options.Qps)
	clientConfig.Burst = options.Burst

	mgr, err := ctrl.NewManager(clientConfig, ctrl.Options{
		Scheme:           scheme,
		LeaderElection:   options.EnableLeaderElection,
		LeaderElectionID: "7n7h49uc.kai.scheduler",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	nodeScaler := scaler.NewScaler(mgr.GetClient(), options.ScalingPodImage, options.ScalingPodNamespace,
		options.ScalingPodAppLabel, options.ScalingPodServiceAccount)

	scaleAdjuster := scale_adjuster.NewScaleAdjuster(
		mgr.GetClient(),
		nodeScaler,
		options.ScalingPodNamespace,
		consts.DefaultCoolDownSeconds,
		options.GPUMemoryToFractionRatio,
		options.SchedulerName)

	podReconciler := &controller.PodReconciler{
		ScaleAdjuster:      scaleAdjuster,
		SchedulerName:      options.SchedulerName,
		NodeScaleNamespace: options.ScalingPodImage,
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
	}

	if err = podReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		return err
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
