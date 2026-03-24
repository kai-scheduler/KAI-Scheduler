// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"flag"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type Options struct {
	MetricsAddr          string
	EnableLeaderElection bool
	ProbeAddr            string
	Qps                  int
	Burst                int
	Namespace            string
	ImagePullSecretName  string
	ZapOptions           zap.Options
}

func SetOptions() (*Options, error) {
	return parse(flag.CommandLine, os.Args[1:])
}

func parse(flagSet *flag.FlagSet, options []string) (*Options, error) {
	opts := &Options{}
	if err := opts.parseCommandLineArgs(flagSet, options); err != nil {
		return nil, err
	}
	return opts, nil
}

func (opts *Options) parseCommandLineArgs(flagSet *flag.FlagSet, options []string) error {
	flagSet.StringVar(&opts.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flagSet.StringVar(&opts.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flagSet.BoolVar(&opts.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flagSet.IntVar(&opts.Qps, "qps", 50, "Queries per second to the K8s API server")
	flagSet.IntVar(&opts.Burst, "burst", 300, "Burst to the K8s API server")

	flagSet.StringVar(&opts.Namespace, "namespace", "runai-engine", "The namespace to create the resources in")
	flagSet.StringVar(&opts.ImagePullSecretName, "image-pull-secret", "", "The name of the image pull secret to use")
	opts.ZapOptions = zap.Options{
		Development: true,
	}
	opts.ZapOptions.BindFlags(flagSet)
	return flagSet.Parse(options)
}
