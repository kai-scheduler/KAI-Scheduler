// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUpdateConfigMapEnvironmentVariableOwnership(t *testing.T) {
	owner := metav1.OwnerReference{APIVersion: "v1", Kind: "Pod", Name: "pod", UID: "pod-uid"}
	for _, tt := range []struct {
		name        string
		owners      []metav1.OwnerReference
		nilData     bool
		emptyPodUID bool
		wantOK      bool
	}{
		{name: "matching owner without controller flag", owners: []metav1.OwnerReference{owner}, wantOK: true},
		{name: "matching owner with nil data", owners: []metav1.OwnerReference{owner}, nilData: true, wantOK: true},
		{name: "missing pod UID", owners: []metav1.OwnerReference{owner}, emptyPodUID: true},
		{name: "foreign owner", owners: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "pod", UID: "another-uid"}}},
		{name: "unowned"},
		{name: "name-only owner", owners: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "pod"}}},
		{name: "multiple owners", owners: []metav1.OwnerReference{owner, {APIVersion: "v1", Kind: "Pod", Name: "another", UID: "another-uid"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "namespace", UID: "pod-uid"}}
			if tt.emptyPodUID {
				pod.UID = ""
			}
			cm := &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name: "config", Namespace: pod.Namespace, OwnerReferences: tt.owners,
				Labels: map[string]string{"unrelated": "label"},
			}, Data: map[string]string{"unrelated": "data"}}
			if tt.nilData {
				cm.Data = nil
			}
			scheme := runtime.NewScheme()
			require.NoError(t, v1.AddToScheme(scheme))
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
			called := false
			err := UpdateConfigMapEnvironmentVariable(t.Context(), kubeClient, pod, cm.Name,
				func(data map[string]string) error { called = true; data["allocation"] = "new"; return nil })
			assert.Equal(t, tt.wantOK, called, "reject ownership before running the data mutation")
			if tt.wantOK {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "not exclusively owned")
			}
			got := &v1.ConfigMap{}
			require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKeyFromObject(cm), got))
			assert.Equal(t, cm.OwnerReferences, got.OwnerReferences)
			assert.Equal(t, cm.Labels, got.Labels)
			if !tt.nilData {
				assert.Equal(t, "data", got.Data["unrelated"])
			}
			if tt.wantOK {
				assert.Equal(t, "new", got.Data["allocation"])
			} else {
				assert.Equal(t, cm.Data, got.Data)
			}
		})
	}
}
