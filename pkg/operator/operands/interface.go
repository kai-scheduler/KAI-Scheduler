// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package operands

import (
	"context"

	enginev1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Operand interface {
	DesiredState(context.Context, client.Reader, *enginev1alpha1.Config) ([]client.Object, error)
	IsDeployed(context.Context, client.Reader) (bool, error)
	IsAvailable(context.Context, client.Reader) (bool, error)
	Name() string
}
