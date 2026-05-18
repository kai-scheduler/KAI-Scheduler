// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2alpha2

const (
	// ReclaimQueueAtFairShare means reclaim was skipped because the reclaimer's queue
	// would exceed its fair share if the requested resources were granted.
	ReclaimQueueAtFairShare UnschedulableReason = "ReclaimQueueAtFairShare"

	// ReclaimNoEligibleVictims means reclaim was attempted but no candidate victim
	// jobs survived the victim filters (e.g. all are protected by min-runtime).
	ReclaimNoEligibleVictims UnschedulableReason = "ReclaimNoEligibleVictims"

	// ReclaimNoFeasibleNodes means reclaim was attempted but no nodes are feasible
	// for the reclaimer.
	ReclaimNoFeasibleNodes UnschedulableReason = "ReclaimNoFeasibleNodes"

	// ReclaimNoSolutionFound means the solver could not find a feasible reclaim
	// scenario for the job.
	ReclaimNoSolutionFound UnschedulableReason = "ReclaimNoSolutionFound"

	// PreemptOverQueueQuota means preemption was skipped because allocating the
	// preemptor would exceed the queue's non-preemptible quota.
	PreemptOverQueueQuota UnschedulableReason = "PreemptOverQueueQuota"

	// PreemptNoEligibleVictims means preemption was attempted but no candidate
	// victim jobs survived the victim filters.
	PreemptNoEligibleVictims UnschedulableReason = "PreemptNoEligibleVictims"

	// PreemptNoFeasibleNodes means preemption was attempted but no nodes are
	// feasible for the preemptor.
	PreemptNoFeasibleNodes UnschedulableReason = "PreemptNoFeasibleNodes"

	// PreemptNoSolutionFound means the solver could not find a feasible preempt
	// scenario for the job.
	PreemptNoSolutionFound UnschedulableReason = "PreemptNoSolutionFound"

	// ConsolidationDisabled means consolidation is disabled at the scheduler
	// level (maxNumberConsolidationPreemptees=0).
	ConsolidationDisabled UnschedulableReason = "ConsolidationDisabled"

	// ConsolidationInsufficientGPUs means consolidation was skipped because the
	// cluster does not have enough allocatable GPUs for the job.
	ConsolidationInsufficientGPUs UnschedulableReason = "ConsolidationInsufficientGPUs"

	// ConsolidationNoSolutionFound means the solver could not find a feasible
	// consolidation scenario for the job.
	ConsolidationNoSolutionFound UnschedulableReason = "ConsolidationNoSolutionFound"
)
