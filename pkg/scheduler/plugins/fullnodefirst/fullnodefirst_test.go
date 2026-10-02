// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fullnodefirst

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestPluginRegistersFullNodeFirstGenerator(t *testing.T) {
	ssn := &framework.Session{}
	plugin := New(nil)

	plugin.OnSessionOpen(ssn)

	require.Equal(t, Name, plugin.Name())
	require.Len(t, ssn.ScenarioGeneratorRegistrations, 1)
	require.Equal(t, constants.GeneratorFullNodeFirst, ssn.ScenarioGeneratorRegistrations[0].Name)
}

func TestPluginDoesNotDoubleRegister(t *testing.T) {
	ssn := &framework.Session{}
	plugin := New(nil)

	plugin.OnSessionOpen(ssn)
	plugin.OnSessionOpen(ssn)

	require.Len(t, ssn.ScenarioGeneratorRegistrations, 1)
}

func TestGeneratorConstructorRejectsInvalidContext(t *testing.T) {
	require.Nil(t, NewFullNodeFirstGenerator(nil))
}
