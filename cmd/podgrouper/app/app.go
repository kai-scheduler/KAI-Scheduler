// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kubeAiSchedulerV2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	controllers "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper"
	// +kubebuilder:scaffold:imports
)

const port = 9443

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

func Run() error {
	var opts Options
	opts.AddFlags(flag.CommandLine)

	initLogger()

	mgr, err := ctrl.NewManager(getClientConfigOrDie(opts), ctrl.Options{
		Scheme: scheme,
		Client: client.Options{
			Cache: &client.CacheOptions{
				Unstructured: true,
			},
		},
		Cache: getCacheOptions(opts),
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
		return err
	}

	if err = (&controllers.PodReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, opts.Configs()); err != nil {
		return err
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	setupLog.Info("starting manager")
	return mgr.Start(ctrl.SetupSignalHandler())
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

func getCacheOptions(opts Options) cache.Options {
	if opts.DefaultPrioritiesConfigMapName == "" || opts.DefaultPrioritiesConfigMapNamespace == "" {
		return cache.Options{}
	}

	// limit configmap cache to the namespace that contains the configmap of default priorities for pod groups
	return cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			&corev1.ConfigMap{}: {
				Namespaces: map[string]cache.Config{
					opts.DefaultPrioritiesConfigMapNamespace: {},
				},
			},
		},
	}
}
