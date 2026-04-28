// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binder

import (
	"context"
	"fmt"

	"github.com/spf13/pflag"

	"k8s.io/client-go/rest"

	"github.com/kai-scheduler/KAI-scheduler/cmd/binder/app"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
)

func RunBinder(cfg *rest.Config, ctx context.Context) error {
	options := app.InitOptions(pflag.NewFlagSet("binder-test", pflag.ContinueOnError))

	options.MetricsAddr = "0"
	options.ProbeAddr = "0"
	options.EnableLeaderElection = false

	app, err := app.New(options, cfg)
	if err != nil {
		return err
	}

	err = registerPlugins(app)
	if err != nil {
		return err
	}
	go func() {
		err := app.Run(ctx)
		if err != nil {
			panic(fmt.Errorf("failed to run binder app: %w", err))
		}
	}()

	return nil
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
