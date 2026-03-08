// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kubeAiSchedulerV2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	controllers "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper"
	pluginshub "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/hub"
	// +kubebuilder:scaffold:imports
)

const (
	port               = 9443
	schedulerNameField = "spec.schedulerName"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v2.AddToScheme(scheme))
	utilruntime.Must(kubeAiSchedulerV2alpha2.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

type App struct {
	Mgr               manager.Manager
	DefaultPluginsHub *pluginshub.DefaultPluginsHub

	configs    controllers.Configs
	pluginsHub pluginshub.PluginsHub
}

func Run() error {
	app, err := New()
	if err != nil {
		return err
	}

	return app.Run()
}

func New() (*App, error) {
	return NewWithScheme(scheme)
}

func NewWithScheme(mgrScheme *runtime.Scheme) (*App, error) {
	var opts Options
	opts.AddFlags(flag.CommandLine)

	initLogger()

	if mgrScheme == nil {
		mgrScheme = scheme
	}

	configs := opts.Configs()
	mgr, err := ctrl.NewManager(getClientConfigOrDie(opts), ctrl.Options{
		Scheme: mgrScheme,
		Client: client.Options{
			Cache: &client.CacheOptions{
				Unstructured: true,
			},
		},
		Cache: getCacheOptions(configs),
		Metrics: metricsserver.Options{
			BindAddress: opts.MetricsAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: port,
		}),
		HealthProbeBindAddress: opts.ProbeAddr,
		LeaderElection:         opts.EnableLeaderElection,
		LeaderElectionID:       "54dff599.kai.scheduler",
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
		return nil, err
	}

	defaultPluginsHub := pluginshub.NewDefaultPluginsHub(mgr.GetClient(), configs.SearchForLegacyPodGroups,
		configs.KnativeGangSchedule, configs.SchedulingQueueLabelKey, configs.NodePoolLabelKey,
		configs.DefaultConfigPerTypeConfigMapName, configs.DefaultConfigPerTypeConfigMapNamespace)

	app := &App{
		Mgr:               mgr,
		DefaultPluginsHub: defaultPluginsHub,
		configs:           configs,
		pluginsHub:        nil,
	}
	return app, nil
}

func (app *App) RegisterPlugins(pluginsHub pluginshub.PluginsHub) {
	app.pluginsHub = pluginsHub
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func (app *App) Run() error {
	pluginsHub := app.pluginsHub
	if pluginsHub == nil {
		pluginsHub = app.DefaultPluginsHub
	}

	if err := (&controllers.PodReconciler{
		Client: app.Mgr.GetClient(),
		Scheme: app.Mgr.GetScheme(),
	}).SetupWithManager(app.Mgr, app.configs, pluginsHub); err != nil {
		return err
	}
	// +kubebuilder:scaffold:builder

	if err := app.Mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := app.Mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	setupLog.Info("starting manager")
	return app.Mgr.Start(ctrl.SetupSignalHandler())
}

func initLogger() {
	logOptions := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOptions)))
}

func getClientConfigOrDie(opts Options) *rest.Config {
	clientConfig := ctrl.GetConfigOrDie()
	clientConfig.QPS = float32(opts.QPS)
	clientConfig.Burst = opts.Burst

	return clientConfig
}

func getCacheOptions(configs controllers.Configs) cache.Options {
	podByObject := cache.ByObject{
		Field: fields.Set{schedulerNameField: configs.SchedulerName}.AsSelector(),
	}
	if len(configs.PodLabelSelector) > 0 {
		podByObject.Label = labels.Set(configs.PodLabelSelector).AsSelector()
	}

	cacheOptions := cache.Options{}
	cacheOptions.ByObject = map[client.Object]cache.ByObject{
		&corev1.Pod{}: podByObject,
	}

	// limit configmap cache to the namespace that contains the configmap of default configs for pod groups
	if configs.DefaultConfigPerTypeConfigMapName != "" && configs.DefaultConfigPerTypeConfigMapNamespace != "" {
		cacheOptions.ByObject[&corev1.ConfigMap{}] = cache.ByObject{
			Namespaces: map[string]cache.Config{
				configs.DefaultConfigPerTypeConfigMapNamespace: {},
			},
		}
	}

	return cacheOptions
}
