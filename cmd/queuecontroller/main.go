// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-logr/logr"
	"github.com/kai-scheduler/KAI-scheduler/cmd/queuecontroller/app"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/version"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

func main() {
	version.RegisterMetrics(metrics.Registry, "queue-controller")

	logOptions := bindLoggerFlags(flag.CommandLine)
	opts := app.InitOptions(flag.CommandLine)

	flag.Parse()
	initLogger(logOptions, os.Stderr)
	ctrl.Log.Info(version.StartupMessage("queue-controller"))

	clientConfig := ctrl.GetConfigOrDie()

	ctx := ctrl.SetupSignalHandler()
	if err := app.Run(opts, clientConfig, ctx); err != nil {
		fmt.Printf("Error while running the app: %v", err)
		os.Exit(1)
	}
}

func bindLoggerFlags(fs *flag.FlagSet) *zap.Options {
	logOptions := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logOptions.BindFlags(fs)
	return &logOptions
}

func initLogger(logOptions *zap.Options, dest io.Writer) {
	ctrl.SetLogger(newLogger(logOptions, dest))
}

func newLogger(logOptions *zap.Options, dest io.Writer) logr.Logger {
	return zap.New(zap.UseFlagOptions(logOptions), zap.WriteTo(dest))
}
