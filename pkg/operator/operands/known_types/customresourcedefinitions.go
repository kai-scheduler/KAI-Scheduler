// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

const (
	queueCrdName          = "queues.scheduling.run.ai"
	SingletonInstanceName = constants.DefaultKAIConfigSingeltonInstanceName
)

func registerCustomResourceDefinitions() {
	SetupKAIConfigOwned(&Collectable{
		Collect: getCurrentCustomResourceDefinitionsState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return nil
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			builder.Watches(&v1.CustomResourceDefinition{},
				handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
					crd := obj.(*v1.CustomResourceDefinition)
					if crd.Name != queueCrdName {
						return []reconcile.Request{}
					}
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{Name: SingletonInstanceName},
						},
					}
				}))
			return builder.Owns(&admissionv1.MutatingWebhookConfiguration{})
		},
	})
}

func getCurrentCustomResourceDefinitionsState(ctx context.Context, runtimeClient client.Client, _ client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	queueCrd := &v1.CustomResourceDefinition{}
	err := runtimeClient.Get(ctx, types.NamespacedName{Name: queueCrdName}, queueCrd)
	if err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	result[GetKey(queueCrd.GroupVersionKind(), queueCrd.Namespace, queueCrd.Name)] = queueCrd
	return result, nil
}
