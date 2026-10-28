// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/hamicore"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestBinderClientConfigMapFreshness(t *testing.T) {
	testEnv := &envtest.Environment{}
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	apiClient, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "binder-configmap-"}}
	require.NoError(t, apiClient.Create(t.Context(), namespace))

	t.Run("reads its writes while the cache is missing or stale", func(t *testing.T) {
		cacheReader := fake.NewClientBuilder().WithScheme(scheme).Build()
		binderClient, err := newClientWithWatch(config, cacheReader)
		require.NoError(t, err)
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: namespace.Name},
			Data:       map[string]string{"value": "created"},
		}
		require.NoError(t, binderClient.Create(t.Context(), configMap))
		key := client.ObjectKeyFromObject(configMap)
		require.True(t, apierrors.IsNotFound(cacheReader.Get(t.Context(), key, &corev1.ConfigMap{})))
		observed := &corev1.ConfigMap{}
		require.NoError(t, binderClient.Get(t.Context(), key, observed))
		require.Equal(t, "created", observed.Data["value"])

		staleCache := fake.NewClientBuilder().WithScheme(scheme).WithObjects(observed.DeepCopy()).Build()
		binderClient, err = newClientWithWatch(config, staleCache)
		require.NoError(t, err)
		before := observed.DeepCopy()
		observed.Data["value"] = "patched"
		require.NoError(t, binderClient.Patch(t.Context(), observed, client.MergeFrom(before)))
		require.NoError(t, staleCache.Get(t.Context(), key, &corev1.ConfigMap{}))
		require.NoError(t, binderClient.Get(t.Context(), key, observed))
		require.Equal(t, "patched", observed.Data["value"])
		configMaps := &corev1.ConfigMapList{}
		require.NoError(t, binderClient.List(t.Context(), configMaps, client.InNamespace(namespace.Name)))
		require.Len(t, configMaps.Items, 1)
		require.Equal(t, "patched", configMaps.Items[0].Data["value"])
	})

	t.Run("preserves cached Pod and BindRequest reads", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "cached-pod", Namespace: namespace.Name}}
		bindRequest := &schedulingv1alpha2.BindRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "cached-request", Namespace: namespace.Name},
		}
		cacheReader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, bindRequest).Build()
		binderClient, err := newClientWithWatch(config, cacheReader)
		require.NoError(t, err)
		require.True(t, apierrors.IsNotFound(apiClient.Get(t.Context(), client.ObjectKeyFromObject(pod), &corev1.Pod{})))
		require.NoError(t, binderClient.Get(t.Context(), client.ObjectKeyFromObject(pod), &corev1.Pod{}))
		require.NoError(t, binderClient.Get(t.Context(), client.ObjectKeyFromObject(bindRequest), &schedulingv1alpha2.BindRequest{}))
		pods := &corev1.PodList{}
		require.NoError(t, binderClient.List(t.Context(), pods, client.InNamespace(namespace.Name)))
		require.Len(t, pods.Items, 1)
		bindRequests := &schedulingv1alpha2.BindRequestList{}
		require.NoError(t, binderClient.List(t.Context(), bindRequests, client.InNamespace(namespace.Name)))
		require.Len(t, bindRequests.Items, 1)
	})

	t.Run("returns API errors despite a cached ConfigMap", func(t *testing.T) {
		cachedConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "deleted", Namespace: namespace.Name},
			Data:       map[string]string{"value": "stale"},
		}
		cacheReader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cachedConfigMap).Build()
		binderClient, err := newClientWithWatch(config, cacheReader)
		require.NoError(t, err)
		err = binderClient.Get(t.Context(), client.ObjectKeyFromObject(cachedConfigMap), &corev1.ConfigMap{})
		require.True(t, apierrors.IsNotFound(err), "expected API NotFound, got %v", err)
	})

	for _, mode := range []struct {
		name      string
		modernEnv bool
		nri       bool
	}{
		{name: "modern", modernEnv: true},
		{name: "legacy"},
		{name: "nri", nri: true},
	} {
		t.Run("GPUSharing and HamiCore with missing cache/"+mode.name, func(t *testing.T) {
			cacheReader := fake.NewClientBuilder().WithScheme(scheme).Build()
			binderClient, err := newClientWithWatch(config, cacheReader)
			require.NoError(t, err)
			configMapName := mode.name + "-shared-gpu-0"
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: mode.name, Namespace: namespace.Name,
					Annotations: map[string]string{
						constants.GpuSharingConfigMapAnnotation: mode.name + "-shared-gpu",
						constants.GpuFractionContainerName:      "gpu-container",
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "gpu-container", Image: "test-image",
					EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: configMapName + "-evar"},
					}}},
				}}},
			}
			if mode.modernEnv {
				common.AddGPUSharingEnvVars(&pod.Spec.Containers[0], configMapName)
			}
			require.NoError(t, apiClient.Create(t.Context(), pod))
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{constants.NvidiaGpuMemory: "2048"},
			}}
			bindRequest := &schedulingv1alpha2.BindRequest{Spec: schedulingv1alpha2.BindRequestSpec{
				ReceivedResourceType: common.ReceivedTypeFraction,
				ReceivedGPU:          &schedulingv1alpha2.ReceivedGPU{Portion: "0.5"},
			}}
			bindingState := &state.BindingState{ReservedGPUIds: []string{"GPU-0"}}
			err = gpusharing.New(binderClient, false, mode.nri).PreBind(t.Context(), pod, node, bindRequest, bindingState)
			require.NoError(t, err)
			require.NoError(t, hamicore.New(binderClient).PreBind(t.Context(), pod, node, bindRequest, bindingState))

			capabilities := &corev1.ConfigMap{}
			key := client.ObjectKey{Namespace: namespace.Name, Name: configMapName}
			require.True(t, apierrors.IsNotFound(cacheReader.Get(t.Context(), key, &corev1.ConfigMap{})))
			require.NoError(t, apiClient.Get(t.Context(), key, capabilities))
			require.Equal(t, "0.5", capabilities.Data[common.GPUPortion])
			require.Equal(t, "0.5", capabilities.Data[common.NumOfGpusEnvVarBC])
			require.Equal(t, "1024m", capabilities.Data[common.CudaDeviceMemoryLimit])
			require.Len(t, capabilities.OwnerReferences, 1)
			require.Equal(t, pod.UID, capabilities.OwnerReferences[0].UID)
			directEnv := &corev1.ConfigMap{}
			key.Name += "-evar"
			require.NoError(t, apiClient.Get(t.Context(), key, directEnv))
			require.Len(t, directEnv.OwnerReferences, 1)
			require.Equal(t, pod.UID, directEnv.OwnerReferences[0].UID)
			if mode.nri {
				require.Equal(t, "GPU-0", bindingState.BindingPodAnnotations["nvidia.cdi.k8s.io/container.gpu-container"])
				require.NotContains(t, capabilities.Data, constants.NvidiaVisibleDevices)
				require.NotContains(t, directEnv.Data, constants.NvidiaVisibleDevices)
			} else if mode.modernEnv {
				require.Equal(t, "GPU-0", capabilities.Data[constants.NvidiaVisibleDevices])
			} else {
				require.Equal(t, "GPU-0", directEnv.Data[constants.NvidiaVisibleDevices])
			}
		})
	}
}
