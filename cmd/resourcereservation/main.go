// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kai-scheduler/KAI-scheduler/cmd/resourcereservation/app"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/memorylimit"
)

func main() {
	memorylimit.RunMain(runMain)
}

func runMain() {
	if err := app.Run(context.Background()); err != nil {
		fmt.Printf("Error while running the app: %v", err)
		os.Exit(1)
	}
}
