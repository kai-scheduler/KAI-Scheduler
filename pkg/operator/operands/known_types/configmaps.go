// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func configMapIndexer(object client.Object) []string {
	job := object.(*v1.ConfigMap)
	owner := metav1.GetControllerOf(job)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerConfigmaps() {
	collectable := &Collectable{
		Collect: getCurrentConfigmapsState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(ctx, &v1.ConfigMap{}, CollectableOwnerKey, configMapIndexer)
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder.Owns(&v1.ConfigMap{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&v1.ConfigMap{}, CollectableOwnerKey, configMapIndexer)
		},
	}
	SetupKAIConfigOwned(collectable)
	SetupSchedulingShardOwned(collectable)
}

func getCurrentConfigmapsState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	configMaps := &v1.ConfigMapList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err := runtimeClient.List(ctx, configMaps, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		return nil, err
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	for _, configmap := range configMaps.Items {
		result[GetKey(gvk, configmap.Namespace, configmap.Name)] = &configmap
	}

	return result, nil
}
