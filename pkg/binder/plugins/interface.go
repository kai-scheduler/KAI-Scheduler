// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v1alpha2"

	"github.com/kai-scheduler/kai-scheduler/pkg/binder/plugins/state"
)

type Plugin interface {
	Name() string
	PreBind(ctx context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest,
		state *state.BindingState) error
	PostBind(ctx context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest,
		state *state.BindingState)
	Rollback(ctx context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest,
		state *state.BindingState) error
}
