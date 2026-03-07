// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queuecontroller

import (
	"context"
	"flag"
	"fmt"

	"k8s.io/client-go/rest"

	"github.com/kai-scheduler/kai-scheduler/cmd/queuecontroller/app"
)

func RunQueueController(cfg *rest.Config, ctx context.Context) error {
	opts := app.InitOptions(flag.NewFlagSet("", flag.ContinueOnError))

	opts.EnableLeaderElection = false
	opts.MetricsAddress = "0"
	opts.EnableWebhook = false
	opts.SkipControllerNameValidation = true

	flag.Parse()

	go func() {
		err := app.Run(opts, cfg, ctx)
		if err != nil {
			panic(fmt.Errorf("failed to run queuecontroller app: %w", err))
		}
	}()

	return nil
}
