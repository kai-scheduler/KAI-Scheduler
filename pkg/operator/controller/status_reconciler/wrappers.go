// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_reconciler

import (
	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type KAIConfigWithStatusWrapper struct {
	*kaiv1.Config
}

func (e *KAIConfigWithStatusWrapper) GetConditions() []metav1.Condition {
	return e.Status.Conditions
}

func (e *KAIConfigWithStatusWrapper) SetConditions(conditions []metav1.Condition) {
	e.Status.Conditions = conditions
}

func (e *KAIConfigWithStatusWrapper) DeepCopy() objectWithConditions {
	return &KAIConfigWithStatusWrapper{e.Config.DeepCopy()}
}

func (e *KAIConfigWithStatusWrapper) GetInternalObject() client.Object {
	return e.Config
}

type SchedulingShardWithStatusWrapper struct {
	*kaiv1.SchedulingShard
}

func (s *SchedulingShardWithStatusWrapper) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

func (s *SchedulingShardWithStatusWrapper) SetConditions(conditions []metav1.Condition) {
	s.Status.Conditions = conditions
}

func (s *SchedulingShardWithStatusWrapper) DeepCopy() objectWithConditions {
	return &SchedulingShardWithStatusWrapper{
		SchedulingShard: s.SchedulingShard.DeepCopy(),
	}
}

func (s *SchedulingShardWithStatusWrapper) GetInternalObject() client.Object {
	return s.SchedulingShard
}
