// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package configurations

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
)

func PatchKAIConfig(ctx context.Context, testCtx *testcontext.TestContext, update func(*kaiv1.Config)) error {
	originalKAIConfig := &kaiv1.Config{}
	err := testCtx.ControllerClient.Get(ctx, client.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName}, originalKAIConfig)
	if err != nil {
		return err
	}
	kaiConfig := originalKAIConfig.DeepCopy()
	update(kaiConfig)
	return testCtx.ControllerClient.Patch(
		ctx, kaiConfig, client.MergeFrom(originalKAIConfig), &client.PatchOptions{},
	)
}

func PatchSchedulingShard(ctx context.Context, testCtx *testcontext.TestContext, shardName string, update func(*kaiv1.SchedulingShard)) error {
	originalShard := &kaiv1.SchedulingShard{}
	err := testCtx.ControllerClient.Get(ctx, client.ObjectKey{Name: shardName}, originalShard)
	if err != nil {
		return err
	}
	shardCopy := originalShard.DeepCopy()
	update(shardCopy)
	return testCtx.ControllerClient.Patch(
		ctx, shardCopy, client.MergeFrom(originalShard), &client.PatchOptions{},
	)
}

func SetShardArg(ctx context.Context, testCtx *testcontext.TestContext, shardName string, argName string, value *string) error {
	return PatchSchedulingShard(
		ctx, testCtx, shardName,
		func(shard *kaiv1.SchedulingShard) {
			if value == nil {
				delete(shard.Spec.Args, argName)
			} else {
				if shard.Spec.Args == nil {
					shard.Spec.Args = map[string]string{}
				}
				shard.Spec.Args[argName] = *value
			}
		},
	)
}
