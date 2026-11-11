// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fullnodefirst

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestPluginRegistersFullNodeFirstGenerator(t *testing.T) {
	ssn := &framework.Session{}
	plugin := New(nil)

	plugin.OnSessionOpen(ssn)

	require.Equal(t, Name, plugin.Name())
	require.Len(t, ssn.ScenarioGeneratorRegistrations, 1)
	require.Equal(t, GeneratorName, ssn.ScenarioGeneratorRegistrations[0].Name)
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

func TestParseThreshold(t *testing.T) {
	cases := []struct {
		name string
		args framework.PluginArguments
		want float64
	}{
		{name: "unset uses the default", args: nil, want: DefaultFullNodeThreshold},
		{name: "valid value", args: framework.PluginArguments{ThresholdArgument: "0.95"}, want: 0.95},
		{name: "one is allowed", args: framework.PluginArguments{ThresholdArgument: "1"}, want: 1.0},
		{name: "just above half is allowed", args: framework.PluginArguments{ThresholdArgument: "0.51"}, want: 0.51},
		{name: "half is rejected", args: framework.PluginArguments{ThresholdArgument: "0.5"}, want: DefaultFullNodeThreshold},
		{name: "below half is rejected", args: framework.PluginArguments{ThresholdArgument: "0.3"}, want: DefaultFullNodeThreshold},
		{name: "above one is rejected", args: framework.PluginArguments{ThresholdArgument: "1.2"}, want: DefaultFullNodeThreshold},
		{name: "negative is rejected", args: framework.PluginArguments{ThresholdArgument: "-1"}, want: DefaultFullNodeThreshold},
		{name: "NaN is rejected", args: framework.PluginArguments{ThresholdArgument: "NaN"}, want: DefaultFullNodeThreshold},
		{name: "unparsable falls back", args: framework.PluginArguments{ThresholdArgument: "abc"}, want: DefaultFullNodeThreshold},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, parseThreshold(tc.args))
			require.Equal(t, tc.want, New(tc.args).(*fullNodeFirstPlugin).threshold)
		})
	}
}
