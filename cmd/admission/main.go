package main

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

import (
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kai-scheduler/kai-scheduler/cmd/admission/app"

	"github.com/kai-scheduler/kai-scheduler/pkg/admission/plugins"
	"github.com/kai-scheduler/kai-scheduler/pkg/admission/webhook/v1alpha2/gpusharing"
	"github.com/kai-scheduler/kai-scheduler/pkg/admission/webhook/v1alpha2/runtimeenforcement"
)

var (
	setupLog = ctrl.Log.WithName("admission-setup")
)

func main() {
	app, err := app.New()
	if err != nil {
		setupLog.Error(err, "failed to create app")
		os.Exit(1)
	}

	err = registerPlugins(app)
	if err != nil {
		setupLog.Error(err, "failed to register plugins")
		os.Exit(1)
	}

	err = app.Run()
	if err != nil {
		setupLog.Error(err, "failed to run app")
		os.Exit(1)
	}
}

func registerPlugins(app *app.App) error {
	admissionPlugins := plugins.New()

	admissionGpuSharingPlugin := gpusharing.New(app.Client, app.Options.GPUSharingEnabled)
	admissionPlugins.RegisterPlugin(admissionGpuSharingPlugin)

	if app.Options.GPUPodRuntimeClassName != "" {
		admissionRuntimeEnforcementPlugin := runtimeenforcement.New(app.Options.GPUPodRuntimeClassName)
		admissionPlugins.RegisterPlugin(admissionRuntimeEnforcementPlugin)
	}

	app.RegisterPlugins(admissionPlugins)
	return nil
}
