// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"

	v1 "k8s.io/api/core/v1"

	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
)

func NewState() ksf.CycleState {
	return k8sframework.NewCycleState()
}

type K8sPlugin interface {
	Name() string
	IsRelevant(pod *v1.Pod) bool
	PreFilter(ctx context.Context, pod *v1.Pod, state ksf.CycleState) (error, bool)
	Filter(ctx context.Context, pod *v1.Pod, node *v1.Node, state ksf.CycleState) error
	Allocate(ctx context.Context, pod *v1.Pod, hostname string, state ksf.CycleState) error
	Bind(ctx context.Context, pod *v1.Pod, request *v1alpha2.BindRequest, state ksf.CycleState) error
	PostBind(ctx context.Context, pod *v1.Pod, hostname string, state ksf.CycleState)
	UnAllocate(ctx context.Context, pod *v1.Pod, hostname string, state ksf.CycleState)
}
