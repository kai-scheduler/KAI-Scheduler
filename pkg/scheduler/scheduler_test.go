// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
)

func TestGetEvictionActionNames(t *testing.T) {
	actions.InitDefaultActions()
	configuredActions, err := conf_util.GetActionsFromConfig(&conf.SchedulerConfiguration{
		Actions: "allocate, reclaim, stalegangeviction",
	})
	require.NoError(t, err)

	require.Equal(t, []string{"reclaim", "stalegangeviction"}, getEvictionActionNames(configuredActions))
}
