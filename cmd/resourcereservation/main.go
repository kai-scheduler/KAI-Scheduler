// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/kai-scheduler/KAI-scheduler/cmd/resourcereservation/app"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/version"
)

func main() {
	version.RegisterMetrics(metrics.Registry, "resource-reservation")

	if err := app.Run(context.Background()); err != nil {
		fmt.Printf("Error while running the app: %v", err)
		os.Exit(1)
	}
}
