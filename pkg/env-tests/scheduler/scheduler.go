// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"

	"github.com/NVIDIA/KAI-scheduler/cmd/scheduler/app"
	"github.com/NVIDIA/KAI-scheduler/cmd/scheduler/app/options"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins"
)

var loggerInitiated = false

func RunScheduler(cfg *rest.Config, schedulerConf *conf.SchedulerConfiguration, stopCh chan struct{}) error {
	if !loggerInitiated {
		err := log.InitLoggers(0, false)
		if err != nil {
			return err
		}
		loggerInitiated = true
	}

	opt := options.NewServerOption()

	args := []string{
		"--schedule-period=1ms",
	}
	fs := pflag.NewFlagSet("flags", pflag.ExitOnError)
	opt.AddFlags(fs)
	err := fs.Parse(args)
	if err != nil {
		return err
	}

	opt.PluginServerPort = 8084
	opt.ListenAddress = ":8085"

	params := app.BuildSchedulerParams(opt)

	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	if schedulerConf == nil {
		schedulerConf, err = conf_util.GetDefaultSchedulerConf()
		if err != nil {
			return err
		}
	}

	s, err := scheduler.NewScheduler(cfg, schedulerConf, params, nil)
	if err != nil {
		return err
	}

	go s.Run(stopCh)

	return nil
}
