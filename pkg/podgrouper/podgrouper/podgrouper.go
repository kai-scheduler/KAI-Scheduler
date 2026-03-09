// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgrouper

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgroup"
	pluginshub "github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/hub"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/grouper"
)

type Interface interface {
	// GetPodOwners returns all the owners of a pod, the top owner returned also as Unstructured
	GetPodOwners(ctx context.Context, pod *v1.Pod) (*unstructured.Unstructured, []*metav1.PartialObjectMetadata, error)
	GetPGMetadata(ctx context.Context, pod *v1.Pod, topOwner *unstructured.Unstructured, allOwners []*metav1.PartialObjectMetadata) (*podgroup.Metadata, error)
}

type podGrouper struct {
	client     client.Client
	pluginsHub pluginshub.PluginsHub

	// For runtime client with cache enabled, GET/LIST calls create an informer/watch for the specified object.
	// Unfortunetly, this is true even if the GET call fails with an RBAC error. The created listener will panic and the reconciler crashes.
	// Using a client without cache for calls that might get this error allow ius to handle the error gracefully.
	// https://github.com/kubernetes/client-go/issues/1310#issuecomment-1921598658
	// https://github.com/kubernetes-sigs/controller-runtime/issues/1222#issuecomment-713037979
	clientWithoutCache client.Client
}

type GetPodGroupMetadataFunc func(topOwner *unstructured.Unstructured, pod *v1.Pod, otherOwners ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error)

func NewPodgrouper(client client.Client, clientWithoutCache client.Client, pluginsHub pluginshub.PluginsHub) *podGrouper {
	podGrouper := &podGrouper{
		client:             client,
		clientWithoutCache: clientWithoutCache,
		pluginsHub:         pluginsHub,
	}

	return podGrouper
}

func (pg *podGrouper) GetPodOwners(ctx context.Context, pod *v1.Pod) (
	*unstructured.Unstructured, []*metav1.PartialObjectMetadata, error,
) {
	logger := log.FromContext(ctx)
	logger.V(1).Info(fmt.Sprintf("Pod %s/%s owners: %+v", pod.Namespace, pod.Name, pod.OwnerReferences))

	var topOwner *metav1.PartialObjectMetadata
	var owners = make([]*metav1.PartialObjectMetadata, 0)
	var err error
	if len(pod.OwnerReferences) > 0 {
		topOwner, owners, err = pg.getResourceOwners(ctx, pod)
	} else {
		topOwner, err = pg.getPodAsResourceOwner(pod)
	}
	if err != nil {
		return nil, nil, err
	}

	logger.V(1).Info(fmt.Sprintf("Found top owner: %s/%s, kind: %s", topOwner.Namespace, topOwner.Name,
		topOwner.Kind))
	topOwnerInstance, err := pg.getTopOwnerInstance(ctx, topOwner)
	return topOwnerInstance, owners, err
}

func (pg *podGrouper) GetPGMetadata(ctx context.Context, pod *v1.Pod, topOwner *unstructured.Unstructured, allOwners []*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	logger := log.FromContext(ctx)
	plugin := pg.selectPluginForOwners(topOwner, allOwners)
	logger.V(1).Info(fmt.Sprintf("Using %v plugin for pod.", plugin.Name()),
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name), "topOwner", topOwner.GroupVersionKind())
	return plugin.GetPodGroupMetadata(topOwner, pod, allOwners...)
}

func (pg *podGrouper) getResourceOwners(ctx context.Context, pod *v1.Pod) (
	ownerInstance *metav1.PartialObjectMetadata, owners []*metav1.PartialObjectMetadata, err error,
) {
	logger := log.FromContext(ctx)
	podOwnerRef := &pod.OwnerReferences[0]
	owners = make([]*metav1.PartialObjectMetadata, 0)

	var nextOwner *metav1.OwnerReference
	var lastOwnerInstance *metav1.PartialObjectMetadata
	for nextOwner = podOwnerRef; nextOwner != nil; {
		ownerInstance, err := pg.getOwnerInstance(ctx, nextOwner, pod.Namespace)
		if err != nil {
			ownerInstance, err = pg.handleGetOwnerError(ctx, err, pod, nextOwner, lastOwnerInstance)
			return ownerInstance, owners, err
		}
		owners = append(owners, ownerInstance)

		nextOwners := ownerInstance.GetOwnerReferences()
		if len(nextOwners) == 0 {
			logger.V(1).Info("Found final owner", "owner", nextOwner)
			return ownerInstance, owners, nil
		} else if len(nextOwners) > 1 {
			return nil, nil, fmt.Errorf("found %s <%s in namespace %s> with multiple owners: %v",
				nextOwner.Kind, nextOwner.Name, pod.Namespace, nextOwners)
		}

		nextOwner = &nextOwners[0]
		lastOwnerInstance = ownerInstance
	}

	logger.V(1).Info(fmt.Sprintf("encountered a resource with nil owner, returning last owner '%s': <%s/%s>",
		lastOwnerInstance.Kind, lastOwnerInstance.GetNamespace(), lastOwnerInstance.GetName()))
	return lastOwnerInstance, owners, nil
}

