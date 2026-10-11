// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package memorylimit

import (
	"context"
	"log"
	"os"
)

// RunMain configures a process memory budget before application initialization.
func RunMain(run func()) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := Start(ctx, Config{
		DefaultRatio: 0.85,
		LogInfo:      log.Printf,
		LogWarning:   log.Printf,
	}); err != nil {
		log.Printf("failed to configure Go memory budget: %v", err)
		os.Exit(1)
	}
	run()
}
