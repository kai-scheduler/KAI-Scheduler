// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

const (
	configMountPath     = "/etc/config/config.yaml"
	binpackStrategy     = "binpack"
	spreadStrategy      = "spread"
	gpuResource         = "gpu"
	cpuResource         = "cpu"
	defaultResourceName = "scheduler"
)

type SchedulerForShard struct {
	schedulingShard *kaiv1.SchedulingShard

	lastDesiredState []client.Object

	BaseResourceName string
}

type SchedulerForConfig struct {
	lastDesiredState []client.Object
	BaseResourceName string
}

func NewSchedulerForShard(schedulingShard *kaiv1.SchedulingShard) *SchedulerForShard {
	return &SchedulerForShard{schedulingShard: schedulingShard, BaseResourceName: defaultResourceName}
}

type resourceForShard func(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config, shardObj *kaiv1.SchedulingShard,
) (client.Object, error)

func (s *SchedulerForShard) DesiredState(
	ctx context.Context, readerClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	logger := log.FromContext(ctx)

	if !*kaiConfig.Spec.Scheduler.Service.Enabled {
		logger.Info("Scheduler operand is disabled")
		s.lastDesiredState = []client.Object{}

		return nil, nil
	}

	objects := []client.Object{}
	for _, resourceFunc := range []resourceForShard{
		s.deploymentForShard,
		s.configMapForShard,
		s.serviceForShard,
	} {
		object, err := resourceFunc(ctx, readerClient, kaiConfig, s.schedulingShard)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}

	if vpa := common.BuildVPAFromObjects(kaiConfig.Spec.Scheduler.VPA, objects, kaiConfig.Spec.Namespace); vpa != nil {
		objects = append(objects, vpa)
	}

	s.lastDesiredState = objects

	return s.lastDesiredState, nil
}

func (s *SchedulerForShard) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	desiredSchedulerDeployment, isOnlyDesiredObject := s.desiredSchedulerDeployment()
	if isOnlyDesiredObject && desiredSchedulerDeployment != nil && desiredSchedulerDeployment.Spec.Replicas != nil &&
		*desiredSchedulerDeployment.Spec.Replicas > 1 {
		return s.isActivePassiveSchedulerAvailable(ctx, readerClient, desiredSchedulerDeployment)
	}
	return common.AllControllersAvailable(ctx, readerClient, s.lastDesiredState)
}

func (s *SchedulerForShard) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, s.lastDesiredState)
}

func (s *SchedulerForShard) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	return nil
}

func (s *SchedulerForShard) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}

func (s *SchedulerForShard) Name() string {
	return "SchedulerForShard"
}

func (s *SchedulerForShard) desiredSchedulerDeployment() (*appsv1.Deployment, bool) {
	if len(s.lastDesiredState) != 1 {
		return nil, false
	}
	deployment, ok := s.lastDesiredState[0].(*appsv1.Deployment)
	return deployment, ok
}

func (s *SchedulerForShard) isActivePassiveSchedulerAvailable(
	ctx context.Context, readerClient client.Reader, desiredDeployment *appsv1.Deployment,
) (bool, error) {
	for _, obj := range s.lastDesiredState {
		if _, isDeployment := obj.(*appsv1.Deployment); isDeployment {
			continue
		}
		if err := readerClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return false, err
		}
	}

	deployment := &appsv1.Deployment{}
	if err := readerClient.Get(ctx, client.ObjectKeyFromObject(desiredDeployment), deployment); err != nil {
		return false, err
	}
	if deployment.Status.UpdatedReplicas != *desiredDeployment.Spec.Replicas {
		return false, nil
	}
	return deployment.Status.ReadyReplicas >= 1, nil
}

func (s *SchedulerForConfig) DesiredState(
	ctx context.Context, readerClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	logger := log.FromContext(ctx)
	if s.BaseResourceName == "" {
		s.BaseResourceName = defaultResourceName
	}

	if !*kaiConfig.Spec.Scheduler.Service.Enabled {
		logger.Info("Scheduler operand is disabled")

		s.lastDesiredState = []client.Object{}

		return nil, nil
	}

	serviceAccount, err := s.serviceAccountForKAIConfig(ctx, readerClient, kaiConfig)
	if err != nil {
		return nil, err
	}

	s.lastDesiredState = []client.Object{serviceAccount}
	return s.lastDesiredState, nil
}

func (s *SchedulerForConfig) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, s.lastDesiredState)
}

func (s *SchedulerForConfig) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, s.lastDesiredState)
}

func (s *SchedulerForConfig) Name() string {
	return "SchedulerForConfig"
}

func (s *SchedulerForConfig) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	return nil
}

func (s *SchedulerForConfig) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}
