// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/api/kai/v1/common"
)

func TestMergeKeepsDefaultsTheShardDoesNotSet(t *testing.T) {
	defaults := &RecorderSpec{
		Enabled:           ptr.To(true),
		Address:           "127.0.0.1:9095",
		HashKeySecretName: "shared-key",
		Batch:             &RecorderBatch{RollInterval: &metav1.Duration{Duration: time.Minute}},
	}
	shard := &RecorderSpec{Address: "127.0.0.1:1", Batch: &RecorderBatch{RollSizeMB: ptr.To(8)}}
	merged := Merge(defaults, shard)
	require.True(t, merged.EnabledValue())
	require.Equal(t, "127.0.0.1:1", merged.Address)
	require.Equal(t, "shared-key", merged.HashKeySecretName)
	require.Equal(t, time.Minute, merged.Batch.RollInterval.Duration)
	require.Equal(t, 8, *merged.Batch.RollSizeMB)

	require.False(t, (*RecorderSpec)(nil).EnabledValue())
	require.Nil(t, (*RecorderSpec)(nil).DeepCopy())
}

func TestApplyDefaultsFillsTheSidecarImageAndResources(t *testing.T) {
	spec := &RecorderSpec{}
	fallback := &common.Image{Repository: ptr.To("registry.example"), Tag: ptr.To("v1"), PullPolicy: ptr.To(corev1.PullIfNotPresent)}
	ApplyDefaults(spec, fallback)
	require.False(t, spec.EnabledValue())
	require.Equal(t, DefaultAddress, spec.Address)
	require.Equal(t, DefaultHashKeySecretName, spec.HashKeySecretName)
	require.Equal(t, DefaultStorageSecretName, spec.StorageSecretName)
	require.Equal(t, DefaultRollInterval, spec.Batch.RollInterval.Duration)
	require.Equal(t, DefaultRollSizeMB, *spec.Batch.RollSizeMB)
	require.Equal(t, "recorder", *spec.Image.Name)
	require.Equal(t, "registry.example", *spec.Image.Repository)
	require.Equal(t, "v1", *spec.Image.Tag)
	require.Equal(t, corev1.PullIfNotPresent, *spec.Image.PullPolicy)
	require.False(t, spec.Resources.Requests.Cpu().IsZero())

	custom := resource.MustParse("1")
	kept := &RecorderSpec{Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: custom}}}
	ApplyDefaults(kept, nil)
	require.Equal(t, custom.String(), kept.Resources.Requests.Cpu().String())
	require.Equal(t, "recorder", *kept.Image.Name)
}

func TestDeepCopyDoesNotSharePointers(t *testing.T) {
	original := &RecorderSpec{
		Enabled: ptr.To(true),
		Image:   &common.Image{Name: ptr.To("recorder")},
		Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		}},
		Batch: &RecorderBatch{RollInterval: &metav1.Duration{Duration: time.Second}, RollSizeMB: ptr.To(4)},
	}
	copied := original.DeepCopy()
	*copied.Enabled = false
	*copied.Batch.RollSizeMB = 9
	copied.Batch.RollInterval.Duration = time.Minute
	require.True(t, *original.Enabled)
	require.Equal(t, 4, *original.Batch.RollSizeMB)
	require.Equal(t, time.Second, original.Batch.RollInterval.Duration)
}
