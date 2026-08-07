// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestScenarioCheckpointDiscardedWhenInputsChange(t *testing.T) {
	ctx, _, scenarioToResume := newScenarioPortfolioTestContext(t, framework.Reclaim)
	ctx.ProbeK = 1
	ctx.Session.ScenarioCheckpointStore = framework.NewScenarioCheckpointStore()
	saveScenarioCheckpoint(ctx, "checkpointed", fingerprintScenario(scenarioToResume), SearchResultDeadlineExhausted)

	checkpoint := loadScenarioCheckpoint(ctx, "checkpointed")
	require.NotNil(t, checkpoint)
	require.Equal(t, [32]byte(fingerprintScenario(scenarioToResume)), checkpoint.Cursor)

	ctx.FeasibleNodes["node-2"] = &node_info.NodeInfo{Name: "node-2"}
	require.Nil(t, loadScenarioCheckpoint(ctx, "checkpointed"))
	_, found := ctx.Session.ScenarioCheckpointStore.Load(scenarioCheckpointKey(ctx))
	require.False(t, found)
}

func TestScenarioCheckpointSkipsEarlierGeneratorsAtCheckpointProbe(t *testing.T) {
	ctx, _, scenarioToResume := newScenarioPortfolioTestContext(t, framework.Reclaim)
	ctx.ProbeK = 1
	ctx.Session.ScenarioCheckpointStore = framework.NewScenarioCheckpointStore()
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(&portfolioTestGenerator{name: "first"}))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(&portfolioTestGenerator{name: "second"}))
	saveScenarioCheckpoint(ctx, "second", fingerprintScenario(scenarioToResume), SearchResultDeadlineExhausted)

	require.True(t, checkpointSkipsGenerator(ctx, "first"))
	require.False(t, checkpointSkipsGenerator(ctx, "second"))
}
