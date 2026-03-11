// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
)

func TestSetDefaultPlugins_Binpack(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
	}
	spec.SetDefaultsWhereNeeded()

	// Verify base plugins are present and enabled
	for _, name := range []string{
		"predicates", "proportion", "priority", "nodeavailability",
		"resourcetype", "podaffinity", "elastic", "kubeflow", "ray",
		"subgrouporder", "taskorder", "nominatednode", "dynamicresources",
		"minruntime", "topology", "snapshot",
	} {
		p, found := spec.Plugins[name]
		require.True(t, found, "expected plugin %s to be present", name)
		assert.True(t, *p.Enabled, "expected plugin %s to be enabled", name)
		assert.NotNil(t, p.Priority, "expected plugin %s to have priority", name)
	}

	// Verify gpupack is present for binpack strategy
	_, found := spec.Plugins["gpupack"]
	assert.True(t, found, "expected gpupack for binpack strategy")

	// Verify gpusharingorder is present for binpack strategy
	_, found = spec.Plugins["gpusharingorder"]
	assert.True(t, found, "expected gpusharingorder for binpack strategy")

	// Verify nodeplacement has placement arguments
	np := spec.Plugins["nodeplacement"]
	assert.Equal(t, map[string]string{"gpu": "binpack", "cpu": "binpack"}, np.Arguments)
}

func TestSetDefaultPlugins_SpreadStrategy(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("spread"),
			CPU: ptr.To("binpack"),
		},
	}
	spec.SetDefaultsWhereNeeded()

	// Verify gpuspread is present
	_, found := spec.Plugins["gpuspread"]
	assert.True(t, found, "expected gpuspread for spread strategy")

	// Verify gpupack is present but disabled for spread strategy
	gpk, found := spec.Plugins["gpupack"]
	assert.True(t, found, "expected gpupack to be present")
	assert.False(t, *gpk.Enabled, "expected gpupack to be disabled for spread strategy")
	assert.Equal(t, 300, *gpk.Priority, "expected gpupack to have default priority")

	// Verify gpusharingorder is present but disabled for spread strategy
	gso, found := spec.Plugins["gpusharingorder"]
	assert.True(t, found, "expected gpusharingorder to be present")
	assert.False(t, *gso.Enabled, "expected gpusharingorder to be disabled for spread strategy")
	assert.Equal(t, 100, *gso.Priority, "expected gpusharingorder to have default priority")
}

func TestSetDefaultPlugins_WithKValue(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		KValue: ptr.To(1.5),
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
	}
	spec.SetDefaultsWhereNeeded()

	assert.Equal(t, map[string]string{"kValue": "1.5"}, spec.Plugins["proportion"].Arguments)
}

func TestSetDefaultPlugins_WithMinRuntime(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		MinRuntime: &kaiv1.MinRuntime{
			PreemptMinRuntime: ptr.To("5m"),
			ReclaimMinRuntime: ptr.To("10m"),
		},
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
	}
	spec.SetDefaultsWhereNeeded()

	assert.Equal(t, map[string]string{
		"defaultPreemptMinRuntime": "5m",
		"defaultReclaimMinRuntime": "10m",
	}, spec.Plugins["minruntime"].Arguments)
}

func TestSetDefaultActions_Binpack(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
	}
	spec.SetDefaultsWhereNeeded()

	for _, name := range []string{"allocate", "consolidation", "reclaim", "preempt", "stalegangeviction"} {
		a, found := spec.Actions[name]
		require.True(t, found, "expected action %s", name)
		assert.True(t, *a.Enabled)
	}
}

func TestSetDefaultActions_SpreadStrategy(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("spread"),
			CPU: ptr.To("binpack"),
		},
	}
	spec.SetDefaultsWhereNeeded()

	a, found := spec.Actions["consolidation"]
	require.True(t, found, "consolidation should be present")
	assert.False(t, *a.Enabled, "consolidation should be disabled for spread strategy")
	assert.Equal(t, 400, *a.Priority, "consolidation should have default priority")
}

func TestSetDefaultPlugins_NoOverridesPreservesDefaults(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
	}
	spec.SetDefaultsWhereNeeded()

	enabledCount := 0
	for _, p := range spec.Plugins {
		if p.Enabled != nil && *p.Enabled {
			enabledCount++
		}
	}

	// Re-run SetDefaults on same spec - should not change count
	spec.SetDefaultsWhereNeeded()

	newEnabledCount := 0
	for _, p := range spec.Plugins {
		if p.Enabled != nil && *p.Enabled {
			newEnabledCount++
		}
	}
	assert.Equal(t, enabledCount, newEnabledCount, "re-running defaults should not change count")
}

