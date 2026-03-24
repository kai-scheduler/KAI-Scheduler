// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"

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
