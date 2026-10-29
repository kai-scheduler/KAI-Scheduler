// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingconfigmap_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/api/constants"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestConfigMapMutationsAPI(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is required for API precondition tests")
	}
	testEnv := &envtest.Environment{}
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	kubeClient, err := client.NewWithWatch(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	for _, sameOwner := range []bool{true, false} {
		ownerCase := "same owner"
		if !sameOwner {
			ownerCase = "different owner"
		}
		t.Run("upsert "+ownerCase+" preserves unrelated fields", func(t *testing.T) {
			pod := mutationPod(t, kubeClient)
			cm := mutationConfigMap(pod, "config")
			if !sameOwner {
				cm.OwnerReferences[0].UID = "previous-pod-uid"
			}
			require.NoError(t, kubeClient.Create(t.Context(), cm))
			uid := cm.UID
			require.NoError(t, gpusharingconfigmap.UpsertJobConfigMap(t.Context(), kubeClient, pod, cm.Name,
				map[string]string{"allocation": "new"}))
			got := &v1.ConfigMap{}
			require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKeyFromObject(cm), got))
			assert.Equal(t, uid, got.UID)
			assert.Equal(t, cm.Labels, got.Labels)
			assert.Equal(t, cm.Annotations, got.Annotations)
			if sameOwner {
				assert.Equal(t, cm.BinaryData, got.BinaryData)
			} else {
				assert.Empty(t, got.BinaryData, "takeover replaces allocation payload")
			}
			assert.Equal(t, pod.UID, got.OwnerReferences[0].UID)
			assert.Equal(t, "new", got.Data["allocation"])
			_, retained := got.Data["previous"]
			assert.Equal(t, sameOwner, retained, "intentional owner takeover replaces stale data")
		})
		for _, recreate := range []bool{false, true} {
			mutation := "owner changes on same object"
			if recreate {
				mutation = "object replaced"
			}
			t.Run("upsert "+ownerCase+" rejects "+mutation, func(t *testing.T) {
				pod := mutationPod(t, kubeClient)
				cm := mutationConfigMap(pod, "config")
				if !sameOwner {
					cm.OwnerReferences[0].UID = "previous-pod-uid"
				}
				require.NoError(t, kubeClient.Create(t.Context(), cm))
				var concurrent *v1.ConfigMap
				patchCalls := 0
				racingClient := interceptor.NewClient(kubeClient, interceptor.Funcs{
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						patchCalls++
						concurrent = replaceAllocation(t, c, cm, recreate)
						return c.Patch(ctx, obj, patch, opts...)
					},
				})
				err := gpusharingconfigmap.UpsertJobConfigMap(t.Context(), racingClient, pod, cm.Name,
					map[string]string{"allocation": "stale"})
				assert.True(t, apierrors.IsConflict(err), "stale upsert must surface a conflict: %v", err)
				assert.Equal(t, 1, patchCalls, "do not retry against the new allocation")
				assertAllocationUnchanged(t, kubeClient, concurrent)
			})
		}
	}

	t.Run("takeover replaces binary entry with string data", func(t *testing.T) {
		pod := mutationPod(t, kubeClient)
		cm := mutationConfigMap(pod, "config")
		cm.OwnerReferences[0].UID = "previous-pod-uid"
		require.NoError(t, kubeClient.Create(t.Context(), cm))
		require.NoError(t, gpusharingconfigmap.UpsertJobConfigMap(t.Context(), kubeClient, pod, cm.Name,
			map[string]string{"unrelated": "new string value"}))
		got := &v1.ConfigMap{}
		require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKeyFromObject(cm), got))
		assert.Equal(t, pod.UID, got.OwnerReferences[0].UID)
		assert.Equal(t, map[string]string{"unrelated": "new string value"}, got.Data)
		assert.Empty(t, got.BinaryData)
	})

	for _, recreate := range []bool{false, true} {
		mutation := "owner changes on same object"
		if recreate {
			mutation = "object replaced"
		}
		t.Run("environment update rejects "+mutation, func(t *testing.T) {
			pod := mutationPod(t, kubeClient)
			cm := mutationConfigMap(pod, "config")
			require.NoError(t, kubeClient.Create(t.Context(), cm))
			var concurrent *v1.ConfigMap
			patchCalls := 0
			racingClient := interceptor.NewClient(kubeClient, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					patchCalls++
					concurrent = replaceAllocation(t, c, cm, recreate)
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			err := common.UpdateConfigMapEnvironmentVariable(t.Context(), racingClient, pod, cm.Name,
				func(data map[string]string) error { data["allocation"] = "stale"; return nil })
			assert.True(t, apierrors.IsConflict(err), "stale environment update must surface a conflict: %v", err)
			assert.Equal(t, 1, patchCalls)
			assertAllocationUnchanged(t, kubeClient, concurrent)
		})

		t.Run("rollback rejects "+mutation+" and continues cleanup", func(t *testing.T) {
			pod := mutationPod(t, kubeClient)
			capabilities := mutationConfigMap(pod, "sharing-0")
			direct := mutationConfigMap(pod, "sharing-0-evar")
			require.NoError(t, kubeClient.Create(t.Context(), capabilities))
			require.NoError(t, kubeClient.Create(t.Context(), direct))
			var concurrent *v1.ConfigMap
			deleteCalls := 0
			racingClient := interceptor.NewClient(kubeClient, interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deleteCalls++
					deleteOptions := (&client.DeleteOptions{}).ApplyOptions(opts)
					if assert.NotNil(t, deleteOptions.Preconditions, "rollback requires both delete preconditions") {
						assert.Equal(t, &obj.(*v1.ConfigMap).UID, deleteOptions.Preconditions.UID)
						assert.Equal(t, &obj.(*v1.ConfigMap).ResourceVersion, deleteOptions.Preconditions.ResourceVersion)
					}
					if obj.GetName() == capabilities.Name {
						concurrent = replaceAllocation(t, c, capabilities, recreate)
					}
					return c.Delete(ctx, obj, opts...)
				},
			})
			err := gpusharing.New(racingClient, false, false).Rollback(t.Context(), pod, nil,
				&v1alpha2.BindRequest{Spec: v1alpha2.BindRequestSpec{ReceivedResourceType: common.ReceivedTypeFraction}}, nil)
			assert.True(t, apierrors.IsConflict(err), "stale rollback must surface a conflict: %v", err)
			assert.Equal(t, 2, deleteCalls, "attempt the other ConfigMap but do not retry the conflict")
			assertAllocationUnchanged(t, kubeClient, concurrent)
			err = kubeClient.Get(t.Context(), client.ObjectKeyFromObject(direct), &v1.ConfigMap{})
			assert.True(t, apierrors.IsNotFound(err), "second ConfigMap must be cleaned up: %v", err)
		})
	}
}

