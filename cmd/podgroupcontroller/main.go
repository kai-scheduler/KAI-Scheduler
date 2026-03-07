// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/kai-scheduler/cmd/podgroupcontroller/app"
)

func main() {
	options := app.InitOptions(nil)
	config := ctrl.GetConfigOrDie()

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
		Level:       zapcore.Level(-1 * options.LogLevel),
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctx := ctrl.SetupSignalHandler()
	if err := app.Run(options, config, ctx); err != nil {
		fmt.Printf("Error while running the app: %v", err)
		os.Exit(1)
	}
}
