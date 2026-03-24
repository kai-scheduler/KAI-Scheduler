// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func daemonSetIndexer(object client.Object) []string {
	job := object.(*v1.DaemonSet)
	owner := metav1.GetControllerOf(job)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerDaemonSets() {
	SetupKAIConfigOwned(&Collectable{
		Collect: getCurrentDaemonSetsState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(ctx, &v1.DaemonSet{}, CollectableOwnerKey, daemonSetIndexer)
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder.Owns(&v1.DaemonSet{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&v1.DaemonSet{}, CollectableOwnerKey, daemonSetIndexer)
		},
	})
}

func getCurrentDaemonSetsState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	daemonsets := &v1.DaemonSetList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err := runtimeClient.List(ctx, daemonsets, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		return nil, err
	}

	for _, daemonSet := range daemonsets.Items {
		result[GetKey(daemonSet.GroupVersionKind(), daemonSet.Namespace, daemonSet.Name)] = &daemonSet
	}

	return result, nil
}