func mutationPod(t *testing.T, kubeClient client.Client) *v1.Pod {
	t.Helper()
	namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "configmap-mutations-"}}
	require.NoError(t, kubeClient.Create(t.Context(), namespace))
	return &v1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "pod", Namespace: namespace.Name, UID: "pod-uid",
		Annotations: map[string]string{constants.GpuSharingConfigMapAnnotation: "sharing"},
	}, Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container"}}}}
}

func mutationConfigMap(pod *v1.Pod, name string) *v1.ConfigMap {
	return &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: pod.Namespace,
		Labels: map[string]string{"unrelated": "label"}, Annotations: map[string]string{"unrelated": "annotation"},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID}},
	}, Data: map[string]string{"previous": "value"}, BinaryData: map[string][]byte{"unrelated": {1, 2}}}
}

func replaceAllocation(t *testing.T, kubeClient client.Client, original *v1.ConfigMap, recreate bool) *v1.ConfigMap {
	t.Helper()
	current := &v1.ConfigMap{}
	require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKeyFromObject(original), current))
	current.OwnerReferences[0].UID = "replacement-pod-uid"
	current.Data = map[string]string{"allocation": "replacement"}
	if recreate {
		require.NoError(t, kubeClient.Delete(t.Context(), current))
		current.UID = ""
		current.ResourceVersion = ""
		current.CreationTimestamp = metav1.Time{}
		require.NoError(t, kubeClient.Create(t.Context(), current))
		require.NotEqual(t, original.UID, current.UID)
	} else {
		require.NoError(t, kubeClient.Update(t.Context(), current))
		require.Equal(t, original.UID, current.UID)
	}
	require.NotEqual(t, original.ResourceVersion, current.ResourceVersion)
	return current.DeepCopy()
}

func assertAllocationUnchanged(t *testing.T, kubeClient client.Client, expected *v1.ConfigMap) {
	t.Helper()
	require.NotNil(t, expected)
	got := &v1.ConfigMap{}
	require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKeyFromObject(expected), got))
	assert.Equal(t, expected.UID, got.UID)
	assert.Equal(t, expected.ResourceVersion, got.ResourceVersion)
	assert.Equal(t, expected.OwnerReferences, got.OwnerReferences)
	assert.Equal(t, expected.Data, got.Data)
	assert.Equal(t, expected.Labels, got.Labels)
	assert.Equal(t, expected.Annotations, got.Annotations)
	assert.Equal(t, expected.BinaryData, got.BinaryData)
}
