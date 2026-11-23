// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package version

import (
	"fmt"
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

// StartupMessage reports the build metadata once when a component starts.
func StartupMessage(component string) string {
	return fmt.Sprintf("KAI build info: component=%s version=%q git_commit=%q go_version=%q",
		component, gitVersion, gitCommit, runtime.Version())
}

// RegisterMetrics publishes metadata to the registry served by the component.
func RegisterMetrics(registerer prometheus.Registerer, component string) {
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kai_build_info",
		Help: "Build information for a KAI component.",
		ConstLabels: prometheus.Labels{
			"component":  component,
			"version":    gitVersion,
			"git_commit": gitCommit,
			"go_version": runtime.Version(),
		},
	})
	buildInfo.Set(1)
	registerer.MustRegister(buildInfo)
}
