// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kai-scheduler/KAI-scheduler/cmd/scheduler/app"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/version"
)

func main() {
	version.RegisterMetrics(prometheus.DefaultRegisterer, "scheduler")

	if err := app.RunApp(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
