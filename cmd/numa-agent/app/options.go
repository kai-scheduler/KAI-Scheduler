// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"flag"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/numaagent/consts"
)

type Options struct {
	NodeName           string
	PodResourcesSocket string
	SysfsRoot          string
	PollInterval       time.Duration

	// k8s client options
	Qps   int
	Burst int
}

// NewOptions creates a new Options with the node name seeded from the NODE_NAME env var
// (populated via the downward API in the DaemonSet).
func NewOptions() *Options {
	return &Options{}
}

// AddFlags registers the agent's flags on the default FlagSet.
func (o *Options) AddFlags() {
	flag.StringVar(&o.NodeName, "node-name", "",
		"Name of the node this agent runs on. Defaults to the NODE_NAME environment variable.")
	flag.StringVar(&o.PodResourcesSocket, "podresources-socket", consts.DefaultPodResourcesSocket,
		"Path to the kubelet podresources gRPC socket.")
	flag.StringVar(&o.SysfsRoot, "sysfs-root", consts.DefaultSysfsRoot,
		"Path to the sysfs mount used to resolve CPU to NUMA node mappings.")
	flag.DurationVar(&o.PollInterval, "poll-interval", consts.DefaultPollInterval,
		"How often to reconcile observed NUMA placement onto pods.")
	flag.IntVar(&o.Qps, "qps", 50, "Queries per second to the K8s API server")
	flag.IntVar(&o.Burst, "burst", 100, "Burst to the K8s API server")
}
