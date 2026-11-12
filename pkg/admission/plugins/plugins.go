// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Plugin interface {
	Name() string
	Validate(context.Context, *v1.Pod, *v1.Pod) error
	Mutate(*v1.Pod) error
}

// PluginError is the error returned by KaiAdmissionPlugins when a plugin rejects a pod.
// It reads like the plugin's own error and unwraps to it, and additionally names the rejecting plugin.
type PluginError struct {
	Plugin string
	Err    error
}

func (e *PluginError) Error() string {
	return e.Err.Error()
}

func (e *PluginError) Unwrap() error {
	return e.Err
}

type KaiAdmissionPlugins struct {
	plugins []Plugin
}

func New() *KaiAdmissionPlugins {
	return &KaiAdmissionPlugins{
		plugins: []Plugin{},
	}
}

func (bp *KaiAdmissionPlugins) RegisterPlugin(plugin Plugin) {
	logger := log.FromContext(context.Background())
	logger.Info("registering plugin", "plugin", plugin.Name())
	bp.plugins = append(bp.plugins, plugin)
}

func (bp *KaiAdmissionPlugins) Validate(ctx context.Context, oldPod, pod *v1.Pod) error {
	for _, p := range bp.plugins {
		err := p.Validate(ctx, oldPod, pod)
		if err != nil {
			logger := log.FromContext(context.Background())
			logger.Error(err, "pod validation failed for pod",
				"namespace", pod.Namespace, "name", pod.Name, "plugin", p.Name())
			return &PluginError{Plugin: p.Name(), Err: err}
		}
	}
	return nil
}

func (bp *KaiAdmissionPlugins) Mutate(pod *v1.Pod) error {
	for _, p := range bp.plugins {
		err := p.Mutate(pod)
		if err != nil {
			logger := log.FromContext(context.Background())
			logger.Error(err, "pod mutation failed for pod",
				"namespace", pod.Namespace, "name", pod.Name, "plugin", p.Name())
			return &PluginError{Plugin: p.Name(), Err: err}
		}
	}
	return nil
}
