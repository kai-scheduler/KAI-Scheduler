// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"context"
	"fmt"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	CollectableOwnerKey = ".metadata.controller"
)

type Collectable struct {
	Collect                   func(context.Context, client.Client, client.Object) (map[string]client.Object, error)
	InitWithManager           func(context.Context, manager.Manager) error
	InitWithBuilder           func(builder *builder.Builder) *builder.Builder
	InitWithFakeClientBuilder func(clientBuilder *fake.ClientBuilder)
}

var (
	KAIConfigRegisteredCollectible       []*Collectable
	SchedulingShardRegisteredCollectable []*Collectable

	InitiatedCollectables []*Collectable
)

func init() {
	registerDeployments()
	registerDaemonSets()
	registerServiceAccounts()
	registerConfigmaps()
	registerServices()
	registerSecrets()
	registerMutatingWebhookConfigurations()
	registerValidatingWebhookConfigurations()
	registerCustomResourceDefinitions()
	registerPrometheus()
	registerVerticalPodAutoscalers()
}

func SetupKAIConfigOwned(fn *Collectable) {
	KAIConfigRegisteredCollectible = append(KAIConfigRegisteredCollectible, fn)
}

func SetupSchedulingShardOwned(fn *Collectable) {
	SchedulingShardRegisteredCollectable = append(SchedulingShardRegisteredCollectable, fn)
}

func MarkInitiatedWithManager(fn *Collectable) {
	InitiatedCollectables = append(InitiatedCollectables, fn)
}

func GetKey(kind schema.GroupVersionKind, namespace, name string) string {
	return fmt.Sprintf("%s/%s", kind, types.NamespacedName{Namespace: namespace, Name: name})
}

func getOwnerKey(owner *metav1.OwnerReference) string {
	groupVersion, _ := schema.ParseGroupVersion(owner.APIVersion)
	ownerScheme := schema.GroupVersionKind{Group: groupVersion.Group, Version: groupVersion.Version, Kind: owner.Kind}
	return GetKey(ownerScheme, "", owner.Name)
}

func checkOwnerType(owner *metav1.OwnerReference) bool {
	if owner == nil {
		return false
	}
	if owner.APIVersion != kaiv1.GroupVersion.String() {
		return false
	}
	return true
}

func getReconcilerKey(reconciler client.Object) string {
	return GetKey(reconciler.GetObjectKind().GroupVersionKind(), "", reconciler.GetName())
}
