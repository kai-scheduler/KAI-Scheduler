// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	recorderv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/recorder/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	"github.com/kai-scheduler/api/kai/v1/common"
	kaiv1scheduler "github.com/kai-scheduler/api/kai/v1/scheduler"
)

func TestRecorderSidecarAndPluginStayOffByDefault(t *testing.T) {
	ctx := context.Background()
	config, shard := recorderFixture()
	operand := NewSchedulerForShard(shard)

	deployment := recorderDeployment(t, ctx, operand, config, shard)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)

	parsed := recorderSchedulerConfig(t, ctx, operand, config, shard)
	for _, plugin := range parsed.Tiers[0].Plugins {
		require.NotEqual(t, "recorder", plugin.Name)
	}
}

func TestRecorderEnabledAddsSidecarAndPlugin(t *testing.T) {
	ctx := context.Background()
	config, shard := recorderFixture()
	operand := NewSchedulerForShard(shard)
	operand.recorderOverride = &recorderv1.RecorderSpec{Enabled: ptr.To(true)}

	deployment := recorderDeployment(t, ctx, operand, config, shard)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 2)
	sidecar := deployment.Spec.Template.Spec.Containers[1]
	require.Equal(t, "recorder", sidecar.Name)
	require.NotEmpty(t, deployment.Spec.Template.Spec.Containers[0].Args)
	var hashMounted, tokenHidden bool
	for _, mount := range sidecar.VolumeMounts {
		require.NotEqual(t, "recorder-storage", mount.Name)
		switch mount.Name {
		case "recorder-hash-key":
			hashMounted = true
			require.True(t, mount.ReadOnly)
		}
		if mount.MountPath == "/var/run/secrets/kubernetes.io/serviceaccount" {
			tokenHidden = true
			require.Equal(t, "recorder-no-sa-token", mount.Name)
		}
	}
	require.True(t, hashMounted)
	require.True(t, tokenHidden)
	require.Equal(t, config.Spec.Global.GetSecurityContext(), sidecar.SecurityContext)
	for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
		require.NotEqual(t, "/var/run/secrets/kubernetes.io/serviceaccount", mount.MountPath)
	}
	require.True(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken == nil || *deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		require.NotEqual(t, "recorder-storage", volume.Name)
		if volume.Name == "recorder-data" {
			require.NotNil(t, volume.EmptyDir)
			require.NotNil(t, volume.EmptyDir.SizeLimit)
		}
		if volume.Name == "recorder-hash-key" {
			require.NotNil(t, volume.Secret)
			require.Equal(t, recorderv1.DefaultHashKeySecretName, volume.Secret.SecretName)
		}
		if volume.Name == "recorder-no-sa-token" {
			require.NotNil(t, volume.EmptyDir)
		}
	}

	parsed := recorderSchedulerConfig(t, ctx, operand, config, shard)
	var found bool
	for _, plugin := range parsed.Tiers[0].Plugins {
		if plugin.Name != "recorder" {
			continue
		}
		found = true
		require.Equal(t, recorderv1.DefaultAddress, plugin.Arguments["address"])
		require.Equal(t, shard.Name, plugin.Arguments["shardName"])
	}
	require.True(t, found)
}

func TestRecorderSidecarUsesBatchAndImageFromTheSpec(t *testing.T) {
	ctx := context.Background()
	config, shard := recorderFixture()
	operand := NewSchedulerForShard(shard)
	pull := corev1.PullAlways
	operand.recorderOverride = &recorderv1.RecorderSpec{
		Enabled: ptr.To(true),
		Address: "127.0.0.1:9099",
		Image:   &common.Image{PullPolicy: &pull, Repository: ptr.To("registry.example"), Name: ptr.To("recorder"), Tag: ptr.To("v9")},
		Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("50m"),
		}},
		Batch: &recorderv1.RecorderBatch{
			RollInterval: &metav1.Duration{Duration: time.Minute},
			RollSizeMB:   ptr.To(8),
		},
	}

	deployment := recorderDeployment(t, ctx, operand, config, shard)
	sidecar := deployment.Spec.Template.Spec.Containers[1]
	require.Equal(t, corev1.PullAlways, sidecar.ImagePullPolicy)
	require.Contains(t, sidecar.Image, "registry.example")
	require.Equal(t, "50m", sidecar.Resources.Requests.Cpu().String())
	require.Contains(t, sidecar.Args, "--listen=127.0.0.1:9099")
	require.Contains(t, sidecar.Args, "--roll-interval=1m0s")
	require.Contains(t, sidecar.Args, "--roll-bytes=8388608")
}

