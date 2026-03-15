// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deployable

import (
	"context"

	kaiConfig "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Deployable interface {
	Deploy(ctx context.Context, runtimeClient client.Client, kaiConfig *kaiConfig.Config, reconciler client.Object) error
	IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error)
	IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error)
	Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiConfig.Config) error
	HasMissingDependencies(ctx context.Context, readerClient client.Reader, obj client.Object) (string, error)
}
