// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binder

import (
	"context"
	"encoding/json"
	"fmt"

	"golang.org/x/mod/semver"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1binder "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/binder"
	kaiConfigUtils "github.com/NVIDIA/KAI-scheduler/pkg/operator/config"
	"github.com/NVIDIA/KAI-scheduler/pkg/operator/operands/common"
)

const (
	defaultResourceName                    = "binder"
	gpuOperatorVersionDefaultCDIDeprecated = "v25.10.0"
	versionLabelName                       = "app.kubernetes.io/version"
)

func (b *Binder) deploymentForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {

	config := kaiConfig.Spec.Binder
	deployment, err := common.DeploymentForKAIConfig(ctx, runtimeClient, kaiConfig, config.Service, b.BaseResourceName)
	if err != nil {
		return nil, err
	}

	fakeGPU, err := hasFakeGPUNodes(ctx, runtimeClient)
	if err != nil {
		return nil, err
	}

	var cdiEnabled bool
	if config.CDIEnabled != nil {
		cdiEnabled = *config.CDIEnabled
	} else {
		cdiEnabled, err = isCdiEnabled(ctx, runtimeClient)
		if err != nil {
			return nil, err
		}
	}

	deployment.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
	deployment.Spec.Strategy.RollingUpdate = nil
	deployment.Spec.Replicas = config.Replicas
	deployment.Spec.Template.Spec.Containers[0].Args = buildArgsList(kaiConfig, config, fakeGPU, cdiEnabled)

	return []client.Object{deployment}, nil
}

func (b *Binder) serviceAccountForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	sa, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.ServiceAccount{}, b.BaseResourceName,
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	sa.(*v1.ServiceAccount).TypeMeta = metav1.TypeMeta{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
	}
	return []client.Object{sa}, err
}

func (b *Binder) serviceForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	serviceObj, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.Service{}, b.BaseResourceName,
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}

	service := serviceObj.(*v1.Service)
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}
	config := kaiConfig.Spec.Binder

	service.Spec.Ports = []v1.ServicePort{
		{
			Name:       "http-metrics",
			Port:       int32(*config.MetricsPort),
			Protocol:   v1.ProtocolTCP,
			TargetPort: intstr.FromInt(*config.MetricsPort),
		},
	}
	service.Spec.Selector = map[string]string{
		"app": b.BaseResourceName,
	}

	service.Spec.SessionAffinity = v1.ServiceAffinityNone
	service.Spec.Type = v1.ServiceTypeClusterIP

	return []client.Object{service}, nil
}

func hasFakeGPUNodes(ctx context.Context, k8sClient client.Reader) (bool, error) {
	var nodes v1.NodeList

	err := k8sClient.List(
		ctx, &nodes,
		client.MatchingLabels{
			"run.ai/fake.gpu": "true",
		})
	if err != nil {
		return false, err
	}

	if len(nodes.Items) > 0 {
		return true, nil
	}

	return false, nil
}

func resourceReservationServiceAccount(
	ctx context.Context, readerClient client.Reader,
	kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	sa := &v1.ServiceAccount{}

	err := readerClient.Get(ctx, client.ObjectKey{
		Namespace: *kaiConfig.Spec.Binder.ResourceReservation.Namespace,
		Name:      *kaiConfig.Spec.Binder.ResourceReservation.ServiceAccountName,
	}, sa)
	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}

	sa.Name = *kaiConfig.Spec.Binder.ResourceReservation.ServiceAccountName
	sa.Namespace = *kaiConfig.Spec.Binder.ResourceReservation.Namespace

	imagePullSecrets := make(map[string]bool)
	for _, secret := range sa.ImagePullSecrets {
		imagePullSecrets[secret.Name] = true
	}

	for _, secret := range kaiConfigUtils.GetGlobalImagePullSecrets(kaiConfig.Spec.Global) {
		if !imagePullSecrets[secret.Name] {
			imagePullSecrets[secret.Name] = true
		}
	}

	sa.ImagePullSecrets = make([]v1.LocalObjectReference, 0, len(imagePullSecrets))
	for secretName := range imagePullSecrets {
		sa.ImagePullSecrets = append(sa.ImagePullSecrets, v1.LocalObjectReference{Name: secretName})
	}

	return []client.Object{sa}, nil
}

