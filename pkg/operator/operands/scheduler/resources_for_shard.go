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
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kai-scheduler/KAI-scheduler/cmd/scheduler/app/options"
	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
)

const (
	invalidJobDepthMapError = "the scheduler's actions are %s. %s isn't one of them, making the queueDepthPerAction invalid"
)

func (s *SchedulerForShard) deploymentForShard(
	ctx context.Context, readerClient client.Reader,
	kaiConfig *kaiv1.Config, shard *kaiv1.SchedulingShard,
) (client.Object, error) {
	shardDeploymentName := DeploymentName(kaiConfig, shard)
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
	if err = validateScenarioSearchBudgets(shard.Spec.ScenarioSearchBudgets); err != nil {
		return nil, err
	}
	if shard.Spec.ScenarioSearchBudgets != nil {
		innerConfig.ScenarioSearchBudgets = shard.Spec.ScenarioSearchBudgets.DeepCopy()
	}

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

var validScenarioSearchActionKeys = []string{
	constants.ActionDefault,
	constants.ActionReclaim,
	constants.ActionPreempt,
	constants.ActionConsolidation,
}

func validateScenarioSearchBudgets(config *kaiv1.ScenarioSearchBudgets) error {
	if config == nil {
		return nil
	}
	if err := validateDurationMap(
		"maxActionSearchDuration", config.MaxActionSearchDuration, validScenarioSearchActionKeySet(), validScenarioSearchActionKeys,
	); err != nil {
		return err
	}
	if err := validateDurationMap("maxGeneratorSearchDuration", config.MaxGeneratorSearchDuration, nil, nil); err != nil {
		return err
	}
	return validateMinJobBudget(config.MinJobSearchDuration, config.MaxJobSearchDuration)
}

func validScenarioSearchActionKeySet() map[string]struct{} {
	validKeys := make(map[string]struct{}, len(validScenarioSearchActionKeys))
	for _, key := range validScenarioSearchActionKeys {
		validKeys[key] = struct{}{}
	}
	return validKeys
}

func validateDurationMap(fieldName string, durations map[string]metav1.Duration, validKeys map[string]struct{}, validKeyNames []string) error {
	for key, duration := range durations {
		if validKeys != nil {
			if _, found := validKeys[key]; !found {
				return fmt.Errorf(
					"%s contains invalid action key %q; valid action keys: %s",
					fieldName, key, strings.Join(validKeyNames, ", "),
				)
			}
		}
		if duration.Duration < 0 {
			return fmt.Errorf("%s[%q] must be non-negative", fieldName, key)
		}
	}
	return nil
}

func validateMinJobBudget(minJobBudget, maxJobBudget *metav1.Duration) error {
	if minJobBudget == nil || maxJobBudget == nil {
		return nil
	}
	if maxJobBudget.Duration < 0 {
		return fmt.Errorf("maxJobSearchDuration must be non-negative")
	}
	if minJobBudget.Duration < 0 {
		return fmt.Errorf("minJobSearchDuration must be non-negative")
	}
	if maxJobBudget.Duration == 0 {
		return nil
	}
	if minJobBudget.Duration >= maxJobBudget.Duration {
		return fmt.Errorf("minJobSearchDuration must be less than maxJobSearchDuration")
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
		} else if kaiConfig.Spec.Prometheus != nil && kaiConfig.Spec.Prometheus.ExternalPrometheusUrl != nil {
			usageDBConfig.ConnectionString = *kaiConfig.Spec.Prometheus.ExternalPrometheusUrl
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
	// With more than one replica, the operator maintains a custom EndpointSlice
	// pointing at the leader-election lease holder, so the Service must be
	// selectorless. With a single replica there is no leader election, so the
	// usual label-based selector is sufficient.
	if schedulerConfig.Replicas != nil && *schedulerConfig.Replicas > 1 {
		service.Spec.Selector = nil
	} else {
		service.Spec.Selector = map[string]string{
			"app": serviceName,
		}
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
		fmt.Sprintf("--%s=%s", "queue-label-key", *kaiConfig.Spec.Global.QueueLabelKey),
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

	schedulerConfig := kaiConfig.Spec.Scheduler
	if schedulerConfig.Replicas != nil && *schedulerConfig.Replicas > 1 {
		args = append(args, "--leader-elect=true")
	}

	return common.AddSchedulerJSONLogArg(kaiConfig.Spec.Global.JSONLog, args), nil
}

func configMapName(config *kaiv1.Config, shard *kaiv1.SchedulingShard) string {
	return fmt.Sprintf("%s-%s", *config.Spec.Global.SchedulerName, shard.Name)
}

func DeploymentName(config *kaiv1.Config, shard *kaiv1.SchedulingShard) string {
	return fmt.Sprintf("%s-%s", *config.Spec.Global.SchedulerName, shard.Name)
}

// serviceName for the per-shard scheduler Service.
func serviceName(config *kaiv1.Config, shard *kaiv1.SchedulingShard) string {
	return fmt.Sprintf("%s-%s", *config.Spec.Global.SchedulerName, shard.Name)
}

func LeaseName(config *kaiv1.Config, shard *kaiv1.SchedulingShard) string {
	if shard.Spec.PartitionLabelValue != "" {
		return fmt.Sprintf("%s-%s", *config.Spec.Global.SchedulerName, shard.Spec.PartitionLabelValue)
	}
	return *config.Spec.Global.SchedulerName
}

// endpointSliceForShard produces an EndpointSlice pointing at the current
// leader-election Lease holder's pod IP. Returns (nil, nil) when leader
// election is not in use (replicas <= 1), in which case the Service's label
// selector populates endpoints normally.
//
// HolderIdentity contract with the scheduler: "<podName>_<uuid>" — see
// cmd/scheduler/app/server.go.
func (s *SchedulerForShard) endpointSliceForShard(
	ctx context.Context, readerClient client.Reader,
	kaiConfig *kaiv1.Config, shard *kaiv1.SchedulingShard,
) (client.Object, error) {
	schedulerConfig := kaiConfig.Spec.Scheduler
	if schedulerConfig.Replicas == nil || *schedulerConfig.Replicas <= 1 {
		return nil, nil
	}

	svcName := serviceName(kaiConfig, shard)
	namespace := kaiConfig.Spec.Namespace
	port := int32(*schedulerConfig.SchedulerService.Port)

	es := &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EndpointSlice",
			APIVersion: discoveryv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName + "-leader",
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: svcName,
				discoveryv1.LabelManagedBy:   "kai-operator",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{
				Name:     ptr.To("http-metrics"),
				Port:     ptr.To(port),
				Protocol: ptr.To(corev1.ProtocolTCP),
			},
		},
		Endpoints: []discoveryv1.Endpoint{},
	}

	lease := &coordinationv1.Lease{}
	leaseKey := client.ObjectKey{Namespace: namespace, Name: LeaseName(kaiConfig, shard)}
	if err := readerClient.Get(ctx, leaseKey, lease); err != nil {
		if apierrors.IsNotFound(err) {
			return es, nil
		}
		return nil, err
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return es, nil
	}
	podName := strings.SplitN(*lease.Spec.HolderIdentity, "_", 2)[0]

	pod := &corev1.Pod{}
	if err := readerClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return es, nil
		}
		return nil, err
	}
	if pod.Status.PodIP == "" || pod.DeletionTimestamp != nil {
		return es, nil
	}

	es.Endpoints = []discoveryv1.Endpoint{
		{
			Addresses:  []string{pod.Status.PodIP},
			Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Namespace: namespace,
				Name:      podName,
				UID:       pod.UID,
			},
		},
	}
	return es, nil
}
