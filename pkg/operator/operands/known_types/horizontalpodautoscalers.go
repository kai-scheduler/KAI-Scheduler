// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func horizontalPodAutoscalerIndexer(object client.Object) []string {
	hpa := object.(*autoscalingv2.HorizontalPodAutoscaler)
	owner := metav1.GetControllerOf(hpa)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerHorizontalPodAutoscalers() {
	collectable := &Collectable{
		Collect: getCurrentHorizontalPodAutoscalersState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(
				ctx,
				&autoscalingv2.HorizontalPodAutoscaler{},
				CollectableOwnerKey,
				horizontalPodAutoscalerIndexer,
			)
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder.Owns(&autoscalingv2.HorizontalPodAutoscaler{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(
				&autoscalingv2.HorizontalPodAutoscaler{},
				CollectableOwnerKey,
				horizontalPodAutoscalerIndexer,
			)
		},
	}

	SetupKAIConfigOwned(collectable)
}

func HPAFieldInherit(current, desired client.Object) {
	if current == nil {
		return
	}

	desired.SetResourceVersion(current.GetResourceVersion())
	desired.SetUID(current.GetUID())
	desired.SetCreationTimestamp(current.GetCreationTimestamp())
	desired.SetGeneration(current.GetGeneration())
	desired.SetOwnerReferences(current.GetOwnerReferences())
	desired.SetManagedFields(current.GetManagedFields())
	desired.SetAnnotations(mergeAnnotations(
		desired.GetAnnotations(),
		current.GetAnnotations(),
	))

	currentHPA, ok := current.(*autoscalingv2.HorizontalPodAutoscaler)
	if !ok {
		return
	}

	desiredHPA, ok := desired.(*autoscalingv2.HorizontalPodAutoscaler)
	if !ok {
		return
	}

	desiredHPA.Status = currentHPA.Status
}

func getCurrentHorizontalPodAutoscalersState(
	ctx context.Context,
	runtimeClient client.Client,
	reconciler client.Object,
) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	hpas := &autoscalingv2.HorizontalPodAutoscalerList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err := runtimeClient.List(
		ctx,
		hpas,
		client.MatchingFields{CollectableOwnerKey: reconcilerKey},
	)
	if err != nil {
		return nil, err
	}

	gvk := schema.GroupVersionKind{
		Group:   autoscalingv2.GroupName,
		Version: "v2",
		Kind:    "HorizontalPodAutoscaler",
	}

	for i := range hpas.Items {
		hpa := &hpas.Items[i]
		result[GetKey(gvk, hpa.Namespace, hpa.Name)] = hpa
	}

	return result, nil
}
