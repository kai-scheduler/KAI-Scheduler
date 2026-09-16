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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
)

const kueueStorageVersion = "v1beta1"

func topologyScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	require.NoError(t, kaiv1alpha1.AddToScheme(scheme))
	return scheme
}

func kueueTopologyCRD(versions ...apiextensionsv1.CustomResourceDefinitionVersion) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: kueueTopologyCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group:    kueueGroup,
			Scope:    apiextensionsv1.ClusterScoped,
			Names:    apiextensionsv1.CustomResourceDefinitionNames{Plural: "topologies", Kind: "Topology"},
			Versions: versions,
		},
	}
}

func servedKueueVersions() []apiextensionsv1.CustomResourceDefinitionVersion {
	return []apiextensionsv1.CustomResourceDefinitionVersion{
		{Name: "v1alpha1", Served: true},
		{Name: kueueStorageVersion, Served: true, Storage: true},
	}
}

func kueueTopology(name string, nodeLabels ...string) *unstructured.Unstructured {
	levels := make([]interface{}, 0, len(nodeLabels))
	for _, label := range nodeLabels {
		levels = append(levels, map[string]interface{}{"nodeLabel": label})
	}
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(kueueGroup + "/" + kueueStorageVersion)
	obj.SetKind("Topology")
	obj.SetName(name)
	if err := unstructured.SetNestedSlice(obj.Object, levels, "spec", "levels"); err != nil {
		panic(err)
	}
	return obj
}

func TestMigrateTopologies_CopiesMissingTopologies(t *testing.T) {
	ctx := context.Background()
	existing := &kaiv1alpha1.Topology{
		ObjectMeta: metav1.ObjectMeta{Name: "existing"},
		Spec:       kaiv1alpha1.TopologySpec{Levels: []kaiv1alpha1.TopologyLevel{{NodeLabel: "kai/zone"}}},
	}
	c := fake.NewClientBuilder().WithScheme(topologyScheme(t)).
		WithObjects(kueueTopologyCRD(servedKueueVersions()...), existing).
		WithObjects(
			kueueTopology("rack-block", "cloud/block", "cloud/rack"),
			kueueTopology("existing", "kueue/zone"),
		).
		Build()

	require.NoError(t, MigrateTopologies(ctx, c))

	migrated := &kaiv1alpha1.Topology{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "rack-block"}, migrated))
	assert.Equal(t, kueueGroup, migrated.Annotations[migratedFromAnnotation])
	assert.Equal(t, []kaiv1alpha1.TopologyLevel{{NodeLabel: "cloud/block"}, {NodeLabel: "cloud/rack"}}, migrated.Spec.Levels)

	untouched := &kaiv1alpha1.Topology{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "existing"}, untouched))
	assert.Equal(t, existing.Spec.Levels, untouched.Spec.Levels)
	assert.NotContains(t, untouched.Annotations, migratedFromAnnotation)
}

func TestMigrateTopologies_SkipsWhenKueueCRDMissing(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(topologyScheme(t)).Build()

	require.NoError(t, MigrateTopologies(ctx, c))

	topologies := &kaiv1alpha1.TopologyList{}
	require.NoError(t, c.List(ctx, topologies))
	assert.Empty(t, topologies.Items)
}

func TestMigrateTopologies_NoKueueTopologies(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(topologyScheme(t)).
		WithObjects(kueueTopologyCRD(servedKueueVersions()...)).Build()

	require.NoError(t, MigrateTopologies(ctx, c))

	topologies := &kaiv1alpha1.TopologyList{}
	require.NoError(t, c.List(ctx, topologies))
	assert.Empty(t, topologies.Items)
}

func TestMigrateTopologies_FailsWithoutStorageVersion(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(topologyScheme(t)).
		WithObjects(kueueTopologyCRD(apiextensionsv1.CustomResourceDefinitionVersion{Name: "v1alpha1", Served: true})).
		Build()

	err := MigrateTopologies(context.Background(), c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage version")
}

func TestKaiTopologyFromKueue_RejectsLevelWithoutNodeLabel(t *testing.T) {
	obj := kueueTopology("broken", "cloud/block")
	levels, _, err := unstructured.NestedSlice(obj.Object, "spec", "levels")
	require.NoError(t, err)
	levels = append(levels, map[string]interface{}{"alias": "no-label"})
	require.NoError(t, unstructured.SetNestedSlice(obj.Object, levels, "spec", "levels"))

	_, err = kaiTopologyFromKueue(obj)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nodeLabel")
}
