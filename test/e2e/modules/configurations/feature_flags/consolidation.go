// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package feature_flags

import (
	"context"
	"strconv"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	testContext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
)

const defaultShardName = "default"

func SetMaxConsolidationPreemptees(
	ctx context.Context, testCtx *testContext.TestContext, maxConsolidationPreemptees int,
) error {
	return patchShard(
		ctx, testCtx, defaultShardName,
		func(shard *kaiv1.SchedulingShard) {
			if shard.Spec.Args == nil {
				shard.Spec.Args = map[string]string{}
			}

			shard.Spec.Args["max-number-consolidation-preemptees"] = strconv.Itoa(maxConsolidationPreemptees)
			shard.Status = kaiv1.SchedulingShardStatus{}
		},
	)
}
