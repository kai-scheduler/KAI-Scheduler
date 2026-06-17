// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenariogenerators

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/scenariosearch"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestNodeLocalGreedyPluginRegistersNodeLocalGenerator(t *testing.T) {
	ssn := &framework.Session{}
	plugin := NewNodeLocalGreedy(nil)

	plugin.OnSessionOpen(ssn)

	require.Equal(t, NodeLocalGreedyName, plugin.Name())
	require.Len(t, ssn.ScenarioGeneratorRegistrations, 1)
	require.Equal(t, scenariosearch.GeneratorNodeLocalGreedy, ssn.ScenarioGeneratorRegistrations[0].Name)
	require.Contains(t, ssn.ScenarioGeneratorRegistrations[0].Actions, framework.Reclaim)
	require.Contains(t, ssn.ScenarioGeneratorRegistrations[0].Actions, framework.Preempt)
	require.Contains(t, ssn.ScenarioGeneratorRegistrations[0].Actions, framework.Consolidation)
}

func TestMultiNodeGangPluginRegistersMultiNodeGangGenerator(t *testing.T) {
	ssn := &framework.Session{}
	plugin := NewMultiNodeGang(nil)

	plugin.OnSessionOpen(ssn)

	require.Equal(t, MultiNodeGangName, plugin.Name())
	require.Len(t, ssn.ScenarioGeneratorRegistrations, 1)
	require.Equal(t, scenariosearch.GeneratorMultiNodeGang, ssn.ScenarioGeneratorRegistrations[0].Name)
	require.Contains(t, ssn.ScenarioGeneratorRegistrations[0].Actions, framework.Reclaim)
	require.Contains(t, ssn.ScenarioGeneratorRegistrations[0].Actions, framework.Preempt)
	require.Contains(t, ssn.ScenarioGeneratorRegistrations[0].Actions, framework.Consolidation)
}

func TestSeparatePluginsPreserveSessionRegistrationOrder(t *testing.T) {
	ssn := &framework.Session{}

	NewNodeLocalGreedy(nil).OnSessionOpen(ssn)
	NewMultiNodeGang(nil).OnSessionOpen(ssn)

	require.Len(t, ssn.ScenarioGeneratorRegistrations, 2)
	require.Equal(t, scenariosearch.GeneratorNodeLocalGreedy, ssn.ScenarioGeneratorRegistrations[0].Name)
	require.Equal(t, scenariosearch.GeneratorMultiNodeGang, ssn.ScenarioGeneratorRegistrations[1].Name)
}

func TestLegacyPluginDoesNotDuplicateConcreteRegistrations(t *testing.T) {
	tests := []struct {
		name    string
		plugins []framework.Plugin
	}{
		{
			name: "legacy after concrete plugins",
			plugins: []framework.Plugin{
				NewNodeLocalGreedy(nil),
				NewMultiNodeGang(nil),
				NewLegacy(nil),
			},
		},
		{
			name: "legacy before concrete plugins",
			plugins: []framework.Plugin{
				NewLegacy(nil),
				NewNodeLocalGreedy(nil),
				NewMultiNodeGang(nil),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := &framework.Session{}

			for _, plugin := range tt.plugins {
				plugin.OnSessionOpen(ssn)
			}

			require.Len(t, ssn.ScenarioGeneratorRegistrations, 2)
			require.Equal(t, scenariosearch.GeneratorNodeLocalGreedy, ssn.ScenarioGeneratorRegistrations[0].Name)
			require.Equal(t, scenariosearch.GeneratorMultiNodeGang, ssn.ScenarioGeneratorRegistrations[1].Name)
		})
	}
}
