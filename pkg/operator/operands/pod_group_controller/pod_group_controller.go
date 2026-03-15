// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_controller

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

type PodGroupController struct {
	namespace        string
	lastDesiredState []client.Object
	BaseResourceName string
}

type resourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error)

func (p *PodGroupController) DesiredState(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	p.namespace = kaiConfig.Spec.Namespace
	if p.BaseResourceName == "" {
		p.BaseResourceName = defaultResourceName
	}

	if *kaiConfig.Spec.PodGroupController.Service.Enabled == false {
		p.lastDesiredState = []client.Object{}
		return nil, nil
	}

	secret, err := p.secretForKAIConfig(ctx, runtimeClient, kaiConfig)
	if err != nil {
		return nil, err
	}
	if len(secret) == 0 {
		return nil, fmt.Errorf("failed to create secret")
	}

	objects := []client.Object{}
	for _, resourceFunc := range []resourceForKAIConfig{
		func(_ context.Context, _ client.Reader, _ *kaiv1.Config) ([]client.Object, error) {
			return []client.Object{secret[0]}, nil
		},
		p.deploymentForKAIConfig,
		p.serviceAccountForKAIConfig,
		p.serviceForKAIConfig,
		func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error) {
			return p.validatingWCForKAIConfig(ctx, runtimeClient, kaiConfig, secret[0].(*v1.Secret))
		},
	} {
		obj, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj...)
	}

	if vpa := common.BuildVPAFromObjects(kaiConfig.Spec.PodGroupController.VPA, objects, kaiConfig.Spec.Namespace); vpa != nil {
		objects = append(objects, vpa)
	}

	p.lastDesiredState = objects
	return objects, nil
}

func (p *PodGroupController) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, p.lastDesiredState)
}

func (p *PodGroupController) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, p.lastDesiredState)
}

func (p *PodGroupController) Name() string {
	return "PodGroupController"
}

func (p *PodGroupController) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	return nil
}

func (p *PodGroupController) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}
