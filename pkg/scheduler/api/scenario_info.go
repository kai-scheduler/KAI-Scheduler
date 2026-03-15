// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

type ScenarioInfo interface {
	GetPreemptor() *podgroup_info.PodGroupInfo
	GetVictims() map[common_info.PodGroupID]*VictimInfo
}

type VictimInfo struct {
	// Job is the original job that is being evicted, as it exists in the session
	Job *podgroup_info.PodGroupInfo
	// Tasks is the list of tasks that are being evicted, as they exist in the session
	Tasks []*pod_info.PodInfo
}