func TestRecorderStartsWithoutStorageSecret(t *testing.T) {
	ctx := context.Background()
	config, shard := recorderFixture()
	operand := NewSchedulerForShard(shard)
	operand.recorderOverride = &recorderv1.RecorderSpec{Enabled: ptr.To(true)}
	kube := fake.NewClientBuilder().Build()

	object, err := operand.deploymentForShard(ctx, kube, config, shard)
	require.NoError(t, err)
	deployment, ok := object.(*appsv1.Deployment)
	require.True(t, ok)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 2)
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		require.NotEqual(t, "recorder-storage", volume.Name)
		if volume.Secret != nil {
			require.NotEqual(t, recorderv1.DefaultStorageSecretName, volume.Secret.SecretName)
		}
	}
	secrets := &corev1.SecretList{}
	require.NoError(t, kube.List(ctx, secrets))
	require.Empty(t, secrets.Items)
}

func TestHashKeySecretIsSharedByShardsAndNotRotated(t *testing.T) {
	ctx := context.Background()
	config, shardA := recorderFixture()
	config.UID = "config-uid"
	shardB := shardA.DeepCopy()
	shardB.Name = "other"
	kube := fake.NewClientBuilder().Build()
	operand := &SchedulerForShard{recorderOverride: &recorderv1.RecorderSpec{Enabled: ptr.To(true)}}
	operand.apiReader = kube

	require.NoError(t, operand.EnsureSharedHashKey(ctx, kube, kube, config, shardA))
	first := &corev1.Secret{}
	require.NoError(t, kube.Get(ctx, client.ObjectKey{Name: recorderv1.DefaultHashKeySecretName, Namespace: config.Spec.Namespace}, first))
	require.NotEmpty(t, first.Data[recorderv1.HMACKeyDataKey])
	require.Equal(t, "Config", first.OwnerReferences[0].Kind)
	require.Equal(t, config.Name, first.OwnerReferences[0].Name)
	require.False(t, first.OwnerReferences[0].Controller != nil && *first.OwnerReferences[0].Controller)

	require.NoError(t, operand.EnsureSharedHashKey(ctx, kube, kube, config, shardB))
	second := &corev1.Secret{}
	require.NoError(t, kube.Get(ctx, client.ObjectKey{Name: recorderv1.DefaultHashKeySecretName, Namespace: config.Spec.Namespace}, second))
	require.Equal(t, first.Data[recorderv1.HMACKeyDataKey], second.Data[recorderv1.HMACKeyDataKey])
	require.Equal(t, first.OwnerReferences, second.OwnerReferences)
}

func TestResolveRecorderReadsShardAndConfigObjects(t *testing.T) {
	ctx := context.Background()
	config, shard := recorderFixture()
	reader := specReader{objects: map[string]map[string]any{
		shard.Name: {"spec": map[string]any{"recorder": map[string]any{
			"enabled": true,
			"address": "127.0.0.1:1",
		}}},
		config.Name: {"spec": map[string]any{"scheduler": map[string]any{"recorder": map[string]any{
			"hashKeySecretName": "from-config",
		}}}},
		"broken": {"spec": "not-a-map"},
	}}
	operand := &SchedulerForShard{}
	spec, err := operand.resolveRecorder(ctx, reader, config, shard)
	require.NoError(t, err)
	require.True(t, spec.EnabledValue())
	require.Equal(t, "127.0.0.1:1", spec.Address)
	require.Equal(t, "from-config", spec.HashKeySecretName)

	missing, err := operand.resolveRecorder(ctx, specReader{}, config, shard)
	require.NoError(t, err)
	require.False(t, missing.EnabledValue())

	brokenShard := shard.DeepCopy()
	brokenShard.Name = "broken"
	broken, err := operand.resolveRecorder(ctx, reader, config, brokenShard)
	require.NoError(t, err)
	require.False(t, broken.EnabledValue())

	empty, err := readRecorderSpec(ctx, nil, kaiv1.GroupVersion.WithKind("Config"), client.ObjectKey{}, "spec")
	require.NoError(t, err)
	require.Nil(t, empty)
}

