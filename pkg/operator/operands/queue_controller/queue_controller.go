// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_controller

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

type QueueController struct {
	namespace        string
	lastDesiredState []client.Object
	BaseResourceName string
}

type resourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error)

func (q *QueueController) DesiredState(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	q.namespace = kaiConfig.Spec.Namespace
	if q.BaseResourceName == "" {
		q.BaseResourceName = defaultResourceName
	}

	if *kaiConfig.Spec.QueueController.Service.Enabled == false {
		q.lastDesiredState = []client.Object{}
		return nil, nil
	}

	queueSecret, err := q.secretForKAIConfig(ctx, runtimeClient, kaiConfig)
	if err != nil {
		return nil, err
	}
	if len(queueSecret) == 0 {
		return nil, fmt.Errorf("Failed to create secret")
	}

	var objects []client.Object
	for _, resourceFunc := range []resourceForKAIConfig{
		func(_ context.Context, _ client.Reader, _ *kaiv1.Config) ([]client.Object, error) {
			return []client.Object{queueSecret[0]}, nil
		},
		q.deploymentForKAIConfig,
		q.serviceAccountForKAIConfig,
		q.serviceForKAIConfig,
		crdForKAIConfig,
		func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error) {
			return q.validatingWCForKAIConfig(ctx, runtimeClient, kaiConfig, queueSecret[0].(*v1.Secret))
		},
	} {
		obj, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj...)
	}

	if vpa := common.BuildVPAFromObjects(kaiConfig.Spec.QueueController.VPA, objects, kaiConfig.Spec.Namespace); vpa != nil {
		objects = append(objects, vpa)
	}

	q.lastDesiredState = objects
	return objects, nil
}

func (q *QueueController) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, q.lastDesiredState)
}

func (q *QueueController) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, q.lastDesiredState)
}

func (q *QueueController) Name() string {
	return "QueueController"
}

func (q *QueueController) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	return nil
}

func (q *QueueController) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}
