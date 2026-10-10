// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
)

func TestConfigMapCacheUpgrade(t *testing.T) {
	testEnv := &envtest.Environment{}
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	apiClient, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	for _, namespace := range []string{"workload", "unrelated"} {
		require.NoError(t, apiClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "workload"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "busybox"}}},
	}
	require.NoError(t, apiClient.Create(ctx, pod))
	legacy := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: pod.Namespace, Labels: map[string]string{"existing": "label"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID}}},
		Data:       map[string]string{"preserved": "old-value"},
		BinaryData: map[string][]byte{"binary": []byte("binary-value")},
	}
	untouched := legacy.DeepCopy()
	untouched.Name = "untouched"
	unrelated := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "large", Namespace: "unrelated"},
		Data:       map[string]string{"payload": strings.Repeat("x", 1024*1024)},
	}
	for _, cm := range []*corev1.ConfigMap{legacy, untouched, unrelated} {
		require.NoError(t, apiClient.Create(ctx, cm))
	}

	t.Log("Before upgrade: the unfiltered cache retains all three test ConfigMaps, including the unrelated payload")
	oldManager, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme, Metrics: server.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)
	stopOldManager := startConfigMapCache(t, ctx, oldManager)
	for _, cm := range []*corev1.ConfigMap{legacy, untouched, unrelated} {
		stored := &corev1.ConfigMap{}
		require.NoError(t, oldManager.GetCache().Get(ctx, client.ObjectKeyFromObject(cm), stored))
		require.Equal(t, cm.Data, stored.Data)
	}
	stopOldManager()

	t.Log("After upgrade: legacy maps are absent from the cache but remain readable through both binder clients")
	options := &Options{MetricsAddr: "0", ProbeAddr: "0", QPS: 50, Burst: 100}
	app, err := New(options, config)
	require.NoError(t, err)
	stopNewManager := startConfigMapCache(t, ctx, app.manager)
	for _, cm := range []*corev1.ConfigMap{legacy, untouched, unrelated} {
		err := app.manager.GetCache().Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})
		require.True(t, errors.IsNotFound(err), "unexpected cache result: %v", err)
	}

	for _, kubeClient := range []client.Client{app.Client, app.manager.GetClient()} {
		stored := &corev1.ConfigMap{}
		require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(legacy), stored))
		require.Equal(t, legacy.Data, stored.Data)
	}
	t.Log("An environment update succeeds before migration without adding the cache label")
	require.NoError(t, common.UpdateConfigMapEnvironmentVariable(ctx, app.Client, pod, legacy.Name, func(data map[string]string) error {
		data["CUDA_VISIBLE_DEVICES"] = "0"
		return nil
	}))
	updatedLegacy := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(legacy), updatedLegacy))
	require.Equal(t, "0", updatedLegacy.Data["CUDA_VISIBLE_DEVICES"])
	require.NotContains(t, updatedLegacy.Labels, gpusharingconfigmap.GPUSharingConfigMapLabel)
	stillUncached := &corev1.ConfigMap{}
	require.True(t, errors.IsNotFound(app.manager.GetCache().Get(ctx, client.ObjectKeyFromObject(legacy), stillUncached)))

	t.Log("Upsert labels the existing map in place and the watch adds it to the filtered cache")
	require.NoError(t, gpusharingconfigmap.UpsertJobConfigMap(ctx, app.Client, pod, legacy.Name, map[string]string{"added": "new-value"}))
	require.NoError(t, gpusharingconfigmap.UpsertJobConfigMap(ctx, app.Client, pod, "new", map[string]string{"value": "new-map"}))
	require.Eventually(t, func() bool {
		list := &corev1.ConfigMapList{}
		if err := app.manager.GetCache().List(ctx, list); err != nil || len(list.Items) != 2 {
			return false
		}
		for _, cm := range list.Items {
			if cm.Namespace != pod.Namespace || cm.Labels[gpusharingconfigmap.GPUSharingConfigMapLabel] != "true" {
				return false
			}
		}
		return true
	}, 10*time.Second, 50*time.Millisecond)
	stored := &corev1.ConfigMap{}
	require.NoError(t, app.Client.Get(ctx, client.ObjectKeyFromObject(legacy), stored))
	require.Equal(t, legacy.UID, stored.UID)
	require.Equal(t, legacy.OwnerReferences, stored.OwnerReferences)
	require.Equal(t, legacy.BinaryData, stored.BinaryData)
	require.Equal(t, map[string]string{"preserved": "old-value", "CUDA_VISIBLE_DEVICES": "0", "added": "new-value"}, stored.Data)
	require.Equal(t, "label", stored.Labels["existing"])
	storedUnrelated := &corev1.ConfigMap{}
	err = app.manager.GetCache().Get(ctx, client.ObjectKeyFromObject(unrelated), storedUnrelated)
	require.True(t, errors.IsNotFound(err), "unexpected cache result: %v", err)

	t.Log("After restart: labeled maps are cached; the untouched legacy map remains unlabeled and readable")
	stopNewManager()
	restarted, err := New(options, config)
	require.NoError(t, err)
	startConfigMapCache(t, ctx, restarted.manager)
	list := &corev1.ConfigMapList{}
	require.NoError(t, restarted.manager.GetCache().List(ctx, list))
	require.Len(t, list.Items, 2)
	for _, cm := range []*corev1.ConfigMap{untouched, unrelated} {
		err := restarted.manager.GetCache().Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})
		require.True(t, errors.IsNotFound(err), "unexpected cache result: %v", err)
	}
	for _, kubeClient := range []client.Client{restarted.Client, restarted.manager.GetClient()} {
		unmigrated := &corev1.ConfigMap{}
		require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(untouched), unmigrated))
		require.Equal(t, untouched.Data, unmigrated.Data)
		require.NotContains(t, unmigrated.Labels, gpusharingconfigmap.GPUSharingConfigMapLabel)
	}
	cachedLegacy := &corev1.ConfigMap{}
	require.NoError(t, restarted.manager.GetCache().Get(ctx, client.ObjectKeyFromObject(legacy), cachedLegacy))
	require.Equal(t, stored.Data, cachedLegacy.Data)
}

func startConfigMapCache(t *testing.T, ctx context.Context, mgr manager.Manager) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	_, err := mgr.GetCache().GetInformer(ctx, &corev1.ConfigMap{})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	var once sync.Once
	stop := func() {
		t.Helper()
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Error("manager did not stop")
			}
		})
	}
	t.Cleanup(stop)
	require.True(t, mgr.GetCache().WaitForCacheSync(ctx))
	return stop
}
