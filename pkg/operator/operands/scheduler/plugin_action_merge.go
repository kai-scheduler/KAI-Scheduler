// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"cmp"
	"slices"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
)

func resolvePlugins(plugins map[string]kaiv1.PluginConfig) []conf.PluginOption {
	var result []conf.PluginOption
	for name, cfg := range plugins {
		if *cfg.Enabled {
			result = append(result, conf.PluginOption{
				Name:      name,
				Arguments: cfg.Arguments,
			})
		}
	}

	slices.SortFunc(result, func(a, b conf.PluginOption) int {
		if pa, pb := *plugins[a.Name].Priority, *plugins[b.Name].Priority; pa != pb {
			return pb - pa // descending
		}
		return cmp.Compare(a.Name, b.Name)
	})

	return result
}

func resolveActions(actions map[string]kaiv1.ActionConfig) []string {
	var names []string
	for name, cfg := range actions {
		if *cfg.Enabled {
			names = append(names, name)
		}
	}

	slices.SortFunc(names, func(a, b string) int {
		if pa, pb := *actions[a].Priority, *actions[b].Priority; pa != pb {
			return pb - pa // descending
		}
		return cmp.Compare(a, b)
	})

	return names
}
