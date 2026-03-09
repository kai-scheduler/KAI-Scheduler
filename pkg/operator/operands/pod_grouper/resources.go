// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_grouper

import (
	"context"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/operator/operands/common"
)

const (
	defaultResourceName = "pod-grouper"
)

func (p *PodGrouper) deploymentForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) (client.Object, error) {

	config := kaiConfig.Spec.PodGrouper
	deployment, err := common.DeploymentForKAIConfig(ctx, runtimeClient, kaiConfig, config.Service, p.BaseResourceName)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Replicas = config.Replicas
	deployment.Spec.Template.Spec.Containers[0].Args = buildArgsList(kaiConfig)

	return deployment, nil
}

func (p *PodGrouper) serviceAccountForKAIConfig(
	ctx context.Context, k8sReader client.Reader, kaiConfig *kaiv1.Config,
) (client.Object, error) {
	sa, err := common.ObjectForKAIConfig(ctx, k8sReader, &v1.ServiceAccount{}, p.BaseResourceName,
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

func buildArgsList(kaiConfig *kaiv1.Config) []string {
	config := kaiConfig.Spec.PodGrouper
	args := []string{
		"--scheduler-name", *kaiConfig.Spec.Global.SchedulerName,
		"--queue-label-key", *kaiConfig.Spec.Global.QueueLabelKey,
		"--nodepool-label-key", *kaiConfig.Spec.Global.NodePoolLabelKey,
	}

	if config.Args.GangScheduleKnative != nil {
		args = append(args, "--knative-gang-schedule="+strconv.FormatBool(*config.Args.GangScheduleKnative))
	}

	k8sClientConfig := config.K8sClientConfig
	if k8sClientConfig.QPS != nil {
		args = append(args, "--qps", strconv.Itoa(*k8sClientConfig.QPS))
	}
	if k8sClientConfig.Burst != nil {
		args = append(args, "--burst", strconv.Itoa(*k8sClientConfig.Burst))
	}

	if config.MaxConcurrentReconciles != nil {
		args = append(args, "--max-concurrent-reconciles", strconv.Itoa(*config.MaxConcurrentReconciles))
	}
	if config.Replicas != nil && *config.Replicas > 1 {
		args = append(args, "--leader-elect")
	}

	if config.Args.DefaultPrioritiesConfigMapName != nil && config.Args.DefaultPrioritiesConfigMapNamespace != nil {
		args = append(args, "--default-priorities-configmap-name", *config.Args.DefaultPrioritiesConfigMapName,
			"--default-priorities-configmap-namespace", *config.Args.DefaultPrioritiesConfigMapNamespace)
	}

	if len(kaiConfig.Spec.Global.NamespaceLabelSelector) > 0 {
		args = append(args, "--namespace-label-selector", formatLabelSelector(kaiConfig.Spec.Global.NamespaceLabelSelector))
	}

	if len(kaiConfig.Spec.Global.PodLabelSelector) > 0 {
		args = append(args, "--pod-label-selector", formatLabelSelector(kaiConfig.Spec.Global.PodLabelSelector))
	}

	if kaiConfig.Spec.Global.JSONLog != nil && *kaiConfig.Spec.Global.JSONLog {
		args = append(args, "--zap-devel=false")
	}

	return args
}

func formatLabelSelector(selector map[string]string) string {
	if len(selector) == 0 {
		return ""
	}

	pairs := make([]string, 0, len(selector))
	for key, value := range selector {
		pairs = append(pairs, key+"="+value)
	}
	return strings.Join(pairs, ",")
}
