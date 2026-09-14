package main

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

import (
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kai-scheduler/KAI-scheduler/cmd/admission/app"

	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/webhook/v1alpha2/deviceaccess"
	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/webhook/v1alpha2/gpusharing"
	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/webhook/v1alpha2/hamicore"
	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/webhook/v1alpha2/nvfractions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/webhook/v1alpha2/runtimeenforcement"
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

	// NvFractions mode is mutually exclusive with the standard gpusharing
	// admission plugin: it owns validation and mutation for fraction pods and
	// does not use the shared GPU configmap.
	if app.Options.NvFractionsEnabled {
		admissionPlugins.RegisterPlugin(nvfractions.New(app.Options.BinderServiceAccountUsername))
	} else {
		admissionGpuSharingPlugin := gpusharing.New(
			app.Client, app.Options.GPUSharingEnabled, app.Options.NRIPluginEnabled)
		admissionPlugins.RegisterPlugin(admissionGpuSharingPlugin)
	}

	if app.Options.HamiCoreEnabled {
		admissionPlugins.RegisterPlugin(hamicore.New())
	}

	if app.Options.BlockNvidiaVisibleDevices {
		admissionPlugins.RegisterPlugin(deviceaccess.New())
	}

	if rc := app.Options.ResolvedGPUFractionRuntimeClassName(); rc != "" {
		admissionRuntimeEnforcementPlugin := runtimeenforcement.New(rc)
		admissionPlugins.RegisterPlugin(admissionRuntimeEnforcementPlugin)
	}

	app.RegisterPlugins(admissionPlugins)
	return nil
}
