// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type SolveContext struct {
	Session              *framework.Session
	ActionType           framework.ActionType
	PartialPendingJob    *podgroup_info.PodGroupInfo
	RecordedVictimsJobs  []*podgroup_info.PodGroupInfo
	RecordedVictimsTasks []*pod_info.PodInfo
	GenerateVictimsQueue GenerateVictimsQueue
	VictimsQueue         *utils.JobsOrderByQueues
	FeasibleNodes        map[string]*node_info.NodeInfo
	ProbeK               int
}

func (ctx *SolveContext) Action() framework.ActionType {
	return ctx.ActionType
}

// ValidateScenarioGeneratorContext extracts the solver context required by scenario generator plugins.
func ValidateScenarioGeneratorContext(ctx framework.ScenarioGeneratorContext) (*SolveContext, GenerateVictimsQueue, bool) {
	solveCtx, ok := ctx.(*SolveContext)
	if !ok || solveCtx == nil || solveCtx.Session == nil || solveCtx.Session.ClusterInfo == nil ||
		solveCtx.Session.ClusterInfo.Nodes == nil || solveCtx.Session.ClusterInfo.PodGroupInfos == nil ||
		solveCtx.PartialPendingJob == nil || solveCtx.FeasibleNodes == nil || solveCtx.GenerateVictimsQueue == nil {
		return nil, nil, false
	}

	return solveCtx, solveCtx.GenerateVictimsQueue, true
}
