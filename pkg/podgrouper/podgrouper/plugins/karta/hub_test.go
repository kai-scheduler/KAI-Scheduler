// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package karta

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
)

func TestKartaHub_GetPodGrouperPluginNoKartaFound(t *testing.T) {
	kubeClient := newFakeClientWithScheme()
	defaultPlugin := namedGrouper("Default Grouper")
	plugin := NewKartaHub(kubeClient, defaultPlugin)

	result := plugin.GetPodGrouperPlugin(createTestGVK())

	assert.Nil(t, result)
}

func TestKartaHub_GetPodGrouperPluginKartaCRDMissingUsesRetryTTL(t *testing.T) {
	var listCalls int
	kubeClient := newFakeClientWithSchemeAndInterceptor(interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			listCalls++
			return &apimeta.NoKindMatchError{
				GroupKind:        schema.GroupKind{Group: "run.ai", Kind: "Karta"},
				SearchedVersions: []string{"v1alpha1"},
			}
		},
	})
	defaultPlugin := namedGrouper("Default Grouper")
	plugin := NewKartaHub(kubeClient, defaultPlugin)

	gvk := createTestGVK()
	firstResult := plugin.GetPodGrouperPlugin(gvk)
	secondResult := plugin.GetPodGrouperPlugin(gvk)

	assert.Nil(t, firstResult)
	assert.Nil(t, secondResult)
	assert.Equal(t, 1, listCalls)
	assert.True(t, plugin.isKartaCrdMissing.load())

	plugin.isKartaCrdMissing.retryAfterUnixNano.Store(time.Now().Add(-time.Second).UnixNano())
	thirdResult := plugin.GetPodGrouperPlugin(gvk)

	assert.Nil(t, thirdResult)
	assert.Equal(t, 2, listCalls)
	assert.True(t, plugin.isKartaCrdMissing.load())
}

func TestKartaHub_GetPodGrouperPluginKartaFound(t *testing.T) {
	gvk := createTestGVK()
	kt := createTestKarta(gvk, false)
	kubeClient := newFakeClientWithScheme(kt)
	defaultPlugin := namedGrouper("Default Grouper")
	plugin := NewKartaHub(kubeClient, defaultPlugin)

	result := plugin.GetPodGrouperPlugin(gvk)

	assert.NotEqual(t, defaultPlugin, result)
	assert.IsType(t, &KartaGrouper{}, result)
}

func TestKartaHub_GetPodGrouperPluginMultipleKartasForGvk(t *testing.T) {
	gvk := createTestGVK()
	firstKarta := createTestKartaWithNameAndUID(gvk, "first-karta", types.UID("first-karta-uid"))
	secondKarta := createTestKartaWithNameAndUID(gvk, "second-karta", types.UID("second-karta-uid"))
	kubeClient := newFakeClientWithScheme(firstKarta, secondKarta)
	defaultPlugin := namedGrouper("Default Grouper")
	plugin := NewKartaHub(kubeClient, defaultPlugin)

	result := plugin.GetPodGrouperPlugin(gvk)

	assert.Nil(t, result)
}

func TestKartaHub_GetPodGrouperPluginKartaFoundIsNotCached(t *testing.T) {
	var listCalls int
	gvk := createTestGVK()
	kt := createTestKarta(gvk, false)
	kubeClient := newFakeClientWithSchemeAndInterceptor(interceptor.Funcs{
		List: func(ctx context.Context, kubeClient client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			listCalls++
			return kubeClient.List(ctx, list, opts...)
		},
	}, kt)
	defaultPlugin := namedGrouper("Default Grouper")
	plugin := NewKartaHub(kubeClient, defaultPlugin)

	firstResult := plugin.GetPodGrouperPlugin(gvk)
	secondResult := plugin.GetPodGrouperPlugin(gvk)

	assert.IsType(t, &KartaGrouper{}, firstResult)
	assert.IsType(t, &KartaGrouper{}, secondResult)
	assert.Equal(t, 2, listCalls)
}

func TestGetKartaForGvkWithoutGangSchedulingInstructions(t *testing.T) {
	gvk := createTestGVK()
	kt := createTestKarta(gvk, false)
	kt.Spec.Instructions.GangScheduling = nil
	kubeClient := newFakeClientWithScheme(kt)

	result, err := getKartaForGvk(context.Background(), kubeClient, gvk)

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetKartaForGvkInvalidKarta(t *testing.T) {
	gvk := createTestGVK()
	kt := createTestKarta(gvk, false)
	setKartaComponentTypeKeyPath(t, kt, ".. | .metadata.name")
	kubeClient := newFakeClientWithScheme(kt)

	result, err := getKartaForGvk(context.Background(), kubeClient, gvk)

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetKartaForGvkInvalidKartaDoesNotBlockValidKarta(t *testing.T) {
	gvk := createTestGVK()
	invalidKarta := createTestKartaWithNameAndUID(gvk, "invalid-karta", types.UID("invalid-karta-uid"))
	setKartaComponentTypeKeyPath(t, invalidKarta, ".. | .metadata.name")
	validKarta := createTestKartaWithNameAndUID(gvk, "valid-karta", types.UID("valid-karta-uid"))
	kubeClient := newFakeClientWithScheme(invalidKarta, validKarta)

	result, err := getKartaForGvk(context.Background(), kubeClient, gvk)

	assert.NoError(t, err)
	assert.Equal(t, validKarta.Name, result.Name)
}

func TestGetKartaForGvkMultipleKartasWithGangSchedulingInstructions(t *testing.T) {
	gvk := createTestGVK()
	firstKarta := createTestKartaWithNameAndUID(gvk, "first-karta", types.UID("first-karta-uid"))
	secondKarta := createTestKartaWithNameAndUID(gvk, "second-karta", types.UID("second-karta-uid"))
	kubeClient := newFakeClientWithScheme(firstKarta, secondKarta)

	result, err := getKartaForGvk(context.Background(), kubeClient, gvk)

	assert.Nil(t, result)
	assert.ErrorContains(t, err, "found multiple Kartas with gang scheduling instructions")
}

func TestGetKartaForGvkWithDeletionTimestamp(t *testing.T) {
	gvk := createTestGVK()
	kt := createTestKarta(gvk, true)
	kubeClient := newFakeClientWithScheme(kt)

	result, err := getKartaForGvk(context.Background(), kubeClient, gvk)

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetKartaForGvkKartaFoundIsNotCached(t *testing.T) {
	var listCalls int
	gvk := createTestGVK()
	kt := createTestKarta(gvk, false)
	kubeClient := newFakeClientWithSchemeAndInterceptor(interceptor.Funcs{
		List: func(ctx context.Context, kubeClient client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			listCalls++
			return kubeClient.List(ctx, list, opts...)
		},
	}, kt)

	firstResult, firstErr := getKartaForGvk(context.Background(), kubeClient, gvk)
	secondResult, secondErr := getKartaForGvk(context.Background(), kubeClient, gvk)

	assert.NoError(t, firstErr)
	assert.NoError(t, secondErr)
	assert.Equal(t, kt.Name, firstResult.Name)
	assert.Equal(t, kt.Name, secondResult.Name)
	assert.Equal(t, 2, listCalls)
}

type namedGrouper string

func (g namedGrouper) Name() string {
	return string(g)
}

func (g namedGrouper) GetPodGroupMetadata(_ *unstructured.Unstructured, _ *v1.Pod, _ ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	return nil, nil
}
