// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"

	"github.com/NVIDIA/KAI-scheduler/cmd/scheduler/app"
	"github.com/NVIDIA/KAI-scheduler/cmd/scheduler/app/options"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

var loggerInitiated = false

func RunScheduler(cfg *rest.Config, stopCh chan struct{}) error {
	if !loggerInitiated {
		err := log.InitLoggers(0)
		if err != nil {
			return err
		}
		loggerInitiated = true
	}

	opt := options.NewServerOption()

	args := []string{
		"--schedule-period=10ms",
	}
	fs := pflag.NewFlagSet("flags", pflag.ExitOnError)
	opt.AddFlags(fs)
	err := fs.Parse(args)
	if err != nil {
		return err
	}

	params := app.BuildSchedulerParams(opt)

	s, err := scheduler.NewScheduler(cfg, "", params, nil)
	if err != nil {
		return err
	}

	go s.Run(stopCh)

	return nil
}
