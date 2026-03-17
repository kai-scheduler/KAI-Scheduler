// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonresources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers"

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
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

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

// +kubebuilder:webhook:path=/validate--v1-podgroup,mutating=false,failurePolicy=fail,sideEffects=None,resources=podgroups.scheduling.run.ai,verbs=create;update,groups=core,versions=v2alpha2,name=podgroupcontroller.run.ai,admissionReviewVersions=v1

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v2alpha2.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func Run(options *Options, config *rest.Config, ctx context.Context) error {
	config.QPS = float32(options.Qps)
	config.Burst = options.Burst

	draClient := commonresources.NewDRAClient(config)
	draAPIVersion := commonresources.DetectDRAAPIVersion(draClient)
	setupLog.Info("Detected DRA API version", "version", draAPIVersion)

	schedulerSelector := fields.Set{schedulerNameField: options.SchedulerName}.AsSelector()
	cacheOptions := cache.Options{}
	cacheOptions.ByObject = map[client.Object]cache.ByObject{
		&v1.Pod{}:                     {Field: schedulerSelector},
		&v1.Node{}:                    {},
		&schedulingv1.PriorityClass{}: {},
		&v2alpha2.PodGroup{}:          {},
	}
	if cacheObj := commonresources.DRACacheObject(draAPIVersion); cacheObj != nil {
		cacheOptions.ByObject[cacheObj] = cache.ByObject{}
	}

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		Cache:                  cacheOptions,
		HealthProbeBindAddress: options.ProbeAddr,
		LeaderElection:         options.EnableLeaderElection,
		LeaderElectionID:       "o1x22tjj.kai.scheduler",
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

	if options.EnablePodGroupWebhook {
		if err = (&v2alpha2.PodGroup{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook for podgroup", "webhook", "podgroup")
			return nil
		}
	}

	configs := controllers.Configs{
		MaxConcurrentReconciles: options.MaxConcurrentReconciles,
	}
	if err = (&controllers.PodGroupReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		DRAAPIVersion: draAPIVersion,
	}).SetupWithManager(mgr, configs, options.SkipControllerNameValidation); err != nil {
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
	if err = mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
