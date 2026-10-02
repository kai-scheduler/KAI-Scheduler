// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

var deletionPollInterval = time.Second

// Cleanup deletes the operator-managed Deployments in namespace and, when configName is set, the
// Config object of that name. Missing objects are not errors. Like `kubectl delete`, it returns
// only once the objects are gone, so finalizers cannot leave them behind for the next install.
func Cleanup(ctx context.Context, c client.Client, namespace, configName string) error {
	logger := logf.FromContext(ctx)

	deployments := &appsv1.DeploymentList{}
	if err := c.List(ctx, deployments, client.InNamespace(namespace),
		client.MatchingLabels{common.OperatorManagedByLabelKey: common.OperatorManagedByLabelValue}); err != nil {
		return fmt.Errorf("failed to list operator-managed deployments in %s: %w", namespace, err)
	}
	for i := range deployments.Items {
		deployment := &deployments.Items[i]
		if err := deleteAndWait(ctx, c, deployment); err != nil {
			return fmt.Errorf("failed to delete deployment %s/%s: %w", namespace, deployment.Name, err)
		}
		logger.Info("Deleted deployment", "namespace", namespace, "name", deployment.Name)
	}

	if configName == "" {
		return nil
	}
	config := &kaiv1.Config{ObjectMeta: metav1.ObjectMeta{Name: configName}}
	if err := deleteAndWait(ctx, c, config); err != nil {
		return fmt.Errorf("failed to delete Config %s: %w", configName, err)
	}
	logger.Info("Deleted Config", "name", configName)
	return nil
}

func deleteAndWait(ctx context.Context, c client.Client, obj client.Object) error {
	if err := c.Delete(ctx, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	key := client.ObjectKeyFromObject(obj)
	uid := obj.GetUID()
	return wait.PollUntilContextCancel(ctx, deletionPollInterval, true, func(ctx context.Context) (bool, error) {
		current := obj.DeepCopyObject().(client.Object)
		err := c.Get(ctx, key, current)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		// A different UID under the same name means the deleted object is already gone.
		return uid != "" && current.GetUID() != uid, nil
	})
}
