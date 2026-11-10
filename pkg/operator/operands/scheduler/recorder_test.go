// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	recorderv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/recorder/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
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
	var secretMounted bool
	for _, mount := range sidecar.VolumeMounts {
		if mount.Name == "recorder-storage" {
			secretMounted = true
			require.True(t, mount.ReadOnly)
		}
	}
	require.True(t, secretMounted)
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == "recorder-storage" {
			require.NotNil(t, volume.Secret)
			require.Equal(t, recorderv1.DefaultStorageSecretName, volume.Secret.SecretName)
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
