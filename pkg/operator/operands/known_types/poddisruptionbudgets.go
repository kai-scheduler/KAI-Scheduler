// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func podDisruptionBudgetIndexer(object client.Object) []string {
	pdb := object.(*policyv1.PodDisruptionBudget)
	owner := metav1.GetControllerOf(pdb)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerPodDisruptionBudgets() {
	collectable := &Collectable{
		Collect: getCurrentPodDisruptionBudgetsState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(
				ctx,
				&policyv1.PodDisruptionBudget{},
				CollectableOwnerKey,
				podDisruptionBudgetIndexer,
			)
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder.Owns(&policyv1.PodDisruptionBudget{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(
				&policyv1.PodDisruptionBudget{},
				CollectableOwnerKey,
				podDisruptionBudgetIndexer,
			)
		},
	}
	SetupKAIConfigOwned(collectable)
	SetupSchedulingShardOwned(collectable)
}

func getCurrentPodDisruptionBudgetsState(
	ctx context.Context,
	runtimeClient client.Client,
	reconciler client.Object,
) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	pdbs := &policyv1.PodDisruptionBudgetList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err := runtimeClient.List(ctx, pdbs, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		return nil, err
	}

	gvk := schema.GroupVersionKind{Group: policyv1.GroupName, Version: "v1", Kind: "PodDisruptionBudget"}
	for i := range pdbs.Items {
		pdb := &pdbs.Items[i]
		result[GetKey(gvk, pdb.Namespace, pdb.Name)] = pdb
	}

	return result, nil
}
