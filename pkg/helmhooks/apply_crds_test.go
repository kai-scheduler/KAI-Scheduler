// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/deployments/kai-scheduler/crds"
)

const topologyCRDName = "topologies.kai.scheduler"

func crdScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	return scheme
}

func TestApplyCRDs_CreatesAllEmbeddedCRDs(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(crdScheme(t)).Build()

	require.NoError(t, ApplyCRDs(ctx, c))

	expected, err := crds.LoadEmbeddedCRDs()
	require.NoError(t, err)
	for _, want := range expected {
		got := &apiextensionsv1.CustomResourceDefinition{}
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: want.Name}, got), want.Name)
		assert.Equal(t, want.Spec.Group, got.Spec.Group)
		assert.Equal(t, want.Spec.Names, got.Spec.Names)
	}
}

func TestApplyCRDs_TakesOverConflictingFields(t *testing.T) {
	ctx := context.Background()
	stale := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: topologyCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "kai.scheduler",
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: "topologies", Kind: "Topology", Singular: "stale"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(crdScheme(t)).WithObjects(stale).Build()

	require.NoError(t, ApplyCRDs(ctx, c))

	got := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: topologyCRDName}, got))
	assert.Equal(t, "topology", got.Spec.Names.Singular)
	assert.Equal(t, apiextensionsv1.ClusterScoped, got.Spec.Scope)
	assert.NotEmpty(t, got.Spec.Versions)
}
