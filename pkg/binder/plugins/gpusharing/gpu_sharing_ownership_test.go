// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/hamicore"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/constants"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestGPUSharingRollbackOwnership(t *testing.T) {
	owner := metav1.OwnerReference{APIVersion: "v1", Kind: "Pod", Name: "pod", UID: "pod-uid"}
	tests := []struct {
		name             string
		owners           []metav1.OwnerReference
		missingPodUID    bool
		missingCMUID     bool
		missingVersion   bool
		readError        bool
		deleteError      bool
		deleteDisappears bool
		wantKept         bool
		wantError        bool
	}{
		{name: "single matching owner without controller flag", owners: []metav1.OwnerReference{owner}},
		{name: "foreign pod UID", owners: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "pod", UID: "replacement-uid"}}, wantKept: true},
		{name: "foreign pod name", owners: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "another-pod", UID: "pod-uid"}}, wantKept: true},
		{name: "wrong owner kind", owners: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Node", Name: "pod", UID: "pod-uid"}}, wantKept: true},
		{name: "wrong API version", owners: []metav1.OwnerReference{{APIVersion: "other/v1", Kind: "Pod", Name: "pod", UID: "pod-uid"}}, wantKept: true},
		{name: "unowned", wantKept: true},
		{name: "legacy name-only owner", owners: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "pod"}}, wantKept: true},
		{name: "additional owner", owners: []metav1.OwnerReference{owner, {APIVersion: "v1", Kind: "Pod", Name: "another", UID: "another-uid"}}, wantKept: true},
		{name: "missing pod UID", owners: []metav1.OwnerReference{owner}, missingPodUID: true, wantKept: true, wantError: true},
		{name: "missing ConfigMap UID", owners: []metav1.OwnerReference{owner}, missingCMUID: true, wantKept: true, wantError: true},
		{name: "missing ConfigMap resource version", owners: []metav1.OwnerReference{owner}, missingVersion: true, wantKept: true, wantError: true},
		{name: "read error", owners: []metav1.OwnerReference{owner}, readError: true, wantKept: true, wantError: true},
		{name: "delete error", owners: []metav1.OwnerReference{owner}, deleteError: true, wantKept: true, wantError: true},
		{name: "deleted after read", owners: []metav1.OwnerReference{owner}, deleteDisappears: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := sharingPod()
			if tt.missingPodUID {
				pod.UID = ""
			}
			capabilities := ownedConfigMap("sharing-0", tt.owners)
			if tt.missingCMUID {
				capabilities.UID = ""
			}
			direct := ownedConfigMap("sharing-0-evar", []metav1.OwnerReference{owner})
			scheme := runtime.NewScheme()
			require.NoError(t, v1.AddToScheme(scheme))
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(capabilities, direct).Build()
			observedClient := interceptor.NewClient(kubeClient, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == capabilities.Name && tt.readError {
						return apierrors.NewInternalError(errors.New("read failed"))
					}
					err := c.Get(ctx, key, obj, opts...)
					if key.Name == capabilities.Name && tt.missingVersion {
						obj.SetResourceVersion("")
					}
					return err
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if obj.GetName() == capabilities.Name {
						if tt.deleteError {
							return apierrors.NewInternalError(errors.New("delete failed"))
						}
						if tt.deleteDisappears {
							require.NoError(t, c.Delete(ctx, obj))
						}
					}
					return c.Delete(ctx, obj, opts...)
				},
			})

			err := gpusharing.New(observedClient, false, false).Rollback(t.Context(), pod, nil, sharingRequest(), nil)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			got := &v1.ConfigMap{}
			err = kubeClient.Get(t.Context(), client.ObjectKeyFromObject(capabilities), got)
			if tt.wantKept {
				require.NoError(t, err)
				assert.Equal(t, capabilities.Data, got.Data)
				assert.Equal(t, capabilities.OwnerReferences, got.OwnerReferences)
			} else {
				assert.True(t, apierrors.IsNotFound(err), "matching ConfigMap must be deleted: %v", err)
			}
			err = kubeClient.Get(t.Context(), client.ObjectKeyFromObject(direct), &v1.ConfigMap{})
			if tt.missingPodUID {
				require.NoError(t, err)
			} else {
				assert.True(t, apierrors.IsNotFound(err), "rollback must continue to the second ConfigMap: %v", err)
			}
		})
	}
}

func TestGPUSharingConfigMapPluginPaths(t *testing.T) {
	for _, mode := range []string{"modern", "legacy", "nri", "hami"} {
		t.Run(mode, func(t *testing.T) {
			pod := sharingPod()
			if mode == "modern" {
				common.AddGPUSharingEnvVars(&pod.Spec.Containers[0], "sharing-0")
			}
			scheme := runtime.NewScheme()
			require.NoError(t, v1.AddToScheme(scheme))
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			bindingState := &state.BindingState{ReservedGPUIds: []string{"gpu-0"}}
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.NvidiaGpuMemory: "1000"}}}
			require.NoError(t, gpusharing.New(kubeClient, false, mode == "nri").PreBind(
				context.Background(), pod, node, sharingRequest(), bindingState))
			if mode == "hami" {
				require.NoError(t, hamicore.New(kubeClient).PreBind(t.Context(), pod, node, sharingRequest(), bindingState))
			}
			capabilities := &v1.ConfigMap{}
			require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKey{Namespace: pod.Namespace, Name: "sharing-0"}, capabilities))
			assert.Equal(t, "0.5", capabilities.Data[common.GPUPortion])
			if mode == "modern" {
				assert.Equal(t, "gpu-0", capabilities.Data[constants.NvidiaVisibleDevices])
			}
			if mode == "hami" {
				assert.NotEmpty(t, capabilities.Data[common.CudaDeviceMemoryLimit])
			}
			direct := &v1.ConfigMap{}
			require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKey{Namespace: pod.Namespace, Name: "sharing-0-evar"}, direct))
			if mode == "legacy" || mode == "hami" {
				assert.Equal(t, "gpu-0", direct.Data[constants.NvidiaVisibleDevices])
			}
			if mode == "nri" {
				assert.Equal(t, "gpu-0", bindingState.BindingPodAnnotations["nvidia.cdi.k8s.io/container.container"])
			}
		})
	}
}

func sharingPod() *v1.Pod {
	return &v1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "pod", Namespace: "test-ns", UID: "pod-uid",
		Annotations: map[string]string{constants.GpuSharingConfigMapAnnotation: "sharing"},
	}, Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container"}}}}
}

func sharingRequest() *v1alpha2.BindRequest {
	return &v1alpha2.BindRequest{Spec: v1alpha2.BindRequestSpec{
		ReceivedResourceType: common.ReceivedTypeFraction,
		ReceivedGPU:          &v1alpha2.ReceivedGPU{Portion: "0.5"},
	}}
}

func ownedConfigMap(name string, owners []metav1.OwnerReference) *v1.ConfigMap {
	return &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "test-ns", UID: "configmap-uid", ResourceVersion: "1", OwnerReferences: owners,
	}, Data: map[string]string{"existing": "value"}}
}
