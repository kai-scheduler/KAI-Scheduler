// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/memorylimit"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

func startMemoryLimitManager(ctx context.Context) error {
	return memorylimit.Start(ctx, memorylimit.Config{
		DefaultRatio: 0.9,
		LogInfo:      log.InfraLogger.V(2).Infof,
		LogWarning:   log.InfraLogger.Warningf,
	})
}
