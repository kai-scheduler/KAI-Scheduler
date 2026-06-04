// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func endpointSliceIndexer(object client.Object) []string {
	endpointSlice := object.(*discoveryv1.EndpointSlice)
	owner := metav1.GetControllerOf(endpointSlice)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerEndpointSlices() {
	collectable := &Collectable{
		Collect: getCurrentEndpointSlicesState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(ctx, &discoveryv1.EndpointSlice{}, CollectableOwnerKey, endpointSliceIndexer)
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder.Owns(&discoveryv1.EndpointSlice{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&discoveryv1.EndpointSlice{}, CollectableOwnerKey, endpointSliceIndexer)
		},
	}
	SetupSchedulingShardOwned(collectable)
}

func getCurrentEndpointSlicesState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	endpointSlices := &discoveryv1.EndpointSliceList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err := runtimeClient.List(ctx, endpointSlices, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		return nil, err
	}

	gvk := schema.GroupVersionKind{Group: "discovery.k8s.io", Version: "v1", Kind: "EndpointSlice"}
	for _, endpointSlice := range endpointSlices.Items {
		es := endpointSlice
		result[GetKey(gvk, es.Namespace, es.Name)] = &es
	}

	return result, nil
}
