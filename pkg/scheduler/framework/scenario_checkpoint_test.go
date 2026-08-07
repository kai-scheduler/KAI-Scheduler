// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScenarioCheckpointStoreIsBounded(t *testing.T) {
	store := newScenarioCheckpointStore(1)
	first := ScenarioCheckpointKey{Action: Reclaim, JobUID: "first"}
	second := ScenarioCheckpointKey{Action: Reclaim, JobUID: "second"}

	store.Save(first, ScenarioCheckpoint{GeneratorName: "first"})
	store.Save(second, ScenarioCheckpoint{GeneratorName: "second"})

	_, foundFirst := store.Load(first)
	checkpoint, foundSecond := store.Load(second)
	require.False(t, foundFirst)
	require.True(t, foundSecond)
	require.Equal(t, "second", checkpoint.GeneratorName)
}
