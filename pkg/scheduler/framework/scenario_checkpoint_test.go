// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScenarioCheckpointStoreRetainsExistingEntriesWhenFull(t *testing.T) {
	store := newScenarioCheckpointStore(1)
	first := ScenarioCheckpointKey{Action: Reclaim, JobUID: "first"}
	second := ScenarioCheckpointKey{Action: Reclaim, JobUID: "second"}

	store.Save(first, ScenarioCheckpoint{GeneratorName: "first"})
	store.Save(second, ScenarioCheckpoint{GeneratorName: "second"})
	store.Save(first, ScenarioCheckpoint{GeneratorName: "first-updated"})

	checkpoint, foundFirst := store.Load(first)
	_, foundSecond := store.Load(second)
	require.True(t, foundFirst)
	require.Equal(t, "first-updated", checkpoint.GeneratorName)
	require.False(t, foundSecond)
}
