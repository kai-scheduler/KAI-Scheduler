// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kai-scheduler/kai-scheduler/cmd/queuecontroller/app"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	initLogger()

	clientConfig := ctrl.GetConfigOrDie()
	opts := app.InitOptions(flag.CommandLine)

	flag.Parse()

	ctx := ctrl.SetupSignalHandler()
	if err := app.Run(opts, clientConfig, ctx); err != nil {
		fmt.Printf("Error while running the app: %v", err)
		os.Exit(1)
	}
}

func initLogger() {
	logOptions := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logOptions.BindFlags(flag.CommandLine)
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOptions)))
}
