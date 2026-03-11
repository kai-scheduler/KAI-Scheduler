// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/controllers"
	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/metrics"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// +kubebuilder:webhook:path=/validate--v1-queue,mutating=false,failurePolicy=fail,sideEffects=None,resources=queues.scheduling.run.ai,verbs=create;update,groups=core,versions=v2,name=queuecontroller.run.ai,admissionReviewVersions=v1

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v2.AddToScheme(scheme))
	utilruntime.Must(v2alpha2.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func Run(opts *Options, clientConfig *rest.Config, ctx context.Context) error {
	metrics.InitMetrics(opts.MetricsNamespace, opts.QueueLabelToMetricLabel.Get(), opts.QueueLabelToDefaultMetricValue.Get())
	setupLog.Info(fmt.Sprintf("Queue metrics initialized and registered with namespace: %s", opts.MetricsNamespace))

	var err error
	ctrlOptions := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: opts.MetricsAddress,
		},
		LeaderElection:   opts.EnableLeaderElection,
		LeaderElectionID: "ov3xj497.kai.scheduler",
	}

	clientConfig.QPS = float32(opts.Qps)
	clientConfig.Burst = opts.Burst

	mgr, err := ctrl.NewManager(clientConfig, ctrlOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return nil
	}

	if opts.EnableWebhook {
		if err = (&v2.Queue{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook for queue v2", "webhook", "Queue")
			return nil
		}
	}

	if err = (&controllers.QueueReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, opts.SkipControllerNameValidation); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Queue")
		return nil
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return nil
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
