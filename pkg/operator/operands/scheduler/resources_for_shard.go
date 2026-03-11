// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"golang.org/x/exp/slices"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/NVIDIA/KAI-scheduler/cmd/scheduler/app/options"
	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	kaiConfigUtils "github.com/NVIDIA/KAI-scheduler/pkg/operator/config"
	"github.com/NVIDIA/KAI-scheduler/pkg/operator/operands/common"
	usagedbapi "github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
)

const (
	invalidJobDepthMapError = "the scheduler's actions are %s. %s isn't one of them, making the queueDepthPerAction invalid"
)

func (s *SchedulerForShard) deploymentForShard(
	ctx context.Context, readerClient client.Reader,
	kaiConfig *kaiv1.Config, shard *kaiv1.SchedulingShard,
) (client.Object, error) {
	shardDeploymentName := deploymentName(kaiConfig, shard)
	config := kaiConfig.Spec.Scheduler

	deployment, err := common.DeploymentForKAIConfig(ctx, readerClient, kaiConfig, config.Service, shardDeploymentName)
	if err != nil {
		return nil, err
	}
	cmObject, err := common.ObjectForKAIConfig(ctx, readerClient, &corev1.ConfigMap{}, configMapName(kaiConfig, shard),
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	schedulerConfig := cmObject.(*corev1.ConfigMap)

	containerArgs, err := buildArgsList(
		shard, kaiConfig, configMountPath,
	)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Replicas = config.Replicas
	deployment.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app": shardDeploymentName,
		},
	}
	deployment.Spec.Strategy.Type = v1.RecreateDeploymentStrategyType
	deployment.Spec.Strategy.RollingUpdate = nil
	deployment.Spec.Template.ObjectMeta = metav1.ObjectMeta{
		Name: shardDeploymentName,
		Labels: map[string]string{
			"app": shardDeploymentName,
		},
		Annotations: map[string]string{
			"configMapVersion": schedulerConfig.ResourceVersion,
		},
	}
	deployment.Spec.Template.Spec.ServiceAccountName = s.BaseResourceName
	deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{
			MountPath: configMountPath,
			Name:      "config",
			SubPath:   "config.yaml",
		},
	}
	deployment.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
		{
			Name:  "GOGC",
			Value: fmt.Sprintf("%d", *config.GOGC),
		},
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}
	deployment.Spec.Template.Spec.Containers[0].Args = containerArgs
	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName(kaiConfig, shard),
					},
				},
			},
			Name: "config",
		},
	}

	return deployment, nil
}

func (s *SchedulerForShard) configMapForShard(
	ctx context.Context, readerClient client.Reader,
	kaiConfig *kaiv1.Config, shard *kaiv1.SchedulingShard,
) (client.Object, error) {
	cmObject, err := common.ObjectForKAIConfig(ctx, readerClient, &corev1.ConfigMap{}, configMapName(kaiConfig, shard),
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	schedulerConfig := cmObject.(*corev1.ConfigMap)
	schedulerConfig.TypeMeta = metav1.TypeMeta{
		Kind:       "ConfigMap",
		APIVersion: "v1",
	}
	innerConfig := conf.SchedulerConfiguration{}

	innerConfig.Tiers = []conf.Tier{{Plugins: resolvePlugins(shard.Spec.Plugins)}}
	actions := resolveActions(shard.Spec.Actions)
	innerConfig.Actions = strings.Join(actions, ", ")

	if len(shard.Spec.QueueDepthPerAction) > 0 {
		if err = validateJobDepthMap(shard, innerConfig, actions); err != nil {
			return nil, err
		}
		// Set the validated map to the scheduler config
		innerConfig.QueueDepthPerAction = shard.Spec.QueueDepthPerAction
	}

	usageDBConfig, err := getUsageDBConfig(shard, kaiConfig)
	if err != nil {
		return nil, err
	}
	innerConfig.UsageDBConfig = usageDBConfig

	data, marshalErr := yaml.Marshal(&innerConfig)
	if marshalErr != nil {
		return nil, marshalErr
	}
	schedulerConfig.Data = map[string]string{
		"config.yaml": string(data),
	}

	return schedulerConfig, nil
}

func validateJobDepthMap(shard *kaiv1.SchedulingShard, innerConfig conf.SchedulerConfiguration, actions []string) error {
	for actionToConfigure := range shard.Spec.QueueDepthPerAction {
		if !slices.Contains(actions, actionToConfigure) {
			return fmt.Errorf(invalidJobDepthMapError, innerConfig.Actions, actionToConfigure)
		}
	}
	return nil
}

func getUsageDBConfig(shard *kaiv1.SchedulingShard, kaiConfig *kaiv1.Config) (*usagedbapi.UsageDBConfig, error) {
	// Check for nil inputs
	if shard == nil {
		return nil, fmt.Errorf("shard cannot be nil")
	}
	if kaiConfig == nil {
		return nil, fmt.Errorf("kaiConfig cannot be nil")
	}

	if shard.Spec.UsageDBConfig == nil {
		return nil, nil
	}

	usageDBConfig := shard.Spec.UsageDBConfig.DeepCopy()

	if usageDBConfig.ClientType != "prometheus" {
		return usageDBConfig, nil
	}

	if usageDBConfig.ConnectionString == "" && usageDBConfig.ConnectionStringEnvVar == "" {
		// Use prometheus from config
		if kaiConfig.Spec.Prometheus != nil &&
			kaiConfig.Spec.Prometheus.Enabled != nil &&
			*kaiConfig.Spec.Prometheus.Enabled {
			usageDBConfig.ConnectionString = fmt.Sprintf("http://usage-prometheus.%s.svc.cluster.local:9090", kaiConfig.Spec.Namespace)
		} else if kaiConfig.Spec.Global != nil && kaiConfig.Spec.Global.ExternalTSDBConnection != nil && kaiConfig.Spec.Global.ExternalTSDBConnection.URL != nil {
			usageDBConfig.ConnectionString = *kaiConfig.Spec.Global.ExternalTSDBConnection.URL
		} else {
			return nil, fmt.Errorf("prometheus connection string not configured: either enable internal prometheus or configure external TSDB connection URL")
		}
	}

	return usageDBConfig, nil
}

func (s *SchedulerForShard) serviceForShard(
	ctx context.Context, readerClient client.Reader,
	kaiConfig *kaiv1.Config, shard *kaiv1.SchedulingShard,
) (client.Object, error) {
	serviceName := fmt.Sprintf("%s-%s", *kaiConfig.Spec.Global.SchedulerName, shard.Name)
	serviceObj, err := common.ObjectForKAIConfig(ctx, readerClient, &corev1.Service{}, serviceName,
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	schedulerConfig := kaiConfig.Spec.Scheduler

	service := serviceObj.(*corev1.Service)
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}

	if service.Annotations == nil {
		service.Annotations = map[string]string{}
	}
	service.Annotations["prometheus.io/scrape"] = "true"

	service.Spec.ClusterIP = "None"
	service.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "http-metrics",
			Port:       int32(*schedulerConfig.SchedulerService.Port),
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromInt(*schedulerConfig.SchedulerService.TargetPort),
		},
	}
	service.Spec.Selector = map[string]string{
		"app": serviceName,
	}
	service.Spec.SessionAffinity = corev1.ServiceAffinityNone
	service.Spec.Type = *schedulerConfig.SchedulerService.Type

	return service, err
}