func TestEnsureSharedHashKeyReportsASecretReadError(t *testing.T) {
	ctx := context.Background()
	config, shard := recorderFixture()
	reader := specReader{fail: recorderv1.DefaultHashKeySecretName}
	operand := &SchedulerForShard{recorderOverride: &recorderv1.RecorderSpec{Enabled: ptr.To(true)}}
	err := operand.EnsureSharedHashKey(ctx, fake.NewClientBuilder().Build(), reader, config, shard)
	require.ErrorContains(t, err, "boom")

	disabled := &SchedulerForShard{recorderOverride: &recorderv1.RecorderSpec{Enabled: ptr.To(false)}}
	require.NoError(t, disabled.EnsureSharedHashKey(ctx, fake.NewClientBuilder().Build(), reader, config, shard))
}

func TestRecorderPluginKeepsAnExistingPriority(t *testing.T) {
	ctx := context.Background()
	config, shard := recorderFixture()
	priority := 12
	shard.Spec.Plugins = map[string]kaiv1.PluginConfig{
		"recorder": {Priority: &priority, Arguments: map[string]string{"extra": "kept"}},
	}
	operand := NewSchedulerForShard(shard)
	operand.recorderOverride = &recorderv1.RecorderSpec{Enabled: ptr.To(true), Address: "127.0.0.1:9099"}
	parsed := recorderSchedulerConfig(t, ctx, operand, config, shard)
	var found bool
	for _, plugin := range parsed.Tiers[0].Plugins {
		if plugin.Name != "recorder" {
			continue
		}
		found = true
		require.Equal(t, "kept", plugin.Arguments["extra"])
		require.Equal(t, "127.0.0.1:9099", plugin.Arguments["address"])
	}
	require.True(t, found)
}

type specReader struct {
	objects map[string]map[string]any
	fail    string
}

func (r specReader) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	if key.Name == r.fail {
		return fmt.Errorf("boom")
	}
	raw, ok := r.objects[key.Name]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "objects"}, key.Name)
	}
	target, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unexpected object type %T", obj)
	}
	target.Object = raw
	return nil
}

func (r specReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return nil
}

func recorderFixture() (*kaiv1.Config, *kaiv1.SchedulingShard) {
	config := &kaiv1.Config{
		ObjectMeta: metav1.ObjectMeta{Name: "kai-config"},
		Spec: kaiv1.ConfigSpec{
			Global:    &kaiv1.GlobalConfig{},
			Namespace: "kai",
			Scheduler: &kaiv1scheduler.Scheduler{},
		},
	}
	config.Spec.SetDefaultsWhereNeeded()
	shard := &kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	shard.Spec.SetDefaultsWhereNeeded()
	return config, shard
}

func recorderDeployment(t *testing.T, ctx context.Context, operand *SchedulerForShard, config *kaiv1.Config, shard *kaiv1.SchedulingShard) *appsv1.Deployment {
	t.Helper()
	object, err := operand.deploymentForShard(ctx, fake.NewClientBuilder().Build(), config, shard)
	require.NoError(t, err)
	deployment, ok := object.(*appsv1.Deployment)
	require.True(t, ok)
	return deployment
}

func recorderSchedulerConfig(t *testing.T, ctx context.Context, operand *SchedulerForShard, config *kaiv1.Config, shard *kaiv1.SchedulingShard) conf.SchedulerConfiguration {
	t.Helper()
	object, err := operand.configMapForShard(ctx, fake.NewClientBuilder().Build(), config, shard)
	require.NoError(t, err)
	configMap, ok := object.(*corev1.ConfigMap)
	require.True(t, ok)
	parsed := conf.SchedulerConfiguration{}
	require.NoError(t, yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &parsed))
	return parsed
}
