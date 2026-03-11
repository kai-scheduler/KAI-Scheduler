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
	Validate(*v1.Pod) error
	Mutate(*v1.Pod) error
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

func (bp *KaiAdmissionPlugins) Validate(pod *v1.Pod) error {
	for _, p := range bp.plugins {
		err := p.Validate(pod)
		if err != nil {
			logger := log.FromContext(context.Background())
			logger.Error(err, "pod validation failed for pod",
				"namespace", pod.Namespace, "name", pod.Name, "plugin", p.Name())
			return err
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
			return err
		}
	}
	return nil
}
