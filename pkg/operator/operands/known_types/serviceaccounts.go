// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func serviceAccountIndexer(object client.Object) []string {
	job := object.(*v1.ServiceAccount)
	owner := metav1.GetControllerOf(job)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerServiceAccounts() {
	collectable := &Collectable{
		Collect: getCurrentServiceAccountsState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(ctx, &v1.ServiceAccount{}, CollectableOwnerKey, serviceAccountIndexer)
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder.Owns(&v1.ServiceAccount{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&v1.ServiceAccount{}, CollectableOwnerKey, serviceAccountIndexer)
		},
	}
	SetupKAIConfigOwned(collectable)
	SetupSchedulingShardOwned(collectable)
}

func getCurrentServiceAccountsState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	serviceAccounts := &v1.ServiceAccountList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err := runtimeClient.List(ctx, serviceAccounts, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		return nil, err
	}

	for _, sa := range serviceAccounts.Items {
		result[GetKey(sa.GroupVersionKind(), sa.Namespace, sa.Name)] = &sa
	}

	return result, nil
}
