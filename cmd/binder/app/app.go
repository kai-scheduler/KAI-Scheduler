// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	schedulingv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	draversionawareclient "github.com/NVIDIA/KAI-scheduler/pkg/common/resources/dra_version_aware_client"

	"github.com/NVIDIA/KAI-scheduler/pkg/binder/binding"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/controllers"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(schedulingv1alpha2.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type App struct {
	K8sInterface     kubernetes.Interface
	Client           client.WithWatch
	InformerFactory  informers.SharedInformerFactory
	Options          *Options
	manager          manager.Manager
	rrs              resourcereservation.Interface
	reconcilerParams *controllers.ReconcilerParams
	plugins          *plugins.BinderPlugins
}

func New(options *Options, config *rest.Config) (*App, error) {
	config.QPS = float32(options.QPS)
	config.Burst = options.Burst

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: options.MetricsAddr,
		},
		HealthProbeBindAddress: options.ProbeAddr,
		LeaderElection:         options.EnableLeaderElection,
		LeaderElectionID:       "2ad35f9c.kai.scheduler",
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

	if err := createIndexesForResourceReservation(mgr); err != nil {
		return nil, err
	}

	clientWithWatch, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader: mgr.GetCache(),
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to create client with watch")
		return nil, err
	}

	kubeClient := draversionawareclient.NewDRAAwareClient(kubernetes.NewForConfigOrDie(config))
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)

	// Deserialize pod resources if provided
	var podResources *corev1.ResourceRequirements
	if options.ResourceReservationPodResourcesJSON != "" {
		podResources = &corev1.ResourceRequirements{}
		if err := json.Unmarshal([]byte(options.ResourceReservationPodResourcesJSON), podResources); err != nil {
			setupLog.Error(err, "failed to unmarshal resource reservation pod resources")
			return nil, fmt.Errorf("failed to unmarshal resource reservation pod resources: %w", err)
		}
	}

	var podSecurityContext *corev1.PodSecurityContext
	if options.ResourceReservationPodSecurityContextJSON != "" {
		podSecurityContext = &corev1.PodSecurityContext{}
		if err := json.Unmarshal([]byte(options.ResourceReservationPodSecurityContextJSON), podSecurityContext); err != nil {
			setupLog.Error(err, "failed to unmarshal resource reservation pod security context")
			return nil, fmt.Errorf("failed to unmarshal resource reservation pod security context: %w", err)
		}
	}

	var containerSecurityContext *corev1.SecurityContext
	if options.ResourceReservationContainerSecurityJSON != "" {
		containerSecurityContext = &corev1.SecurityContext{}
		if err := json.Unmarshal([]byte(options.ResourceReservationContainerSecurityJSON), containerSecurityContext); err != nil {
			setupLog.Error(err, "failed to unmarshal resource reservation container security context")
			return nil, fmt.Errorf("failed to unmarshal resource reservation container security context: %w", err)
		}
	}

	rrs := resourcereservation.NewService(options.FakeGPUNodes, clientWithWatch, options.ResourceReservationPodImage,
		time.Duration(options.ResourceReservationAllocationTimeout)*time.Second,
		options.ResourceReservationNamespace, options.ResourceReservationServiceAccount,
		options.ResourceReservationAppLabel, options.ScalingPodNamespace, options.RuntimeClassName,
		podResources, podSecurityContext, containerSecurityContext)

	reconcilerParams := &controllers.ReconcilerParams{
		MaxConcurrentReconciles:     options.MaxConcurrentReconciles,
		RateLimiterBaseDelaySeconds: options.RateLimiterBaseDelaySeconds,
		RateLimiterMaxDelaySeconds:  options.RateLimiterMaxDelaySeconds,
	}

	app := &App{
		K8sInterface:     kubeClient,
		Client:           clientWithWatch,
		InformerFactory:  informerFactory,
		Options:          options,
		manager:          mgr,
		rrs:              rrs,
		reconcilerParams: reconcilerParams,
	}
	return app, nil
}

func (app *App) RegisterPlugins(plugins *plugins.BinderPlugins) {
	app.plugins = plugins
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func (app *App) Run(ctx context.Context) error {
	var err error
	go func() {
		app.manager.GetCache().WaitForCacheSync(context.Background())
		setupLog.Info("syncing resource reservation")
		err := app.rrs.Sync(context.Background())
		if err != nil {
			setupLog.Error(err, "unable to sync resource reservation")
			panic(err)
		}
	}()

	if err = (&controllers.PodReconciler{
		Client:              app.manager.GetClient(),
		Scheme:              app.manager.GetScheme(),
		ResourceReservation: app.rrs,
		SchedulerName:       app.Options.SchedulerName,
	}).SetupWithManager(app.manager, app.reconcilerParams); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		return err
	}

	binder := binding.NewBinder(app.Client, app.rrs, app.plugins)

	app.InformerFactory.Start(ctx.Done())
	app.InformerFactory.WaitForCacheSync(ctx.Done())

	reconciler := controllers.NewBindRequestReconciler(
		app.manager.GetClient(), app.manager.GetScheme(), app.manager.GetEventRecorderFor("binder"), app.reconcilerParams,
		binder, app.rrs)
	if err = reconciler.SetupWithManager(app.manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BindRequest")
		return err
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err = app.manager.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}
	return nil
}

func createIndexesForResourceReservation(mgr manager.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(), &corev1.Pod{}, "spec.nodeName",
		func(obj client.Object) []string {
			nodeName := obj.(*corev1.Pod).Spec.NodeName
			if nodeName == "" {
				return nil
			}
			return []string{nodeName}
		},
	); err != nil {
		setupLog.Error(err, "failed to create index for spec.nodeName")
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(), &corev1.Pod{}, fmt.Sprintf("metadata.labels.%s", constants.GPUGroup),
		func(obj client.Object) []string {
			labels := obj.(*corev1.Pod).Labels
			if labels == nil {
				return nil
			}
			gpuGroup, found := labels[constants.GPUGroup]
			if !found {
				return nil
			}
			return []string{gpuGroup}
		},
	); err != nil {
		setupLog.Error(err, "failed to create index for spec.nodeName")
		return err
	}

	return nil
}
