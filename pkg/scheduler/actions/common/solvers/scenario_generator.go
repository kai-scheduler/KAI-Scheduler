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
	VictimsQueue         *utils.JobsOrderByQueues
	FeasibleNodes        map[string]*node_info.NodeInfo
	ProbeK               int
}

func (ctx *SolveContext) Action() framework.ActionType {
	return ctx.ActionType
}

func NewNodeLocalGreedyGenerator(_ framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
	return nil
}

func NewMultiNodeGangGenerator(_ framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
	return nil
}
