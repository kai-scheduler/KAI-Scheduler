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
	"github.com/kai-scheduler/KAI-scheduler/pkg/helmhooks"
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
	require.NoError(t, apiClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload"}}))
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "workload", Annotations: map[string]string{"runai/shared-gpu-configmap": "worker-shared-gpu"}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "busybox"}}}}
	require.NoError(t, apiClient.Create(ctx, pod))
	legacy := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "worker-shared-gpu-0", Namespace: pod.Namespace, Labels: map[string]string{"existing": "label"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID}}},
		Data: map[string]string{"preserved": "old-value"}, BinaryData: map[string][]byte{"binary": []byte("binary-value")}}
	unrelated := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: pod.Namespace}, Data: map[string]string{"payload": strings.Repeat("x", 1024*1024)}}
	require.NoError(t, apiClient.Create(ctx, legacy))
	require.NoError(t, apiClient.Create(ctx, unrelated))
	oldManager, err := ctrl.NewManager(config, ctrl.Options{Scheme: scheme, Metrics: server.Options{BindAddress: "0"}, HealthProbeBindAddress: "0"})
	require.NoError(t, err)
	stopOld := startConfigMapCache(t, ctx, oldManager)
	require.NoError(t, oldManager.GetClient().Get(ctx, client.ObjectKeyFromObject(unrelated), &corev1.ConfigMap{}))
	require.NoError(t, helmhooks.MigrateGPUSharingConfigMaps(ctx, apiClient))
	// The old binder can still create a map after the pre-upgrade scan.
	late := legacy.DeepCopy()
	late.Name = "worker-shared-gpu-0-evar"
	late.ResourceVersion = ""
	late.UID = ""
	require.NoError(t, apiClient.Create(ctx, late))
	stopOld()
	options := &Options{MetricsAddr: "0", ProbeAddr: "0", QPS: 50, Burst: 100}
	app, err := New(options, config)
	require.NoError(t, err)
	stopNew := startConfigMapCache(t, ctx, app.manager)
	for _, c := range []client.Client{app.Client, app.manager.GetClient()} {
		require.True(t, errors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(unrelated), &corev1.ConfigMap{})))
		require.True(t, errors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(late), &corev1.ConfigMap{})))
	}
	require.NoError(t, helmhooks.MigrateGPUSharingConfigMaps(ctx, apiClient))
	require.NoError(t, helmhooks.MigrateGPUSharingConfigMaps(ctx, apiClient))
	for _, original := range []*corev1.ConfigMap{legacy, late} {
		require.Eventually(t, func() bool {
			for _, c := range []client.Client{app.Client, app.manager.GetClient()} {
				stored := &corev1.ConfigMap{}
				if c.Get(ctx, client.ObjectKeyFromObject(original), stored) != nil {
					return false
				}
				if stored.Labels[gpusharingconfigmap.GPUSharingConfigMapLabel] != "true" {
					return false
				}
				require.Equal(t, original.UID, stored.UID)
				require.Equal(t, original.OwnerReferences, stored.OwnerReferences)
				require.Equal(t, original.Data, stored.Data)
				require.Equal(t, original.BinaryData, stored.BinaryData)
				require.Equal(t, "label", stored.Labels["existing"])
			}
			return true
		}, 10*time.Second, 50*time.Millisecond)
	}
	require.NoError(t, common.UpdateConfigMapEnvironmentVariable(ctx, app.Client, pod, late.Name, func(data map[string]string) error { data["CUDA_VISIBLE_DEVICES"] = "0"; return nil }))
	require.NoError(t, gpusharingconfigmap.UpsertJobConfigMap(ctx, app.Client, pod, "new", map[string]string{"value": "new-map"}))
	require.Eventually(t, func() bool {
		return app.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: "new"}, &corev1.ConfigMap{}) == nil
	}, 10*time.Second, 50*time.Millisecond)
	stopNew()
	restarted, err := New(options, config)
	require.NoError(t, err)
	startConfigMapCache(t, ctx, restarted.manager)
	for _, c := range []client.Client{restarted.Client, restarted.manager.GetClient()} {
		stored := &corev1.ConfigMap{}
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(late), stored))
		require.Equal(t, "0", stored.Data["CUDA_VISIBLE_DEVICES"])
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: "new"}, &corev1.ConfigMap{}))
		require.True(t, errors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(unrelated), &corev1.ConfigMap{})))
	}
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
