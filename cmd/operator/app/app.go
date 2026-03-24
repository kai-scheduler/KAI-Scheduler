// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"os"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	"github.com/kai-scheduler/KAI-scheduler/cmd/operator/config"
	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/controller"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(kaiv1.AddToScheme(scheme))
	utilruntime.Must(kaiv1alpha1.AddToScheme(scheme))
	utilruntime.Must(nvidiav1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(vpav1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type App struct {
	manager          manager.Manager
	configReconciler *controller.ConfigReconciler
	shardReconciler  *controller.SchedulingShardReconciler
}

func New() (*App, error) {
	opts, err := config.SetOptions()
	if err != nil {
		setupLog.Error(err, "unable to parse arguments")
		return nil, err
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts.ZapOptions)))

	clientConfig := ctrl.GetConfigOrDie()
	clientConfig.QPS = float32(opts.Qps)
	clientConfig.Burst = opts.Burst

	mgr, err := ctrl.NewManager(clientConfig, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: opts.MetricsAddr,
		},
		HealthProbeBindAddress: opts.ProbeAddr,
		LeaderElection:         opts.EnableLeaderElection,
		LeaderElectionID:       "41fce092.run.ai",
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
		return nil, err
	}

	configReconciler := &controller.ConfigReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	shardReconciler := controller.NewSchedulingShardReconciler(
		mgr.GetClient(), mgr.GetScheme(),
	)

	return &App{
		manager:          mgr,
		configReconciler: configReconciler,
		shardReconciler:  shardReconciler,
	}, nil
}

func (app *App) InitOperands(configOperands []operands.Operand, shardOperandsForShard func(*kaiv1.SchedulingShard) []operands.Operand) {
	app.configReconciler.SetOperands(configOperands)
	app.shardReconciler.SetOperands(shardOperandsForShard)
}

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func (app *App) Run() error {

	var err error
	if err = app.configReconciler.SetupWithManager(app.manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Config")
		os.Exit(1)
	}
	if err = app.shardReconciler.SetupWithManager(app.manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SchedulingShard")
		os.Exit(1)
	}

	if err := app.manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := app.manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := app.manager.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
