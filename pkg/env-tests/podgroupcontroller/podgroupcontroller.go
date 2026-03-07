// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroupcontroller

import (
	"context"
	"flag"
	"fmt"

	"k8s.io/client-go/rest"

	"github.com/kai-scheduler/kai-scheduler/cmd/podgroupcontroller/app"
)

func RunPodGroupController(cfg *rest.Config, ctx context.Context) error {
	options := app.InitOptions(flag.NewFlagSet("", flag.ExitOnError))
	options.MetricsAddr = "0"
	options.ProbeAddr = "0"
	options.EnablePodGroupWebhook = false
	options.EnableLeaderElection = false
	options.SkipControllerNameValidation = true

	go func() {
		if err := app.Run(options, cfg, ctx); err != nil {
			panic(fmt.Errorf("failed to run podgroupcontroller app: %w", err))
		}
	}()

	return nil
}
