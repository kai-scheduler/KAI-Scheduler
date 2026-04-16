// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_controller

import (
	"context"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/pod_group_controller"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

const (
	deploymentName = "podgroup-controller"
)

func deploymentForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) (client.Object, error) {

	config := kaiConfig.Spec.PodGroupController

	deployment, err := common.DeploymentForKAIConfig(ctx, runtimeClient, kaiConfig, config.Service, deploymentName)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Replicas = config.Replicas
	deployment.Spec.Template.Spec.Containers[0].Args = buildArgsList(config, *kaiConfig.Spec.Global.SchedulerName)

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

func buildArgsList(config *pod_group_controller.PodGroupController, schedulerName string) []string {
	args := []string{
		"--scheduler-name", schedulerName,
	}

	common.AddK8sClientConfigToArgs(config.Service.K8sClientConfig, args)

	if config.MaxConcurrentReconciles != nil {
		args = append(args, "--max-concurrent-reconciles", strconv.Itoa(*config.MaxConcurrentReconciles))
	}

	if config.Replicas != nil && *config.Replicas > 1 {
		args = append(args, "--leader-elect")
	}

	return args
}
