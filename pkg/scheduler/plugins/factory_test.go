// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/scenariosearch"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scenariogenerators"
)

func TestInitDefaultPluginsRegistersLegacyScenarioGeneratorsPlugin(t *testing.T) {
	InitDefaultPlugins()

	builder, found := framework.GetPluginBuilder(scenariogenerators.LegacyName)
	require.True(t, found)

	plugin := builder(nil)
	require.Equal(t, scenariogenerators.LegacyName, plugin.Name())

	ssn := &framework.Session{}
	plugin.OnSessionOpen(ssn)

	require.Len(t, ssn.ScenarioGeneratorRegistrations, 2)
	require.Equal(t, scenariosearch.GeneratorNodeLocalGreedy, ssn.ScenarioGeneratorRegistrations[0].Name)
	require.Equal(t, scenariosearch.GeneratorMultiNodeGang, ssn.ScenarioGeneratorRegistrations[1].Name)
}
