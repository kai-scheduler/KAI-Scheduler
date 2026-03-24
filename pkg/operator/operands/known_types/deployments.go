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

func deploymentIndexer(object client.Object) []string {
	job := object.(*v1.Deployment)
	owner := metav1.GetControllerOf(job)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerDeployments() {
	collectable := &Collectable{
		Collect: getCurrentDeploymentsState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(ctx, &v1.Deployment{}, CollectableOwnerKey, deploymentIndexer)
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder.Owns(&v1.Deployment{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&v1.Deployment{}, CollectableOwnerKey, deploymentIndexer)
		},
	}
	SetupKAIConfigOwned(collectable)
	SetupSchedulingShardOwned(collectable)
}

func getCurrentDeploymentsState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	deployments := &v1.DeploymentList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err := runtimeClient.List(ctx, deployments, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		return nil, err
	}

	for _, deploy := range deployments.Items {
		result[GetKey(deploy.GroupVersionKind(), deploy.Namespace, deploy.Name)] = &deploy
	}

	return result, nil
}
