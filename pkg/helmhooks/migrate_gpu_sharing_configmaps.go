// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
)

const gpuSharingLabelValue = "true"

// MigrateGPUSharingConfigMaps labels Pod-owned GPU-sharing maps without changing their contents or identity.
func MigrateGPUSharingConfigMaps(ctx context.Context, c client.Client) error {
	pods := &metav1.PartialObjectMetadataList{}
	pods.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PodList"))
	if err := c.List(ctx, pods); err != nil {
		return fmt.Errorf("list GPU-sharing pods: %w", err)
	}
	prefixes := make(map[types.UID]client.ObjectKey)
	for _, pod := range pods.Items {
		if prefix := pod.Annotations["runai/shared-gpu-configmap"]; prefix != "" {
			prefixes[pod.UID] = client.ObjectKey{Namespace: pod.Namespace, Name: prefix}
		}
	}
	if len(prefixes) == 0 {
		return nil
	}
	// Metadata-only requests keep unrelated ConfigMap payloads out of the migration job.
	maps := &metav1.PartialObjectMetadataList{}
	maps.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMapList"))
	if err := c.List(ctx, maps); err != nil {
		return fmt.Errorf("list GPU-sharing ConfigMaps: %w", err)
	}
	for i := range maps.Items {
		cm := &maps.Items[i]
		cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
		if !isGPUSharingConfigMap(cm, prefixes) || cm.Labels[gpusharingconfigmap.GPUSharingConfigMapLabel] == gpuSharingLabelValue {
			continue
		}
		uid := cm.UID
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current := &metav1.PartialObjectMetadata{}
			current.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
			if err := c.Get(ctx, client.ObjectKeyFromObject(cm), current); err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			if current.UID != uid || !isGPUSharingConfigMap(current, prefixes) || current.Labels[gpusharingconfigmap.GPUSharingConfigMapLabel] == gpuSharingLabelValue {
				return nil
			}
			before := current.DeepCopy()
			if current.Labels == nil {
				current.Labels = map[string]string{}
			}
			current.Labels[gpusharingconfigmap.GPUSharingConfigMapLabel] = gpuSharingLabelValue
			if err := c.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); !apierrors.IsNotFound(err) {
				return err
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("label ConfigMap %s: %w", client.ObjectKeyFromObject(cm), err)
		}
		logf.FromContext(ctx).Info("Migrated GPU-sharing ConfigMap", "namespace", cm.Namespace, "name", cm.Name)
	}
	return nil
}

func isGPUSharingConfigMap(cm *metav1.PartialObjectMetadata, prefixes map[types.UID]client.ObjectKey) bool {
	for _, owner := range cm.OwnerReferences {
		prefix, found := prefixes[owner.UID]
		if !found || owner.APIVersion != "v1" || owner.Kind != "Pod" || cm.Namespace != prefix.Namespace {
			continue
		}
		suffix, found := strings.CutPrefix(cm.Name, prefix.Name+"-")
		if !found {
			continue
		}
		suffix = strings.TrimSuffix(suffix, "-evar")
		index, err := strconv.Atoi(suffix)
		if err == nil && index >= 0 && strconv.Itoa(index) == suffix {
			return true
		}
	}
	return false
}

// WaitForBinderUpgrade fences the final migration scan behind the disappearance of old binder writers.
// An empty image waits for a disabled binder to be removed.
func WaitForBinderUpgrade(ctx context.Context, c client.Client, namespace, image string) error {
	return wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		deployment := &appsv1.Deployment{}
		err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "binder"}, deployment)
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		if image != "" {
			if apierrors.IsNotFound(err) || len(deployment.Spec.Template.Spec.Containers) == 0 || deployment.Spec.Template.Spec.Containers[0].Image != image {
				return false, nil
			}
			desired := int32(1)
			if deployment.Spec.Replicas != nil {
				desired = *deployment.Spec.Replicas
			}
			if deployment.Status.ObservedGeneration < deployment.Generation || deployment.Status.Replicas != desired || deployment.Status.UpdatedReplicas != desired || deployment.Status.AvailableReplicas != desired {
				return false, nil
			}
		} else if !apierrors.IsNotFound(err) {
			return false, nil
		}
		pods := &corev1.PodList{}
		if err := c.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels{"app": "binder"}); err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if image == "" || len(pod.Spec.Containers) == 0 || pod.Spec.Containers[0].Image != image {
				return false, nil
			}
		}
		return true, nil
	})
}
