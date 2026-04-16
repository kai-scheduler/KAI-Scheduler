// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_controller

import (
	"context"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodGroupController struct {
	namespace        string
	lastDesiredState []client.Object
}

type resourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) (client.Object, error)

func (p *PodGroupController) DesiredState(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	p.namespace = kaiConfig.Spec.Namespace

	if *kaiConfig.Spec.PodGroupController.Service.Enabled == false {
		p.lastDesiredState = []client.Object{}
		return nil, nil
	}

	objects := []client.Object{}
	for _, resourceFunc := range []resourceForKAIConfig{
		deploymentForKAIConfig,
		serviceAccountForKAIConfig,
	} {
		obj, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj)
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
