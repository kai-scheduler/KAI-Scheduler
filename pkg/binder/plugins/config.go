// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"cmp"
	"slices"

	"k8s.io/utils/ptr"

	kaiv1binder "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/binder"
)

const (
	VolumeBindingPluginName    = kaiv1binder.VolumeBindingPluginName
	DynamicResourcesPluginName = kaiv1binder.DynamicResourcesPluginName
	GPUSharingPluginName       = kaiv1binder.GPUSharingPluginName

	BindTimeoutSecondsArgument = kaiv1binder.BindTimeoutSecondsArgument
	CDIEnabledArgument         = kaiv1binder.CDIEnabledArgument

	DefaultBindTimeoutSeconds = kaiv1binder.DefaultBindTimeoutSeconds
	DefaultCDIEnabled         = kaiv1binder.DefaultCDIEnabled
)

type Config map[string]kaiv1binder.PluginConfig

type PluginOption struct {
	Name      string
	Arguments map[string]string
}

func DefaultConfig(volumeBindingTimeoutSeconds int, cdiEnabled bool) Config {
	return FromAPIConfig(kaiv1binder.DefaultPluginsConfig(volumeBindingTimeoutSeconds, cdiEnabled))
}

func FromAPIConfig(config map[string]kaiv1binder.PluginConfig) Config {
	result := make(Config, len(config))
	for name, pluginConfig := range config {
		pc := kaiv1binder.PluginConfig{}
		if pluginConfig.Enabled != nil {
			pc.Enabled = ptr.To(*pluginConfig.Enabled)
		}
		if pluginConfig.Priority != nil {
			pc.Priority = ptr.To(*pluginConfig.Priority)
		}
		pc.Arguments = copyArguments(pluginConfig.Arguments)
		result[name] = pc
	}
	return result
}

func ResolveConfig(defaults, overrides Config) Config {
	resolved := defaults.deepCopy()
	for name, override := range overrides {
		existing, found := resolved[name]
		if !found {
			existing = kaiv1binder.PluginConfig{
				Enabled:   ptr.To(true),
				Priority:  ptr.To(0),
				Arguments: map[string]string{},
			}
		}
		if override.Enabled != nil {
			existing.Enabled = ptr.To(*override.Enabled)
		}
		if override.Priority != nil {
			existing.Priority = ptr.To(*override.Priority)
		}
		if override.Arguments != nil {
			existing.Arguments = copyArguments(override.Arguments)
		}
		resolved[name] = existing
	}

	return resolved
}

func (c Config) EnabledOptions() []PluginOption {
	options := make([]PluginOption, 0, len(c))
	for name, config := range c {
		if config.Enabled != nil && !*config.Enabled {
			continue
		}
		options = append(options, PluginOption{
			Name:      name,
			Arguments: copyArguments(config.Arguments),
		})
	}

	slices.SortFunc(options, func(a, b PluginOption) int {
		priorityA := ptr.Deref(c[a.Name].Priority, 0)
		priorityB := ptr.Deref(c[b.Name].Priority, 0)
		if priorityA != priorityB {
			return cmp.Compare(priorityB, priorityA)
		}
		return cmp.Compare(a.Name, b.Name)
	})

	return options
}

func (c Config) deepCopy() Config {
	result := make(Config, len(c))
	for name, config := range c {
		if config.Enabled != nil {
			config.Enabled = ptr.To(*config.Enabled)
		}
		if config.Priority != nil {
			config.Priority = ptr.To(*config.Priority)
		}
		config.Arguments = copyArguments(config.Arguments)
		result[name] = config
	}

	return result
}

func copyArguments(arguments map[string]string) map[string]string {
	if arguments == nil {
		return nil
	}
	result := make(map[string]string, len(arguments))
	for key, value := range arguments {
		result[key] = value
	}
	return result
}
