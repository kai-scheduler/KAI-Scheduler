// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

const releaseNamespace = "kai-scheduler"

func cleanupScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, kaiv1.AddToScheme(scheme))
	return scheme
}

func deployment(namespace, name string, managed bool) *appsv1.Deployment {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
	if managed {
		d.Labels = map[string]string{common.OperatorManagedByLabelKey: common.OperatorManagedByLabelValue}
	}
	return d
}

func TestCleanup_DeletesManagedDeploymentsAndConfig(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(cleanupScheme(t)).WithObjects(
		deployment(releaseNamespace, "scheduler", true),
		deployment(releaseNamespace, "binder", true),
		deployment(releaseNamespace, "operator", false),
		deployment("other", "scheduler", true),
		&kaiv1.Config{ObjectMeta: metav1.ObjectMeta{Name: "kai-config"}},
	).Build()

	require.NoError(t, Cleanup(ctx, c, releaseNamespace, "kai-config"))

	remaining := &appsv1.DeploymentList{}
	require.NoError(t, c.List(ctx, remaining))
	var names []string
	for _, d := range remaining.Items {
		names = append(names, d.Namespace+"/"+d.Name)
	}
	assert.ElementsMatch(t, []string{releaseNamespace + "/operator", "other/scheduler"}, names)

	err := c.Get(ctx, client.ObjectKey{Name: "kai-config"}, &kaiv1.Config{})
	assert.True(t, apierrors.IsNotFound(err))
}

func TestCleanup_KeepsConfigWhenNameEmpty(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(cleanupScheme(t)).WithObjects(
		deployment(releaseNamespace, "scheduler", true),
		&kaiv1.Config{ObjectMeta: metav1.ObjectMeta{Name: "kai-config"}},
	).Build()

	require.NoError(t, Cleanup(ctx, c, releaseNamespace, ""))

	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "kai-config"}, &kaiv1.Config{}))
	err := c.Get(ctx, client.ObjectKey{Namespace: releaseNamespace, Name: "scheduler"}, &appsv1.Deployment{})
	assert.True(t, apierrors.IsNotFound(err))
}

func TestCleanup_ToleratesMissingConfig(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(cleanupScheme(t)).Build()
	require.NoError(t, Cleanup(context.Background(), c, releaseNamespace, "kai-config"))
}

func TestCleanup_WaitsForFinalizers(t *testing.T) {
	deletionPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { deletionPollInterval = time.Second })

	ctx := context.Background()
	blocked := deployment(releaseNamespace, "scheduler", true)
	blocked.Finalizers = []string{"test.kai.scheduler/hold"}
	c := fake.NewClientBuilder().WithScheme(cleanupScheme(t)).WithObjects(blocked).Build()

	go func() {
		time.Sleep(50 * time.Millisecond)
		current := &appsv1.Deployment{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(blocked), current); err != nil {
			return
		}
		current.Finalizers = nil
		_ = c.Update(ctx, current)
	}()

	start := time.Now()
	require.NoError(t, Cleanup(ctx, c, releaseNamespace, ""))
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
	err := c.Get(ctx, client.ObjectKeyFromObject(blocked), &appsv1.Deployment{})
	assert.True(t, apierrors.IsNotFound(err))
}

func TestCleanup_GivesUpWhenContextEnds(t *testing.T) {
	deletionPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { deletionPollInterval = time.Second })

	blocked := deployment(releaseNamespace, "scheduler", true)
	blocked.Finalizers = []string{"test.kai.scheduler/hold"}
	c := fake.NewClientBuilder().WithScheme(cleanupScheme(t)).WithObjects(blocked).Build()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := Cleanup(ctx, c, releaseNamespace, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler")
}
