// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_grouper

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

type PodGrouper struct {
	namespace        string
	lastDesiredState []client.Object
	BaseResourceName string
}

type resourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) (client.Object, error)

func (p *PodGrouper) DesiredState(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	p.namespace = kaiConfig.Spec.Namespace
	if p.BaseResourceName == "" {
		p.BaseResourceName = defaultResourceName
	}

	if *kaiConfig.Spec.PodGrouper.Service.Enabled == false {
		p.lastDesiredState = []client.Object{}
		return nil, nil
	}

	var objects []client.Object
	for _, resourceFunc := range []resourceForKAIConfig{
		p.deploymentForKAIConfig,
		p.serviceAccountForKAIConfig,
	} {
		obj, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj)
	}

	if vpa := common.BuildVPAFromObjects(kaiConfig.Spec.PodGrouper.VPA, objects, kaiConfig.Spec.Namespace); vpa != nil {
		objects = append(objects, vpa)
	}

	p.lastDesiredState = objects
	return objects, nil
}

func (p *PodGrouper) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, p.lastDesiredState)
}

func (p *PodGrouper) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, p.lastDesiredState)
}

func (p *PodGrouper) Name() string {
	return "PodGrouper"
}

func (p *PodGrouper) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	return nil
}

func (p *PodGrouper) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}
