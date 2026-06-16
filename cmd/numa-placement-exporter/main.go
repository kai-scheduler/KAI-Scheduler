// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/kai-scheduler/KAI-scheduler/cmd/numa-placement-exporter/app"
)

func main() {
	if err := app.Run(); err != nil {
		fmt.Printf("Error while running the NUMA placement exporter: %v\n", err)
		os.Exit(1)
	}
}
