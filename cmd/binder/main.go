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
	plugins.InitDefaultPlugins()
	defaultConfig := plugins.DefaultConfig(plugins.DefaultBindTimeoutSeconds, plugins.DefaultCDIEnabled)
	config := plugins.ResolveConfig(defaultConfig, nil)
	if app.Options.Plugins.Value != nil {
		config = plugins.ResolveConfig(defaultConfig, *app.Options.Plugins.Value)
	}

	binderPlugins, err := plugins.BuildConfiguredPlugins(plugins.PluginBuildContext{
		KubeClient:      app.Client,
		K8sInterface:    app.K8sInterface,
		InformerFactory: app.InformerFactory,
	}, config)
	if err != nil {
		return err
	}
	app.RegisterPlugins(binderPlugins)
	return nil
}
