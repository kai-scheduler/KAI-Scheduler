// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

// Scenario is a candidate plan: pending tasks to allocate, and the victim
// set to evict. No "recorded" carry-forward, no per-node bucketing in the
// type — those are internal details of generator implementations.
type Scenario struct {
	// Preemptor is the job whose pending tasks we are trying to place.
	Preemptor *podgroup_info.PodGroupInfo

	// Pending are the pods of Preemptor to be placed in this scenario.
	Pending []*pod_info.PodInfo

	// Victims are the pods to evict to make room for Pending.
	Victims []*pod_info.PodInfo
}
