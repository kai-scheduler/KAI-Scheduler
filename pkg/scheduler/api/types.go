// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// PredicateFn is used to predicate node for task.
type PredicateFn func(*pod_info.PodInfo, *podgroup_info.PodGroupInfo, *node_info.NodeInfo) error

// PrePredicateFn is used to prepare for predicate on pod.
type PrePredicateFn func(*pod_info.PodInfo, *podgroup_info.PodGroupInfo) error

// CanReclaimResourcesFn is a function that determines if a reclaimer can get more resources
type CanReclaimResourcesFn func(pendingJob *podgroup_info.PodGroupInfo) bool

// VictimFilterFn is a function which filters out jobs that cannot a victim candidate for a specific reclaimer/preemptor.
type VictimFilterFn func(pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool

// ScenarioValidatorFn is a function which determines the validity of a scenario.
type ScenarioValidatorFn func(scenario ScenarioInfo) bool

// QueueResource is a function which returns the resource of a queue.
type QueueResource func(*queue_info.QueueInfo) *resource_info.ResourceRequirements

// IsJobOverCapacityFn is a function which determines if a job is over queue capacity.
type IsJobOverCapacityFn func(job *podgroup_info.PodGroupInfo, tasksToAllocate []*pod_info.PodInfo) *SchedulableResult

// IsTaskAllocationOverCapacityFn is a function which determines if a task is over capacity.
type IsTaskAllocationOverCapacityFn func(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) *SchedulableResult

// GpuOrderFn is used to get priority score for a gpu for a particular task.
type GpuOrderFn func(*pod_info.PodInfo, *node_info.NodeInfo, string) (float64, error)

// NodeOrderFn is used to get priority score for a node for a particular task.
type NodeOrderFn func(*pod_info.PodInfo, *node_info.NodeInfo) (float64, error)

// NodePreOrderFn is used for pre-calculations on the feasible nodes for pods.
// Outputs of these calculations will be used in the NodeOrderFn
type NodePreOrderFn func(*pod_info.PodInfo, []*node_info.NodeInfo) error

// OnJobSolutionStartFn is used for notifying on job solution (and scenario simulations) start
type OnJobSolutionStartFn func()

// CompareQueueFn is used to compare two queues for ordering based on their jobs and victims.
type CompareQueueFn func(
	lQ, rQ *queue_info.QueueInfo,
	lJob, rJob *podgroup_info.PodGroupInfo,
	lVictims, rVictims []*podgroup_info.PodGroupInfo,
	minNodeGPUMemory int64,
) int

type SchedulableResult struct {
	IsSchedulable bool
	Reason        v2alpha2.UnschedulableReason
	Message       string
	Details       *v2alpha2.UnschedulableExplanationDetails
}