func isCdiEnabled(ctx context.Context, readerClient client.Reader) (bool, error) {
	nvidiaClusterPolicies := &nvidiav1.ClusterPolicyList{}
	err := readerClient.List(ctx, nvidiaClusterPolicies)
	if err != nil {
		if meta.IsNoMatchError(err) || kerrors.IsNotFound(err) {
			return false, nil
		}
		logger := log.FromContext(ctx)
		logger.Error(err, "cannot list nvidia cluster policy")
		return false, err
	}

	if len(nvidiaClusterPolicies.Items) == 0 {
		return false, nil
	}
	if len(nvidiaClusterPolicies.Items) > 1 {
		logger := log.FromContext(ctx)
		logger.Info(fmt.Sprintf("Cluster has %d clusterpolicies.nvidia.com/v1 objects."+
			" First one is queried for the cdi configuration", len(nvidiaClusterPolicies.Items)))
	}

	nvidiaClusterPolicy := nvidiaClusterPolicies.Items[0]
	if nvidiaClusterPolicy.Spec.CDI.Enabled != nil && *nvidiaClusterPolicy.Spec.CDI.Enabled {
		gpuOperatorVersion, found := nvidiaClusterPolicy.Labels[versionLabelName]
		if found && semver.Compare(gpuOperatorVersion, gpuOperatorVersionDefaultCDIDeprecated) >= 0 {
			return true, nil
		}
		if nvidiaClusterPolicy.Spec.CDI.Default != nil && *nvidiaClusterPolicy.Spec.CDI.Default {
			return true, nil
		}
	}

	return false, nil
}

func buildArgsList(kaiConfig *kaiv1.Config, config *kaiv1binder.Binder, fakeGPU bool, cdiEnabled bool) []string {
	args := []string{
		"--scheduler-name",
		*kaiConfig.Spec.Global.SchedulerName,
		"--resource-reservation-namespace",
		*config.ResourceReservation.Namespace,
		"--resource-reservation-service-account",
		*config.ResourceReservation.ServiceAccountName,
		"--resource-reservation-app-label",
		*config.ResourceReservation.AppLabel,
		"--resource-reservation-pod-image",
		config.ResourceReservation.Image.Url(),
		"--scale-adjust-namespace",
		*kaiConfig.Spec.NodeScaleAdjuster.Args.NodeScaleNamespace,
		"--health-probe-bind-address",
		fmt.Sprintf(":%d", *config.ProbePort),
		"--metrics-bind-address",
		fmt.Sprintf(":%d", *config.MetricsPort),
		fmt.Sprintf("--cdi-enabled=%t", cdiEnabled),
	}
	if config.MaxConcurrentReconciles != nil {
		args = append(args, fmt.Sprintf("--max-concurrent-reconciles=%d",
			*config.MaxConcurrentReconciles))
	}

	if config.ResourceReservation.AllocationTimeout != nil {
		args = append(args, fmt.Sprintf("--resource-reservation-allocation-timeout=%d",
			*config.ResourceReservation.AllocationTimeout))
	}

	if config.VolumeBindingTimeoutSeconds != nil {
		args = append(args, fmt.Sprintf("--volume-binding-timeout-seconds=%d",
			*config.VolumeBindingTimeoutSeconds))
	}

	if fakeGPU {
		args = append(args, "--fake-gpu-nodes")
	}

	if config.Replicas != nil && *config.Replicas > 1 {
		args = append(args, "--leader-elect")
	}

	if featureGates := kaiConfigUtils.FeatureGatesArg(); featureGates != "" {
		args = append(args, featureGates)
	}

	if config.Service.K8sClientConfig.QPS != nil {
		args = append(args, []string{"--qps", fmt.Sprintf("%d", *config.Service.K8sClientConfig.QPS)}...)
	}
	if config.Service.K8sClientConfig.Burst != nil {
		args = append(args, []string{"--burst", fmt.Sprintf("%d", *config.Service.K8sClientConfig.Burst)}...)
	}

	if config.ResourceReservation.RuntimeClassName != nil && len(*config.ResourceReservation.RuntimeClassName) > 0 {
		args = append(args, []string{fmt.Sprintf("--runtime-class-name=%s", *config.ResourceReservation.RuntimeClassName)}...)
	}

	// Serialize and add GPU reservation pod resource configurations
	if config.ResourceReservation.PodResources != nil {
		resourceRequirements := v1.ResourceRequirements{
			Requests: config.ResourceReservation.PodResources.Requests,
			Limits:   config.ResourceReservation.PodResources.Limits,
		}
		resourcesJSON, err := json.Marshal(resourceRequirements)
		if err == nil {
			args = append(args, []string{"--resource-reservation-pod-resources", string(resourcesJSON)}...)
		}
	}

	return args
}
