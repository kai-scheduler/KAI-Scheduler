// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

type Admission struct {
	namespace        string
	lastDesiredState []client.Object
}

type resourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error)

func (b *Admission) DesiredState(
	ctx context.Context, runtimeClient client.Reader,
	kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	b.namespace = kaiConfig.Spec.Namespace

	if kaiConfig.Spec.Admission == nil || kaiConfig.Spec.Admission.Service.Enabled == nil ||
		!*kaiConfig.Spec.Admission.Service.Enabled {
		b.lastDesiredState = []client.Object{}
		return nil, nil
	}

	err, secret, webhookName := upsertKAIAdmissionCertSecret(ctx, runtimeClient, kaiConfig)
	if err != nil {
		return nil, err
	}

	objects := []client.Object{secret}
	for _, resourceFunc := range []resourceForKAIConfig{
		deploymentForKAIConfig,
		serviceAccountForKAIConfig,
		serviceForKAIConfig,
		func(_ context.Context, _ client.Reader, _ *kaiv1.Config) ([]client.Object, error) {
			return []client.Object{secret}, nil
		},
		func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error) {
			return mutatingWCForKAIConfig(ctx, runtimeClient, kaiConfig, secret, webhookName)
		},
		func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error) {
			return validatingWCForKAIConfig(ctx, runtimeClient, kaiConfig, secret, webhookName)
		},
	} {
		newResources, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, newResources...)
	}

	b.lastDesiredState = objects
	return objects, nil
}

func (b *Admission) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, b.lastDesiredState)
}

func (b *Admission) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, b.lastDesiredState)
}

func (b *Admission) Name() string {
	return "KAIAdmission"
}