func (pg *podGrouper) getOwnerInstance(ctx context.Context, ownerRef *metav1.OwnerReference, namespace string) (
	*metav1.PartialObjectMetadata, error) {
	owner := &metav1.PartialObjectMetadata{}
	groupVersion := strings.Split(ownerRef.APIVersion, "/")
	if len(groupVersion) == 1 {
		groupVersion = []string{"", groupVersion[0]}
	}
	owner.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   groupVersion[0],
		Version: groupVersion[1],
		Kind:    ownerRef.Kind,
	})
	err := pg.clientWithoutCache.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ownerRef.Name}, owner)
	if err != nil {
		return nil, err
	}
	// In some edge cases the owner is already deleted and an identical resource with the same name has been created
	if ownerRef.UID != owner.GetUID() {
		return nil, errors.NewResourceExpired(fmt.Sprintf("Owner resource with uid %s not found", ownerRef.UID))
	}

	return owner, nil
}

func (pg *podGrouper) getPodAsResourceOwner(pod *v1.Pod) (
	*metav1.PartialObjectMetadata, error) {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(pod.GroupVersionKind())
	obj.TypeMeta = pod.TypeMeta
	obj.ObjectMeta = pod.ObjectMeta
	return obj, nil
}

func (pg *podGrouper) getTopOwnerInstance(ctx context.Context, topOwner *metav1.PartialObjectMetadata) (*unstructured.Unstructured, error) {
	topOwnerInstance := &unstructured.Unstructured{}
	topOwnerInstance.SetGroupVersionKind(topOwner.GroupVersionKind())
	key := types.NamespacedName{
		Namespace: topOwner.GetNamespace(),
		Name:      topOwner.GetName(),
	}
	err := pg.client.Get(ctx, key, topOwnerInstance)
	return topOwnerInstance, err
}

func (pg *podGrouper) handleGetOwnerError(
	ctx context.Context, err error, pod *v1.Pod, owner *metav1.OwnerReference, lastOwnerInstance *metav1.PartialObjectMetadata,
) (*metav1.PartialObjectMetadata, error) {
	logger := log.FromContext(ctx)
	if errors.IsForbidden(err) {
		if lastOwnerInstance != nil {
			logger.V(1).Info(fmt.Sprintf("No permissions to get resourceOwner '%s': <%s/%s>. "+
				"returning last owner '%s': <%s/%s>",
				owner.Kind, owner, owner.Name,
				lastOwnerInstance.Kind, lastOwnerInstance.GetNamespace(), lastOwnerInstance.GetName()))
			return lastOwnerInstance, nil
		} else {
			logger.V(1).Info(fmt.Sprintf("No permissions to get resourceOwner '%s': <%s/%s>. "+
				"returning the pod as the owner '%s': <%s/%s>",
				owner.Kind, owner, owner.Name, pod.Kind, pod.Namespace, pod.Name))
			return pg.getPodAsResourceOwner(pod)
		}
	}
	return nil, fmt.Errorf("failed to find %s: <%s/%s>, uid: <%v>: %w",
		owner.Kind, pod.Namespace, owner.Name, owner.UID, err)
}

func (pg *podGrouper) selectPluginForOwners(
	topOwner *unstructured.Unstructured,
	allOwners []*metav1.PartialObjectMetadata,
) grouper.Grouper {
	hub, ok := pg.pluginsHub.(*pluginshub.DefaultPluginsHub)
	if !ok {
		return pg.pluginsHub.GetPodGrouperPlugin(metav1.GroupVersionKind{})
	}

	defaultPlugin := hub.GetDefaultPlugin()

	// Build ownerGVKs in top-down order
	ownerGVKs := make([]metav1.GroupVersionKind, 0, len(allOwners)+1)
	ownerGVKs = append(ownerGVKs, metav1.GroupVersionKind(topOwner.GroupVersionKind()))

	for i := len(allOwners) - 1; i >= 0; i-- {
		ownerGVKs = append(ownerGVKs, metav1.GroupVersionKind(allOwners[i].GroupVersionKind()))
	}

	for _, gvk := range ownerGVKs {
		plugin := pg.pluginsHub.GetPodGrouperPlugin(gvk)
		if plugin != defaultPlugin {
			return plugin
		}
	}
	return defaultPlugin
}
