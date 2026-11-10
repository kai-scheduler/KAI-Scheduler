// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"encoding/json"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	recorderv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/recorder/v1"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	"github.com/kai-scheduler/api/kai/v1/common"
)

// recorderOverride is set by tests. The generated SchedulingShard type cannot carry spec.recorder
// until github.com/kai-scheduler/api publishes the field.
func (s *SchedulerForShard) resolveRecorder(
	ctx context.Context, reader client.Reader, kaiConfig *kaiv1.Config, shard *kaiv1.SchedulingShard,
) (*recorderv1.RecorderSpec, error) {
	var shardSpec, configSpec *recorderv1.RecorderSpec
	var err error
	if s.apiReader != nil {
		reader = s.apiReader
	}
	if s.recorderOverride != nil {
		shardSpec = s.recorderOverride.DeepCopy()
	} else if shard != nil && shard.Name != "" {
		shardSpec, err = readRecorderSpec(ctx, reader, kaiv1.GroupVersion.WithKind("SchedulingShard"), client.ObjectKey{Name: shard.Name}, "spec", "recorder")
		if err != nil {
			return nil, err
		}
	}
	if kaiConfig != nil && kaiConfig.Name != "" {
		configSpec, err = readRecorderSpec(ctx, reader, kaiv1.GroupVersion.WithKind("Config"), client.ObjectKey{Name: kaiConfig.Name}, "spec", "scheduler", "recorder")
		if err != nil {
			return nil, err
		}
	}
	merged := recorderv1.Merge(configSpec, shardSpec)
	var fallback *common.Image
	if kaiConfig != nil && kaiConfig.Spec.Scheduler != nil && kaiConfig.Spec.Scheduler.Service != nil {
		fallback = kaiConfig.Spec.Scheduler.Service.Image
	}
	recorderv1.ApplyDefaults(merged, fallback)
	return merged, nil
}

func readRecorderSpec(ctx context.Context, reader client.Reader, gvk schema.GroupVersionKind, key client.ObjectKey, path ...string) (*recorderv1.RecorderSpec, error) {
	if reader == nil || key.Name == "" {
		return nil, nil
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := reader.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	current := any(obj.Object)
	for _, field := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, nil
		}
		next, found := asMap[field]
		if !found || next == nil {
			return nil, nil
		}
		current = next
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	spec := &recorderv1.RecorderSpec{}
	if err := json.Unmarshal(raw, spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func pluginsWithRecorder(plugins map[string]kaiv1.PluginConfig, spec *recorderv1.RecorderSpec, kaiConfig *kaiv1.Config, shard *kaiv1.SchedulingShard) map[string]kaiv1.PluginConfig {
	if spec == nil || !spec.EnabledValue() {
		return plugins
	}
	merged := make(map[string]kaiv1.PluginConfig, len(plugins)+1)
	for name, cfg := range plugins {
		merged[name] = cfg
	}
	existing := merged["recorder"]
	priority := recorderv1.PluginPriority
	if existing.Priority != nil {
		priority = *existing.Priority
	}
	args := map[string]string{}
	for key, value := range existing.Arguments {
		args[key] = value
	}
	args["address"] = spec.Address
	args["cycleBuffer"] = strconv.Itoa(*spec.CycleBufferSize)
	args["lifecycleBuffer"] = strconv.Itoa(*spec.LifecycleBufferSize)
	if kaiConfig != nil {
		args["clusterName"] = kaiConfig.Name
	}
	if shard != nil {
		args["shardName"] = shard.Name
	}
	enabled := true
	merged["recorder"] = kaiv1.PluginConfig{
		Enabled:   &enabled,
		Priority:  &priority,
		Arguments: args,
	}
	return merged
}

func appendRecorderSidecar(deployment *appsv1.Deployment, spec *recorderv1.RecorderSpec) {
	if deployment == nil || spec == nil || !spec.EnabledValue() {
		return
	}
	pullPolicy := corev1.PullIfNotPresent
	if spec.Image != nil && spec.Image.PullPolicy != nil {
		pullPolicy = *spec.Image.PullPolicy
	}
	image := "recorder"
	if spec.Image != nil {
		image = spec.Image.Url()
	}
	resources := corev1.ResourceRequirements{}
	if spec.Resources != nil {
		resources = *spec.Resources
	}
	rollInterval := recorderv1.DefaultRollInterval.String()
	rollBytes := int64(recorderv1.DefaultRollSizeMB) << 20
	if spec.Batch != nil && spec.Batch.RollInterval != nil {
		rollInterval = spec.Batch.RollInterval.Duration.String()
	}
	if spec.Batch != nil && spec.Batch.RollSizeMB != nil {
		rollBytes = int64(*spec.Batch.RollSizeMB) << 20
	}
	deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, corev1.Container{
		Name:            "recorder",
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Args: []string{
			"--listen=" + spec.Address,
			"--roll-interval=" + rollInterval,
			"--roll-bytes=" + strconv.FormatInt(rollBytes, 10),
			"--hmac-key-file=/var/run/kai-recorder/hmac-key",
			"--dir=/var/lib/kai-recorder",
		},
		Resources: resources,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "recorder-storage", MountPath: "/var/run/kai-recorder", ReadOnly: true},
			{Name: "recorder-data", MountPath: "/var/lib/kai-recorder"},
		},
	})
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes,
		corev1.Volume{
			Name: "recorder-storage",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: spec.StorageSecretName},
			},
		},
		corev1.Volume{
			Name: "recorder-data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	)
}
