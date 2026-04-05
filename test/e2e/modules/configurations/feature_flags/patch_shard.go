// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package feature_flags

import (
	"context"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

type patchCallback func(shard *kaiv1.SchedulingShard)

func patchShard(
	ctx context.Context, testCtx *testContext.TestContext, shardName string, patcher patchCallback,
) error {
	shard := &kaiv1.SchedulingShard{}
	err := testCtx.ControllerClient.Get(ctx, types.NamespacedName{Name: shardName}, shard)
	if errors.IsNotFound(err) {
		shard = &kaiv1.SchedulingShard{
			ObjectMeta: metav1.ObjectMeta{Name: shardName},
		}
		Expect(testCtx.ControllerClient.Create(ctx, shard)).To(Succeed())
	}
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	Expect(configurations.PatchSchedulingShard(ctx, testCtx, shardName, patcher)).To(Succeed())

	wait.ForSchedulingShardStatusOK(ctx, testCtx.ControllerClient, shardName)

	// These lines are here to workaround shard status issue - RUN-13930:
	engineConfig := &kaiv1.Config{}
	Expect(testCtx.ControllerClient.Get(ctx, types.NamespacedName{Name: constants.DefaultKAIConfigSingeltonInstanceName}, engineConfig)).To(Succeed())

	schedulerAppName := "kai-scheduler-" + shardName
	err = testCtx.ControllerClient.DeleteAllOf(
		ctx, &v1.Pod{},
		client.InNamespace(engineConfig.Spec.Namespace),
		client.MatchingLabels{constant.AppLabelName: schedulerAppName},
		client.GracePeriodSeconds(0),
	)
	err = client.IgnoreNotFound(err)
	Expect(err).To(Succeed())

	wait.ForRunningSystemComponentEvent(ctx, testCtx.ControllerClient, schedulerAppName)

	return nil
}
