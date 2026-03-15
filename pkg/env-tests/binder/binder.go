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
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	k8s_plugins "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/k8s-plugins"
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
