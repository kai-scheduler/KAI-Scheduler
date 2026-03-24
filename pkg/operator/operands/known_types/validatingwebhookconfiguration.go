// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func validatingWebhookConfigurationIndexer(object client.Object) []string {
	job := object.(*admissionv1.ValidatingWebhookConfiguration)
	owner := metav1.GetControllerOf(job)
	if !checkOwnerType(owner) {
		return nil
	}
	return []string{getOwnerKey(owner)}
}

func registerValidatingWebhookConfigurations() {
	SetupKAIConfigOwned(&Collectable{
		Collect: getCurrentValidatingWebhookConfigurationsState,
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(ctx, &admissionv1.ValidatingWebhookConfiguration{}, CollectableOwnerKey, validatingWebhookConfigurationIndexer)
		},
		InitWithBuilder: func(builder *builder.Builder) *builder.Builder {
			return builder.Owns(&admissionv1.ValidatingWebhookConfiguration{})
		},
		InitWithFakeClientBuilder: func(fakeClientBuilder *fake.ClientBuilder) {
			fakeClientBuilder.WithIndex(&admissionv1.ValidatingWebhookConfiguration{}, CollectableOwnerKey, validatingWebhookConfigurationIndexer)
		},
	})
}

func getCurrentValidatingWebhookConfigurationsState(ctx context.Context, runtimeClient client.Client, reconciler client.Object) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	webhookconfigurations := &admissionv1.ValidatingWebhookConfigurationList{}
	reconcilerKey := getReconcilerKey(reconciler)

	err := runtimeClient.List(ctx, webhookconfigurations, client.MatchingFields{CollectableOwnerKey: reconcilerKey})
	if err != nil {
		return nil, err
	}

	for _, sa := range webhookconfigurations.Items {
		result[GetKey(sa.GroupVersionKind(), sa.Namespace, sa.Name)] = &sa
	}

	return result, nil
}

func ValidatingWebhookConfigurationFieldInherit(current, desired client.Object) {
	if current == nil {
		return
	}
	currentT := current.(*admissionv1.ValidatingWebhookConfiguration)
	desiredT := desired.(*admissionv1.ValidatingWebhookConfiguration)

	desiredT.Annotations = mergeAnnotations(desiredT.Annotations, currentT.Annotations)
	if len(currentT.Webhooks) == len(desiredT.Webhooks) {
		for webhookIndex, currentWebhook := range currentT.Webhooks {
			if desiredT.Webhooks[webhookIndex].NamespaceSelector == nil {
				desiredT.Webhooks[webhookIndex].NamespaceSelector = currentWebhook.NamespaceSelector
			}
		}
	}
}
