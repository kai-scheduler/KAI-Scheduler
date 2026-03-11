// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binder

import (
	"context"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Binder struct {
	namespace        string
	lastDesiredState []client.Object
	BaseResourceName string
}

type resourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error)

func (b *Binder) DesiredState(
	ctx context.Context, runtimeClient client.Reader,
	kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	b.namespace = kaiConfig.Spec.Namespace
	if b.BaseResourceName == "" {
		b.BaseResourceName = defaultResourceName
	}

	if kaiConfig.Spec.Binder == nil || kaiConfig.Spec.Binder.Service.Enabled == nil || !*kaiConfig.Spec.Binder.Service.Enabled {
		b.lastDesiredState = []client.Object{}
		return nil, nil
	}

	objects := []client.Object{}
	for _, resourceFunc := range []resourceForKAIConfig{
		b.deploymentForKAIConfig,
		b.serviceAccountForKAIConfig,
		b.serviceForKAIConfig,
		resourceReservationServiceAccount,
	} {
		newResources, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, newResources...)
	}

	if vpa := common.BuildVPAFromObjects(kaiConfig.Spec.Binder.VPA, objects, kaiConfig.Spec.Namespace); vpa != nil {
		objects = append(objects, vpa)
	}

	b.lastDesiredState = objects
	return objects, nil
}

func (b *Binder) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, b.lastDesiredState)
}

func (b *Binder) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, b.lastDesiredState)
}

func (b *Binder) Name() string {
	return "Binder"
}

func (b *Binder) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	return nil
}

func (b *Binder) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}