func buildArgsList(
	shard *kaiv1.SchedulingShard, kaiConfig *kaiv1.Config, configName string,
) ([]string, error) {
	so := options.NewServerOption()
	flagSet := pflag.NewFlagSet("fake", pflag.ContinueOnError)
	so.AddFlags(flagSet)

	args := []string{
		fmt.Sprintf("--%s=%s", "scheduler-conf", configName),
		fmt.Sprintf("--%s=%s", "scheduler-name", *kaiConfig.Spec.Global.SchedulerName),
		fmt.Sprintf("--%s=%s", "namespace", kaiConfig.Spec.Namespace),
		fmt.Sprintf("--%s=%s", "nodepool-label-key", *kaiConfig.Spec.Global.NodePoolLabelKey),
		fmt.Sprintf("--%s=%s", "partition-label-value", shard.Spec.PartitionLabelValue),
		fmt.Sprintf("--%s=%s", "resource-reservation-app-label", *kaiConfig.Spec.Binder.ResourceReservation.AppLabel),
	}

	if kaiConfig.Spec.Scheduler.SchedulerService.Port != nil {
		portNumberString := strconv.Itoa(*kaiConfig.Spec.Scheduler.SchedulerService.Port)
		args = append(args, fmt.Sprintf("--%s=:%s", "listen-address", portNumberString))
	}

	if kaiConfig.Spec.QueueController.MetricsNamespace != nil {
		args = append(args, fmt.Sprintf("--%s=%s", "metrics-namespace", *kaiConfig.Spec.QueueController.MetricsNamespace))
	}

	// Dynamically apply valid scheduler flags from shard args, ignoring unknown flags
	flagSet.VisitAll(func(flag *pflag.Flag) {
		if value, found := shard.Spec.Args[flag.Name]; found {
			args = append(args, fmt.Sprintf("--%s=%v", flag.Name, value))
		}
	})

	if featureGates := kaiConfigUtils.FeatureGatesArg(); featureGates != "" {
		args = append(args, featureGates)
	}
	schedulerConfig := kaiConfig.Spec.Scheduler
	if schedulerConfig.Replicas != nil && *schedulerConfig.Replicas > 1 {
		args = append(args, "--leader-elect=true")
	}

	return args, nil
}

func configMapName(config *kaiv1.Config, shard *kaiv1.SchedulingShard) string {
	return fmt.Sprintf("%s-%s", *config.Spec.Global.SchedulerName, shard.Name)
}

func deploymentName(config *kaiv1.Config, shard *kaiv1.SchedulingShard) string {
	return fmt.Sprintf("%s-%s", *config.Spec.Global.SchedulerName, shard.Name)
}
