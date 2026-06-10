// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/kai-scheduler/KAI-scheduler/cmd/numa-agent/app"
)

func main() {
	if err := app.Run(); err != nil {
		fmt.Printf("Error while running the NUMA placement agent: %v\n", err)
		os.Exit(1)
	}
}
