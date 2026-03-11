// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"os"

	"github.com/spf13/pflag"

	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/KAI-scheduler/cmd/binder/app"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	k8s_plugins "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/k8s-plugins"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	options := app.InitOptions(nil)
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	app, err := app.New(options, ctrl.GetConfigOrDie())
	if err != nil {
		setupLog.Error(err, "failed to create app")
		os.Exit(1)
	}

	err = registerPlugins(app)
	if err != nil {
		setupLog.Error(err, "failed to register plugins")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()
	err = app.Run(ctx)
	if err != nil {
		setupLog.Error(err, "failed to run app")
		os.Exit(1)
	}
}

func registerPlugins(app *app.App) error {
	binderPlugins := plugins.New()
	k8sPlugins, err := k8s_plugins.New(app.K8sInterface, app.InformerFactory,
		int64(app.Options.VolumeBindingTimeoutSeconds))
	if err != nil {
		return err
	}
	binderPlugins.RegisterPlugin(k8sPlugins)

	bindingGpuSharingPlugin := gpusharing.New(app.Client, app.Options.GpuCdiEnabled)

	binderPlugins.RegisterPlugin(bindingGpuSharingPlugin)
	app.RegisterPlugins(binderPlugins)
	return nil
}