func TestSetDefaultPlugins_DisablePlugin(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Plugins: map[string]kaiv1.PluginConfig{
			"elastic": {Enabled: ptr.To(false)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	assert.False(t, *spec.Plugins["elastic"].Enabled)

	resolved := resolvePlugins(spec.Plugins)
	for _, p := range resolved {
		assert.NotEqual(t, "elastic", p.Name, "disabled plugin should not appear in resolved list")
	}
}

func TestSetDefaultPlugins_ChangePriority(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Plugins: map[string]kaiv1.PluginConfig{
			"predicates": {Priority: ptr.To(50)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	assert.Equal(t, 50, *spec.Plugins["predicates"].Priority)

	resolved := resolvePlugins(spec.Plugins)
	lastIdx := len(resolved) - 1
	for i, p := range resolved {
		if p.Name == "predicates" {
			assert.Greater(t, i, lastIdx/2, "predicates with low priority should be in bottom half")
			break
		}
	}
}

func TestSetDefaultPlugins_OverrideArguments(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		KValue: ptr.To(1.5),
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Plugins: map[string]kaiv1.PluginConfig{
			"proportion": {Arguments: map[string]string{"kValue": "3.0"}},
		},
	}
	spec.SetDefaultsWhereNeeded()

	assert.Equal(t, map[string]string{"kValue": "3.0"}, spec.Plugins["proportion"].Arguments)
}

func TestSetDefaultPlugins_AddCustomPlugin(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Plugins: map[string]kaiv1.PluginConfig{
			"myplugin": {Priority: ptr.To(1050), Arguments: map[string]string{"key": "val"}},
		},
	}
	spec.SetDefaultsWhereNeeded()

	p, found := spec.Plugins["myplugin"]
	require.True(t, found)
	assert.True(t, *p.Enabled)
	assert.Equal(t, 1050, *p.Priority)
	assert.Equal(t, map[string]string{"key": "val"}, p.Arguments)

	resolved := resolvePlugins(spec.Plugins)
	var mypluginIdx, subgrouporderIdx int
	for i, p := range resolved {
		if p.Name == "myplugin" {
			mypluginIdx = i
		}
		if p.Name == "subgrouporder" {
			subgrouporderIdx = i
		}
	}
	assert.Less(t, mypluginIdx, subgrouporderIdx, "myplugin should come before subgrouporder")
}

func TestSetDefaultActions_DisableAction(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Actions: map[string]kaiv1.ActionConfig{
			"preempt": {Enabled: ptr.To(false)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	assert.False(t, *spec.Actions["preempt"].Enabled)

	actionNames := resolveActions(spec.Actions)
	assert.NotContains(t, actionNames, "preempt")
}

func TestSetDefaultActions_ChangePriority(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Actions: map[string]kaiv1.ActionConfig{
			"reclaim": {Priority: ptr.To(600)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	actionNames := resolveActions(spec.Actions)
	var reclaimIdx, allocateIdx int
	for i, name := range actionNames {
		if name == "reclaim" {
			reclaimIdx = i
		}
		if name == "allocate" {
			allocateIdx = i
		}
	}
	assert.Less(t, reclaimIdx, allocateIdx, "reclaim with higher priority should come before allocate")
}

func TestSetDefaultActions_AddCustomAction(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Actions: map[string]kaiv1.ActionConfig{
			"myaction": {Priority: ptr.To(250)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	a, found := spec.Actions["myaction"]
	require.True(t, found)
	assert.True(t, *a.Enabled)
	assert.Equal(t, 250, *a.Priority)

	actionNames := resolveActions(spec.Actions)
	assert.Contains(t, actionNames, "myaction")

	var myactionIdx, reclaimIdx, preemptIdx int
	for i, name := range actionNames {
		switch name {
		case "myaction":
			myactionIdx = i
		case "reclaim":
			reclaimIdx = i
		case "preempt":
			preemptIdx = i
		}
	}
	assert.Less(t, reclaimIdx, myactionIdx, "reclaim should come before myaction")
	assert.Less(t, myactionIdx, preemptIdx, "myaction should come before preempt")
}

func TestSetDefaultActions_EnableConsolidationOnSpreadWithoutPriority(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("spread"),
			CPU: ptr.To("binpack"),
		},
		Actions: map[string]kaiv1.ActionConfig{
			"consolidation": {Enabled: ptr.To(true)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	a, found := spec.Actions["consolidation"]
	require.True(t, found)
	assert.True(t, *a.Enabled)
	assert.Equal(t, 400, *a.Priority, "should inherit default priority")

	actionNames := resolveActions(spec.Actions)
	assert.Contains(t, actionNames, "consolidation")
}

func TestSetDefaultPlugins_EnableGpuSharingOrderOnSpreadWithoutPriority(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("spread"),
			CPU: ptr.To("binpack"),
		},
		Plugins: map[string]kaiv1.PluginConfig{
			"gpusharingorder": {Enabled: ptr.To(true)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	p, found := spec.Plugins["gpusharingorder"]
	require.True(t, found)
	assert.True(t, *p.Enabled)
	assert.Equal(t, 100, *p.Priority, "should inherit default priority")

	resolved := resolvePlugins(spec.Plugins)
	var names []string
	for _, rp := range resolved {
		names = append(names, rp.Name)
	}
	assert.Contains(t, names, "gpusharingorder")
}

func TestSetDefaultActions_DisableConsolidationOnBinpack(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Actions: map[string]kaiv1.ActionConfig{
			"consolidation": {Enabled: ptr.To(false)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	a, found := spec.Actions["consolidation"]
	require.True(t, found)
	assert.False(t, *a.Enabled)

	actionNames := resolveActions(spec.Actions)
	assert.NotContains(t, actionNames, "consolidation")
}

func TestSetDefaultPlugins_SpreadNodesPackDevices(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("spread"),
			CPU: ptr.To("binpack"),
		},
		Plugins: map[string]kaiv1.PluginConfig{
			"gpuspread": {Enabled: ptr.To(false)},
			"gpupack":   {Enabled: ptr.To(true)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	gpk := spec.Plugins["gpupack"]
	assert.True(t, *gpk.Enabled)
	assert.Equal(t, 300, *gpk.Priority, "gpupack should have default priority")

	gps := spec.Plugins["gpuspread"]
	assert.False(t, *gps.Enabled)

	np := spec.Plugins["nodeplacement"]
	assert.Equal(t, "spread", np.Arguments["gpu"], "node-level strategy should remain spread")
}

func TestSetDefaultPlugins_PackNodesSpreadDevices(t *testing.T) {
	spec := kaiv1.SchedulingShardSpec{
		PlacementStrategy: &kaiv1.PlacementStrategy{
			GPU: ptr.To("binpack"),
			CPU: ptr.To("binpack"),
		},
		Plugins: map[string]kaiv1.PluginConfig{
			"gpupack":   {Enabled: ptr.To(false)},
			"gpuspread": {Enabled: ptr.To(true)},
		},
	}
	spec.SetDefaultsWhereNeeded()

	gps := spec.Plugins["gpuspread"]
	assert.True(t, *gps.Enabled)
	assert.Equal(t, 300, *gps.Priority, "gpuspread should have default priority")

	gpk := spec.Plugins["gpupack"]
	assert.False(t, *gpk.Enabled)

	np := spec.Plugins["nodeplacement"]
	assert.Equal(t, "binpack", np.Arguments["gpu"], "node-level strategy should remain binpack")
}

func TestResolvePlugins_Ordering(t *testing.T) {
	plugins := map[string]kaiv1.PluginConfig{
		"a": {Enabled: ptr.To(true), Priority: ptr.To(100)},
		"b": {Enabled: ptr.To(true), Priority: ptr.To(200)},
		"c": {Enabled: ptr.To(true), Priority: ptr.To(100)},
		"d": {Enabled: ptr.To(false), Priority: ptr.To(300)},
	}

	resolved := resolvePlugins(plugins)

	require.Len(t, resolved, 3, "disabled plugin should be filtered")
	assert.Equal(t, "b", resolved[0].Name, "highest priority first")
	assert.Equal(t, "a", resolved[1].Name, "alphabetical tiebreak")
	assert.Equal(t, "c", resolved[2].Name, "alphabetical tiebreak")
}

func TestResolveActions_Ordering(t *testing.T) {
	actions := map[string]kaiv1.ActionConfig{
		"x": {Enabled: ptr.To(true), Priority: ptr.To(50)},
		"y": {Enabled: ptr.To(true), Priority: ptr.To(100)},
		"z": {Enabled: ptr.To(true), Priority: ptr.To(50)},
		"w": {Enabled: ptr.To(false), Priority: ptr.To(200)},
	}

	actionNames := resolveActions(actions)

	require.Len(t, actionNames, 3, "disabled action should be filtered")
	assert.Equal(t, []string{"y", "x", "z"}, actionNames)
}
