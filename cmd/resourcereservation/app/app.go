// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"flag"
	"fmt"

	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/kai-scheduler/pkg/resourcereservation/discovery"
	"github.com/kai-scheduler/kai-scheduler/pkg/resourcereservation/patcher"
	"github.com/kai-scheduler/kai-scheduler/pkg/resourcereservation/poddetails"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func Run(ctx context.Context) error {
	initLogger()

	logger := log.FromContext(ctx)

	kubeConfig := config.GetConfigOrDie()
	kubeClient, err := client.New(kubeConfig, client.Options{})
	if err != nil {
		return fmt.Errorf("could not create client, err: %v", err)
	}

	namespace, name, err := poddetails.CurrentPodDetails()
	if err != nil {
		return fmt.Errorf("could not get pod details, err: %v", err)
	}

	patch := patcher.NewPodPatcher(kubeClient, namespace, name)
	err, patched := patch.AlreadyPatched(ctx)
	if err != nil {
		return fmt.Errorf("could not check if pod was already patched, err: %v", err)
	}

	if !patched {
		logger.Info("Looking for GPU device id for pod", "namespace", namespace, "name", name)
		var gpuDevice string
		gpuDevice, err = discovery.GetGPUDevice(ctx)
		if err != nil {
			return err
		}
		logger.Info("Found GPU device", "deviceId", gpuDevice)

		err = patch.PatchDeviceInfo(ctx, gpuDevice)
		if err != nil {
			return fmt.Errorf("could not patch pod. error: %v", err)
		}

		logger.Info("Pod was updated with GPU device UUID")
	}

	mgr, err := ctrl.NewManager(kubeConfig, ctrl.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		return err
	}
	return nil
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
