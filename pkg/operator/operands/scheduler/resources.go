// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/common"
)

func (s *SchedulerForConfig) serviceAccountForKAIConfig(
	ctx context.Context, readerClient client.Reader,
	kaiConfig *kaiv1.Config,
) (*corev1.ServiceAccount, error) {
	sa, err := common.ObjectForKAIConfig(
		ctx, readerClient, &corev1.ServiceAccount{},
		s.BaseResourceName, kaiConfig.Spec.Namespace,
	)
	if err != nil {
		return nil, err
	}
	sa.(*corev1.ServiceAccount).TypeMeta = metav1.TypeMeta{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
	}
	return sa.(*corev1.ServiceAccount), err
}
