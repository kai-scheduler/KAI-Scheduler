// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/KAI-scheduler/pkg/npe"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/cputopology"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/podresources"
)

var setupLog = ctrl.Log.WithName("setup")

func Run() error {
	options := NewOptions()
	options.AddFlags()

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if options.NodeName == "" {
		options.NodeName = os.Getenv("NODE_NAME")
	}
	if options.NodeName == "" {
		return fmt.Errorf("node name is required: set --node-name or the NODE_NAME environment variable")
	}

	cpuToNUMA, err := cputopology.Load(options.SysfsRoot)
	if err != nil {
		setupLog.Error(err, "unable to load CPU topology")
		return err
	}
	setupLog.Info("Loaded CPU topology", "cpus", len(cpuToNUMA))

	resourcesClient, err := podresources.New(options.PodResourcesSocket)
	if err != nil {
		setupLog.Error(err, "unable to connect to kubelet podresources socket")
		return err
	}
	defer func() { _ = resourcesClient.Close() }()

	clientConfig := ctrl.GetConfigOrDie()
	clientConfig.QPS = float32(options.Qps)
	clientConfig.Burst = options.Burst

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes client")
		return err
	}

	exporter := npe.New(options.NodeName, options.PollInterval, options.DriftResyncInterval,
		resourcesClient, cpuToNUMA, clientset)

	if err := exporter.Run(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "exporter terminated with error")
		return err
	}
	return nil
}
