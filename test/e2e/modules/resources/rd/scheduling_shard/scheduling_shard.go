/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package scheduling_shard

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

func CreateShardForLabeledNodes(
	ctx context.Context,
	controllerClient client.Client,
	shardName string,
	nodeLabels client.MatchingLabels,
	spec kaiv1.SchedulingShardSpec,
) error {
	nodeList := &v1.NodeList{}
	if err := controllerClient.List(ctx, nodeList, nodeLabels); err != nil {
		return err
	}

	for _, node := range nodeList.Items {
		nodeCopy := node.DeepCopy()
		nodeCopy.Labels[constants.DefaultNodePoolLabelKey] = spec.PartitionLabelValue
		if err := controllerClient.Update(ctx, nodeCopy); err != nil {
			return err
		}
	}

	shard := &kaiv1.SchedulingShard{
		ObjectMeta: metav1.ObjectMeta{
			Name: shardName,
		},
		Spec: spec,
	}
	if err := controllerClient.Create(ctx, shard); err != nil {
		return err
	}

	wait.ForSchedulingShardStatusOK(ctx, controllerClient, shardName)

	return nil
}

func DeleteShardAndRemoveLabels(
	ctx context.Context,
	controllerClient client.Client,
	shardName string,
) error {
	shard := &kaiv1.SchedulingShard{}
	if err := controllerClient.Get(ctx, client.ObjectKey{Name: shardName}, shard); err != nil {
		return client.IgnoreNotFound(err)
	}

	nodeList := &v1.NodeList{}
	if err := controllerClient.List(ctx, nodeList, client.MatchingLabels{
		constants.DefaultNodePoolLabelKey: shard.Spec.PartitionLabelValue,
	}); err != nil {
		return err
	}

	for _, node := range nodeList.Items {
		nodeCopy := node.DeepCopy()
		delete(nodeCopy.Labels, constants.DefaultNodePoolLabelKey)
		if err := controllerClient.Update(ctx, nodeCopy); err != nil {
			return err
		}
	}

	return client.IgnoreNotFound(controllerClient.Delete(ctx, shard))
}
