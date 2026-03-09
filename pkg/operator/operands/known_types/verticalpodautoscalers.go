// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

func vpaIndexer(object client.Object) []string {
	vpa := object.(*vpav1.VerticalPodAutoscaler)
	owner := metav1.GetControllerOf(vpa)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerVerticalPodAutoscalers() {
	var vpaAvailable bool
	collectable := &Collectable{
		Collect: getCurrentVPAState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			err := mgr.GetFieldIndexer().IndexField(ctx, &vpav1.VerticalPodAutoscaler{}, CollectableOwnerKey, vpaIndexer)
			if err != nil {
				log.FromContext(ctx).Info("VPA CRD not available, skipping field indexer registration", "error", err)
				return nil
			}
			vpaAvailable = true
			return nil
		},
		InitWithBuilder: func(b *builder.Builder) *builder.Builder {
			if !vpaAvailable {
				return b
			}
			return b.Owns(&vpav1.VerticalPodAutoscaler{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&vpav1.VerticalPodAutoscaler{}, CollectableOwnerKey, vpaIndexer)
		},
	}
	SetupKAIConfigOwned(collectable)
	SetupSchedulingShardOwned(collectable)
}

// VPAFieldInherit copies server-managed metadata fields from the current cluster
// object into the desired object so reflect.DeepEqual won't trigger false updates.
func VPAFieldInherit(current, desired client.Object) {
	if current == nil {
		return
	}
	desired.SetResourceVersion(current.GetResourceVersion())
	desired.SetUID(current.GetUID())
	desired.SetCreationTimestamp(current.GetCreationTimestamp())
	desired.SetGeneration(current.GetGeneration())
	desired.SetOwnerReferences(current.GetOwnerReferences())
	desired.SetManagedFields(current.GetManagedFields())
	desired.SetAnnotations(mergeAnnotations(desired.GetAnnotations(), current.GetAnnotations()))

	currentVPA, ok := current.(*vpav1.VerticalPodAutoscaler)
	if !ok {
		return
	}
	desiredVPA, ok := desired.(*vpav1.VerticalPodAutoscaler)
	if !ok {
		return
	}
	desiredVPA.Status = currentVPA.Status
}

func getCurrentVPAState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}

	hasVPACRD, err := common.CheckCRDsAvailable(ctx, runtimeClient, "verticalpodautoscalers.autoscaling.k8s.io")
	if err != nil {
		return nil, err
	}
	if !hasVPACRD {
		return result, nil
	}

	vpas := &vpav1.VerticalPodAutoscalerList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err = runtimeClient.List(ctx, vpas, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		return nil, err
	}

	for _, vpa := range vpas.Items {
		result[GetKey(vpa.GroupVersionKind(), vpa.Namespace, vpa.Name)] = &vpa
	}

	return result, nil
}
