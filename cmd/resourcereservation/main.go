// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kai-scheduler/kai-scheduler/cmd/resourcereservation/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		fmt.Printf("Error while running the app: %v", err)
		os.Exit(1)
	}
}
