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
	BaseResourceName string
}

type resourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error)

func (a *Admission) DesiredState(
	ctx context.Context, runtimeClient client.Reader,
	kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	a.namespace = kaiConfig.Spec.Namespace
	if a.BaseResourceName == "" {
		a.BaseResourceName = defaultResourceName
	}

	if kaiConfig.Spec.Admission == nil || kaiConfig.Spec.Admission.Service.Enabled == nil ||
		!*kaiConfig.Spec.Admission.Service.Enabled {
		a.lastDesiredState = []client.Object{}
		return nil, nil
	}

	err, secret, webhookName := a.upsertKAIAdmissionCertSecret(ctx, runtimeClient, kaiConfig)
	if err != nil {
		return nil, err
	}

	objects := []client.Object{secret}
	for _, resourceFunc := range []resourceForKAIConfig{
		a.deploymentForKAIConfig,
		a.serviceAccountForKAIConfig,
		a.serviceForKAIConfig,
		func(_ context.Context, _ client.Reader, _ *kaiv1.Config) ([]client.Object, error) {
			return []client.Object{secret}, nil
		},
		func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error) {
			return a.mutatingWCForKAIConfig(ctx, runtimeClient, kaiConfig, secret, webhookName)
		},
		func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error) {
			return a.validatingWCForKAIConfig(ctx, runtimeClient, kaiConfig, secret, webhookName)
		},
	} {
		newResources, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, newResources...)
	}

	if vpa := common.BuildVPAFromObjects(kaiConfig.Spec.Admission.VPA, objects, kaiConfig.Spec.Namespace); vpa != nil {
		objects = append(objects, vpa)
	}

	a.lastDesiredState = objects
	return objects, nil
}

func (a *Admission) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, a.lastDesiredState)
}

func (a *Admission) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, a.lastDesiredState)
}

func (a *Admission) Name() string {
	return "KAIAdmission"
}

func (a *Admission) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	return nil
}

func (a *Admission) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}
