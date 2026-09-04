// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/memorylimit"
)

const defaultGoMemLimitRatio = 0.85

// StartMemoryLimitManager starts cgroup-derived memory management before Binder creates caches or plugins.
func StartMemoryLimitManager(ctx context.Context) error {
	return memorylimit.Start(ctx, memorylimit.Config{
		DefaultRatio: defaultGoMemLimitRatio,
		LogInfo:      func(format string, args ...interface{}) { setupLog.V(2).Info(fmt.Sprintf(format, args...)) },
		LogWarning:   func(format string, args ...interface{}) { setupLog.Info(fmt.Sprintf(format, args...)) },
	})
}
