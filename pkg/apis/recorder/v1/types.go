// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package v1 holds a temporary copy of the recorder API.
//
// These types belong in github.com/kai-scheduler/api, not in this repository:
//   - RecorderSpec, and a Recorder *RecorderSpec field on kai/v1.SchedulingShardSpec
//   - the same Recorder field on kai/v1/scheduler.Scheduler, so Config.spec.scheduler can carry defaults
//
// Shipping that change means a new api module release, a go.mod bump in this repo, and regenerating
// deployments/kai-scheduler/crds/kai.scheduler_schedulingshards.yaml and kai.scheduler_configs.yaml.
// Until then the operator decodes spec.recorder from the live object JSON, because the generated
// structs drop unknown fields. The CRD schemas in this repo already allow the field so an apiserver
// will store it.
package v1

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/api/kai/v1/common"
)

const (
	DefaultAddress             = "127.0.0.1:9095"
	DefaultStorageSecretName   = "kai-recorder-storage"
	DefaultHashKeySecretName   = "kai-recorder-hash-key"
	HMACKeyDataKey             = "hmac-key"
	DefaultCycleBufferSize     = 4
	DefaultLifecycleBufferSize = 1024
	DefaultRollInterval        = 5 * time.Minute
	DefaultRollSizeMB          = 64
	PluginPriority             = 450
)

// RecorderSpec configures the scheduling recorder for one shard, or the defaults inherited from Config.
type RecorderSpec struct {
	Enabled             *bool                        `json:"enabled,omitempty"`
	Image               *common.Image                `json:"image,omitempty"`
	Resources           *corev1.ResourceRequirements `json:"resources,omitempty"`
	StorageSecretName   string                       `json:"storageSecretName,omitempty"`
	HashKeySecretName   string                       `json:"hashKeySecretName,omitempty"`
	Batch               *RecorderBatch               `json:"batch,omitempty"`
	Address             string                       `json:"address,omitempty"`
	CycleBufferSize     *int                         `json:"cycleBufferSize,omitempty"`
	LifecycleBufferSize *int                         `json:"lifecycleBufferSize,omitempty"`
}

// RecorderBatch controls how the sidecar rolls captured records.
type RecorderBatch struct {
	RollInterval *metav1.Duration `json:"rollInterval,omitempty"`
	RollSizeMB   *int             `json:"rollSizeMB,omitempty"`
}

// EnabledValue reports whether the sidecar and plugin should be installed.
func (s *RecorderSpec) EnabledValue() bool {
	return s != nil && s.Enabled != nil && *s.Enabled
}

// DeepCopy returns a copy safe to mutate.
func (s *RecorderSpec) DeepCopy() *RecorderSpec {
	if s == nil {
		return nil
	}
	out := *s
	if s.Enabled != nil {
		out.Enabled = ptr.To(*s.Enabled)
	}
	if s.Image != nil {
		image := *s.Image
		out.Image = &image
	}
	if s.Resources != nil {
		resources := s.Resources.DeepCopy()
		out.Resources = resources
	}
	if s.Batch != nil {
		batch := *s.Batch
		if s.Batch.RollInterval != nil {
			batch.RollInterval = &metav1.Duration{Duration: s.Batch.RollInterval.Duration}
		}
		if s.Batch.RollSizeMB != nil {
			batch.RollSizeMB = ptr.To(*s.Batch.RollSizeMB)
		}
		out.Batch = &batch
	}
	if s.CycleBufferSize != nil {
		out.CycleBufferSize = ptr.To(*s.CycleBufferSize)
	}
	if s.LifecycleBufferSize != nil {
		out.LifecycleBufferSize = ptr.To(*s.LifecycleBufferSize)
	}
	return &out
}

// Merge overlays shard onto the config defaults. Nil fields do not override.
func Merge(defaults, shard *RecorderSpec) *RecorderSpec {
	out := &RecorderSpec{}
	apply(out, defaults)
	apply(out, shard)
	return out
}

func apply(dst, src *RecorderSpec) {
	if src == nil {
		return
	}
	if src.Enabled != nil {
		dst.Enabled = ptr.To(*src.Enabled)
	}
	if src.Image != nil {
		image := *src.Image
		dst.Image = &image
	}
	if src.Resources != nil {
		dst.Resources = src.Resources.DeepCopy()
	}
	if src.StorageSecretName != "" {
		dst.StorageSecretName = src.StorageSecretName
	}
	if src.HashKeySecretName != "" {
		dst.HashKeySecretName = src.HashKeySecretName
	}
	if src.Batch != nil {
		if dst.Batch == nil {
			dst.Batch = &RecorderBatch{}
		}
		if src.Batch.RollInterval != nil {
			dst.Batch.RollInterval = &metav1.Duration{Duration: src.Batch.RollInterval.Duration}
		}
		if src.Batch.RollSizeMB != nil {
			dst.Batch.RollSizeMB = ptr.To(*src.Batch.RollSizeMB)
		}
	}
	if src.Address != "" {
		dst.Address = src.Address
	}
	if src.CycleBufferSize != nil {
		dst.CycleBufferSize = ptr.To(*src.CycleBufferSize)
	}
	if src.LifecycleBufferSize != nil {
		dst.LifecycleBufferSize = ptr.To(*src.LifecycleBufferSize)
	}
}

// ApplyDefaults fills every unset field. imageFallback supplies registry and tag when the recorder image omits them.
func ApplyDefaults(spec *RecorderSpec, imageFallback *common.Image) {
	if spec.Enabled == nil {
		spec.Enabled = ptr.To(false)
	}
	if spec.Address == "" {
		spec.Address = DefaultAddress
	}
	if spec.StorageSecretName == "" {
		spec.StorageSecretName = DefaultStorageSecretName
	}
	if spec.HashKeySecretName == "" {
		spec.HashKeySecretName = DefaultHashKeySecretName
	}
	if spec.CycleBufferSize == nil {
		spec.CycleBufferSize = ptr.To(DefaultCycleBufferSize)
	}
	if spec.LifecycleBufferSize == nil {
		spec.LifecycleBufferSize = ptr.To(DefaultLifecycleBufferSize)
	}
	if spec.Batch == nil {
		spec.Batch = &RecorderBatch{}
	}
	if spec.Batch.RollInterval == nil {
		spec.Batch.RollInterval = &metav1.Duration{Duration: DefaultRollInterval}
	}
	if spec.Batch.RollSizeMB == nil {
		spec.Batch.RollSizeMB = ptr.To(DefaultRollSizeMB)
	}
	if spec.Image == nil {
		spec.Image = &common.Image{}
	}
	if spec.Image.Name == nil || *spec.Image.Name == "" {
		spec.Image.Name = ptr.To("recorder")
	}
	if imageFallback != nil {
		if spec.Image.Repository == nil {
			spec.Image.Repository = imageFallback.Repository
		}
		if spec.Image.Tag == nil {
			spec.Image.Tag = imageFallback.Tag
		}
		if spec.Image.PullPolicy == nil {
			spec.Image.PullPolicy = imageFallback.PullPolicy
		}
	}
	spec.Image.SetDefaultsWhereNeeded()
	if spec.Resources == nil {
		spec.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}
	}
}
