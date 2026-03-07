// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package operands

import (
	"context"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Operand interface {
	DesiredState(context.Context, client.Reader, *kaiv1.Config) ([]client.Object, error)
	IsDeployed(context.Context, client.Reader) (bool, error)
	IsAvailable(context.Context, client.Reader) (bool, error)
	Monitor(context.Context, client.Reader, *kaiv1.Config) error
	HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error)
	Name() string
}
