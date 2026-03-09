// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_scale_adjuster

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/node_scale_adjuster"
	kaiConfigUtils "github.com/NVIDIA/KAI-scheduler/pkg/operator/config"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/operator/operands/common"
)

const (
	deploymentName = "node-scale-adjuster"
)

func deploymentForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) (client.Object, error) {

	config := kaiConfig.Spec.NodeScaleAdjuster
	deployment, err := common.DeploymentForKAIConfig(ctx, runtimeClient, kaiConfig, config.Service, deploymentName)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Template.Spec.Containers[0].Args = argsForKAIConfig(kaiConfig.Spec.NodeScaleAdjuster, *kaiConfig.Spec.Global.SchedulerName,
		kaiConfig.Spec.Global.JSONLog != nil && *kaiConfig.Spec.Global.JSONLog)

	return deployment, nil
}

func serviceAccountForKAIConfig(
	ctx context.Context, k8sReader client.Reader, kaiConfig *kaiv1.Config,
) (client.Object, error) {
	sa, err := common.ObjectForKAIConfig(ctx, k8sReader, &v1.ServiceAccount{}, deploymentName,
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	sa.(*v1.ServiceAccount).TypeMeta = metav1.TypeMeta{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
	}
	return sa, err
}

func scalingPodServiceAccountForKAIConfig(
	ctx context.Context, k8sReader client.Reader, kaiConfig *kaiv1.Config,
) (client.Object, error) {
	saObject, err := common.ObjectForKAIConfig(ctx, k8sReader, &v1.ServiceAccount{}, *kaiConfig.Spec.NodeScaleAdjuster.Args.NodeScaleServiceAccount,
		*kaiConfig.Spec.NodeScaleAdjuster.Args.NodeScaleNamespace)
	err = client.IgnoreNotFound(err)
	if err != nil {
		return nil, err
	}
	sa := saObject.(*v1.ServiceAccount)

	sa.TypeMeta = metav1.TypeMeta{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
	}

	sa.Name = *kaiConfig.Spec.NodeScaleAdjuster.Args.NodeScaleServiceAccount
	sa.Namespace = *kaiConfig.Spec.NodeScaleAdjuster.Args.NodeScaleNamespace
	sa.ImagePullSecrets = kaiConfigUtils.GetGlobalImagePullSecrets(kaiConfig.Spec.Global)

	return sa, err
}

func argsForKAIConfig(config *node_scale_adjuster.NodeScaleAdjuster, schedulerName string, jsonLog bool) []string {
	args := []string{
		"--scheduler-name", schedulerName,
	}

	if config.Args.NodeScaleServiceAccount != nil {
		args = append(args, "--scaling-pod-service-account", *config.Args.NodeScaleServiceAccount)
	}

	if config.Args.ScalingPodImage != nil {
		args = append(args, "--scaling-pod-image", config.Args.ScalingPodImage.Url())
	}

	if config.Args.NodeScaleNamespace != nil {
		args = append(args, "--scale-adjust-namespace", *config.Args.NodeScaleNamespace)
	}

	if config.Args.GPUMemoryToFractionRatio != nil {
		args = append(args, "--gpu-memory-to-fraction-ratio",
			fmt.Sprintf("%f", *config.Args.GPUMemoryToFractionRatio))
	}

	if jsonLog {
		args = append(args, "--zap-devel=false")
	}

	return args
}
