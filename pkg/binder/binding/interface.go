// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"context"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v1alpha2"
)

type Interface interface {
	Bind(ctx context.Context, task *v1.Pod, host *v1.Node, bindRequest *v1alpha2.BindRequest) error
	Rollback(ctx context.Context, task *v1.Pod, host *v1.Node, bindRequest *v1alpha2.BindRequest) error
}
