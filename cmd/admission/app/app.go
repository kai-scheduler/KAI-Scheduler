// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"flag"

	admissionhooks "github.com/kai-scheduler/kai-scheduler/pkg/admission/webhook/v1alpha2/podhooks"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	schedulingv1alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v1alpha2"

	admissionplugins "github.com/kai-scheduler/kai-scheduler/pkg/admission/plugins"
	"github.com/kai-scheduler/kai-scheduler/pkg/binder/controllers"
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
	reconcilerParams *controllers.ReconcilerParams
	admissionPlugins *admissionplugins.KaiAdmissionPlugins
}

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,resources=pods,verbs=create,groups=core,versions=v1,name=admission.run.ai,admissionReviewVersions=v1,reinvocationPolicy=IfNeeded
// +kubebuilder:webhook:path=/validate--v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,resources=pods,verbs=create;update,groups=core,versions=v1,name=admission.run.ai,admissionReviewVersions=v1

func New() (*App, error) {
	options := InitOptions()
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	config := ctrl.GetConfigOrDie()
	config.QPS = float32(options.QPS)
	config.Burst = options.Burst

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: options.MetricsAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: options.WebhookPort,
		}),
		HealthProbeBindAddress: options.ProbeAddr,
		LeaderElection:         options.EnableLeaderElection,
		LeaderElectionID:       "ojptxr84.kai.scheduler",
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

	kubeClient := kubernetes.NewForConfigOrDie(config)
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)

	reconcilerParams := &controllers.ReconcilerParams{
		RateLimiterBaseDelaySeconds: options.RateLimiterBaseDelaySeconds,
		RateLimiterMaxDelaySeconds:  options.RateLimiterMaxDelaySeconds,
	}

	app := &App{
		K8sInterface:     kubeClient,
		Client:           clientWithWatch,
		InformerFactory:  informerFactory,
		Options:          options,
		manager:          mgr,
		reconcilerParams: reconcilerParams,
	}
	return app, nil
}

func (app *App) RegisterPlugins(admissionPlugins *admissionplugins.KaiAdmissionPlugins) {
	app.admissionPlugins = admissionPlugins
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch;update

func (app *App) Run() error {
	var err error
	go func() {
		app.manager.GetCache().WaitForCacheSync(context.Background())
	}()

	// +kubebuilder:scaffold:builder

	if err = ctrl.NewWebhookManagedBy(app.manager).For(&corev1.Pod{}).
		WithDefaulter(admissionhooks.NewPodMutator(app.manager.GetClient(), app.admissionPlugins, app.Options.SchedulerName)).
		WithValidator(admissionhooks.NewPodValidator(app.manager.GetClient(), app.admissionPlugins, app.Options.SchedulerName)).Complete(); err != nil {
		setupLog.Error(err, "unable to create pod webhooks", "webhook", "Pod")
		return err
	}

	stopCh := make(chan struct{})
	app.InformerFactory.Start(stopCh)
	app.InformerFactory.WaitForCacheSync(stopCh)

	if err = app.manager.AddHealthzCheck("healthz", app.manager.GetWebhookServer().StartedChecker()); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err = app.manager.AddReadyzCheck("readyz", app.manager.GetWebhookServer().StartedChecker()); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err = app.manager.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
