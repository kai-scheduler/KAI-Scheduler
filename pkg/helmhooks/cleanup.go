// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

// Cleanup deletes the operator-managed Deployments in namespace and, when configName is set, the
// Config object of that name. Missing objects are not errors.
func Cleanup(ctx context.Context, c client.Client, namespace, configName string) error {
	logger := logf.FromContext(ctx)

	deployments := &appsv1.DeploymentList{}
	if err := c.List(ctx, deployments, client.InNamespace(namespace),
		client.MatchingLabels{common.OperatorManagedByLabelKey: common.OperatorManagedByLabelValue}); err != nil {
		return fmt.Errorf("failed to list operator-managed deployments in %s: %w", namespace, err)
	}
	for i := range deployments.Items {
		deployment := &deployments.Items[i]
		if err := c.Delete(ctx, deployment); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete deployment %s/%s: %w", namespace, deployment.Name, err)
		}
		logger.Info("Deleted deployment", "namespace", namespace, "name", deployment.Name)
	}

	if configName == "" {
		return nil
	}
	config := &kaiv1.Config{ObjectMeta: metav1.ObjectMeta{Name: configName}}
	if err := c.Delete(ctx, config); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete Config %s: %w", configName, err)
	}
	logger.Info("Deleted Config", "name", configName)
	return nil
}
