// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"net/http"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

func (ssn *Session) AddGPUOrderFn(gof api.GpuOrderFn) {
	ssn.GpuOrderFns = append(ssn.GpuOrderFns, gof)
}

func (ssn *Session) AddNodePreOrderFn(npof api.NodePreOrderFn) {
	ssn.NodePreOrderFns = append(ssn.NodePreOrderFns, npof)
}

func (ssn *Session) AddNodeOrderFn(nof api.NodeOrderFn) {
	ssn.NodeOrderFns = append(ssn.NodeOrderFns, nof)
}

func (ssn *Session) AddPrePredicateFn(pf api.PrePredicateFn) {
	ssn.PrePredicateFns = append(ssn.PrePredicateFns, pf)
}

func (ssn *Session) AddPredicateFn(pf api.PredicateFn) {
	ssn.PredicateFns = append(ssn.PredicateFns, pf)
}

func (ssn *Session) AddJobOrderFn(jof common_info.CompareFn) {
	ssn.JobOrderFns = append(ssn.JobOrderFns, jof)
}

func (ssn *Session) AddTaskOrderFn(tof common_info.CompareFn) {
	ssn.TaskOrderFns = append(ssn.TaskOrderFns, tof)
}

func (ssn *Session) AddQueueOrderFn(qof api.CompareQueueFn) {
	ssn.QueueOrderFns = append(ssn.QueueOrderFns, qof)
}

func (ssn *Session) AddOnJobSolutionStartFn(jssf api.OnJobSolutionStartFn) {
	ssn.OnJobSolutionStartFns = append(ssn.OnJobSolutionStartFns, jssf)
}

func (ssn *Session) AddGetQueueAllocatedResourcesFn(of api.QueueResource) {
	ssn.GetQueueAllocatedResourcesFns = append(ssn.GetQueueAllocatedResourcesFns, of)
}

func (ssn *Session) AddPreemptVictimFilterFn(pf api.VictimFilterFn) {
	ssn.PreemptVictimFilterFns = append(ssn.PreemptVictimFilterFns, pf)
}

func (ssn *Session) AddCanReclaimResourcesFn(crf api.CanReclaimResourcesFn) {
	ssn.CanReclaimResourcesFns = append(ssn.CanReclaimResourcesFns, crf)
}

func (ssn *Session) AddReclaimScenarioValidatorFn(rf api.ScenarioValidatorFn) {
	ssn.ReclaimScenarioValidatorFns = append(ssn.ReclaimScenarioValidatorFns, rf)
}

func (ssn *Session) AddPreemptScenarioValidatorFn(rf api.ScenarioValidatorFn) {
	ssn.PreemptScenarioValidatorFns = append(ssn.PreemptScenarioValidatorFns, rf)
}

func (ssn *Session) AddReclaimVictimFilterFn(rf api.VictimFilterFn) {
	ssn.ReclaimVictimFilterFns = append(ssn.ReclaimVictimFilterFns, rf)
}

func (ssn *Session) CanReclaimResources(reclaimer *podgroup_info.PodGroupInfo) bool {
	for _, canReclaimFn := range ssn.CanReclaimResourcesFns {
		return canReclaimFn(reclaimer)
	}

	return false
}

func (ssn *Session) ReclaimVictimFilter(reclaimer *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
	for _, rf := range ssn.ReclaimVictimFilterFns {
		if !rf(reclaimer, victim) {
			return false
		}
	}

	return true
}

func (ssn *Session) ReclaimScenarioValidatorFn(scenario api.ScenarioInfo) bool {
	for _, rf := range ssn.ReclaimScenarioValidatorFns {
		if !rf(scenario) {
			return false
		}
	}

	return true
}

func (ssn *Session) PreemptVictimFilter(preemptor *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
	for _, pf := range ssn.PreemptVictimFilterFns {
		if !pf(preemptor, victim) {
			return false
		}
	}

	return true
}

func (ssn *Session) PreemptScenarioValidator(
	scenario api.ScenarioInfo,
) bool {
	for _, pf := range ssn.PreemptScenarioValidatorFns {
		if !pf(scenario) {
			return false
		}
	}

	return true
}

func (ssn *Session) AddHttpHandler(path string, handler func(http.ResponseWriter, *http.Request)) {
	if server == nil {
		return
	}
	err := server.registerPlugin(path, handler)
	if err != nil {
		log.InfraLogger.Errorf("Failed to register plugin %s: %v", path, err)
	}
}

func (ssn *Session) OnJobSolutionStart() {
	for _, jssf := range ssn.OnJobSolutionStartFns {
		jssf()
	}
}

func (ssn *Session) QueueDeservedResources(queue *queue_info.QueueInfo) *resource_info.ResourceRequirements {
	for _, of := range ssn.GetQueueDeservedResourcesFns {
		return of(queue)
	}

	return nil
}

func (ssn *Session) AddGetQueueDeservedResourcesFn(of api.QueueResource) {
	ssn.GetQueueDeservedResourcesFns = append(ssn.GetQueueDeservedResourcesFns, of)
}

func (ssn *Session) AddGetQueueFairShareFn(of api.QueueResource) {
	ssn.GetQueueFairShareFns = append(ssn.GetQueueFairShareFns, of)
}

func (ssn *Session) AddIsNonPreemptibleJobOverQueueQuotaFns(of api.IsJobOverCapacityFn) {
	ssn.IsNonPreemptibleJobOverQueueQuotaFns = append(ssn.IsNonPreemptibleJobOverQueueQuotaFns, of)
}

func (ssn *Session) AddIsJobOverCapacityFn(of api.IsJobOverCapacityFn) {
	ssn.IsJobOverCapacityFns = append(ssn.IsJobOverCapacityFns, of)
}

func (ssn *Session) AddIsTaskAllocationOnNodeOverCapacityFn(of api.IsTaskAllocationOverCapacityFn) {
	ssn.IsTaskAllocationOnNodeOverCapacityFns = append(ssn.IsTaskAllocationOnNodeOverCapacityFns, of)
}

func (ssn *Session) QueueFairShare(queue *queue_info.QueueInfo) *resource_info.ResourceRequirements {
	for _, of := range ssn.GetQueueFairShareFns {
		return of(queue)
	}

	return nil
}

func (ssn *Session) QueueAllocatedResources(queue *queue_info.QueueInfo) *resource_info.ResourceRequirements {
	for _, of := range ssn.GetQueueAllocatedResourcesFns {
		return of(queue)
	}

	return nil
}

func (ssn *Session) JobOrderFn(l, r interface{}) bool {
	for _, jof := range ssn.JobOrderFns {
		if j := jof(l, r); j != 0 {
			return j < 0
		}
	}

	// If no job order funcs, order job by CreationTimestamp first, then by UID.
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)
	if lv.CreationTimestamp.Equal(&rv.CreationTimestamp) {
		return lv.UID < rv.UID
	} else {
		return lv.CreationTimestamp.Before(&rv.CreationTimestamp)
	}
}

func (ssn *Session) TaskOrderFn(l, r interface{}) bool {
	for _, compareTasks := range ssn.TaskOrderFns {
		if comparison := compareTasks(l, r); comparison != 0 {
			return comparison < 0
		}
	}

	// If no job order funcs, order job by CreationTimestamp first, then by UID.
	lv := l.(*pod_info.PodInfo)
	rv := r.(*pod_info.PodInfo)
	if lv.Pod.CreationTimestamp.Equal(&rv.Pod.CreationTimestamp) {
		return lv.UID < rv.UID
	} else {
		return lv.Pod.CreationTimestamp.Before(&rv.Pod.CreationTimestamp)
	}
}

func (ssn *Session) QueueOrderFn(lQ, rQ *queue_info.QueueInfo, lJob, rJob *podgroup_info.PodGroupInfo,
	lVictims, rVictims []*podgroup_info.PodGroupInfo,
) bool {
	minNodeGPUMemory := ssn.MinNodeGPUMemory
	for _, qof := range ssn.QueueOrderFns {
		if j := qof(lQ, rQ, lJob, rJob, lVictims, rVictims, minNodeGPUMemory); j != 0 {
			return j < 0
		}
	}

	// If no queue order funcs, order queue by CreationTimestamp first, then by UID.
	if lQ.CreationTimestamp.Equal(&rQ.CreationTimestamp) {
		return lQ.UID < rQ.UID
	}
	return lQ.CreationTimestamp.Before(&rQ.CreationTimestamp)
}

func (ssn *Session) IsNonPreemptibleJobOverQueueQuotaFn(job *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo) *api.SchedulableResult {

	for _, fn := range ssn.IsNonPreemptibleJobOverQueueQuotaFns {
		return fn(job, tasksToAllocate)
	}

	return &api.SchedulableResult{
		IsSchedulable: true,
		Reason:        "",
		Message:       "",
		Details:       nil,
	}
}

func (ssn *Session) IsJobOverQueueCapacityFn(job *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo) *api.SchedulableResult {
	for _, fn := range ssn.IsJobOverCapacityFns {
		return fn(job, tasksToAllocate)
	}

	return &api.SchedulableResult{
		IsSchedulable: true,
		Reason:        "",
		Message:       "",
		Details:       nil,
	}
}

func (ssn *Session) IsTaskAllocationOnNodeOverCapacityFn(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo,
	node *node_info.NodeInfo) *api.SchedulableResult {
	for _, fn := range ssn.IsTaskAllocationOnNodeOverCapacityFns {
		return fn(task, job, node)

	}

	return &api.SchedulableResult{
		IsSchedulable: true,
		Reason:        "",
		Message:       "",
		Details:       nil,
	}
}

func (ssn *Session) PrePredicateFn(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo) error {
	for _, prePredicate := range ssn.PrePredicateFns {
		err := prePredicate(task, job)
		if err != nil {
			log.InfraLogger.V(6).Infof(
				"Failed to run Pre-Predicate on task %s", task.Name)
			return err
		}
	}
	return nil
}

func (ssn *Session) PredicateFn(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
	for _, pfn := range ssn.PredicateFns {
		err := pfn(task, job, node)
		if err != nil {
			log.InfraLogger.V(6).Infof(
				"Failed to run Predicate on task %s", task.Name)
			return err
		}
	}
	return nil
}

func (ssn *Session) GpuOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo, gpuIdx string) (float64, error) {
	score := float64(0)
	for _, gof := range ssn.GpuOrderFns {
		pluginScore, err := gof(task, node, gpuIdx)
		if err != nil {
			return 0, err
		}
		score += pluginScore
	}

	return score, nil
}

func (ssn *Session) NodePreOrderFn(task *pod_info.PodInfo, fittingNodes []*node_info.NodeInfo) {
	for _, nodePreOrderFn := range ssn.NodePreOrderFns {
		if err := nodePreOrderFn(task, fittingNodes); err != nil {
			log.InfraLogger.Errorf(
				"Failed to run pre-order on task %s: %v", task.Name, err)
		}
	}
}

func (ssn *Session) NodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	priorityScore := float64(0)
	for _, nodeOrderFn := range ssn.NodeOrderFns {
		score, err := nodeOrderFn(task, node)
		if err != nil {
			return 0, err
		}
		priorityScore += score
	}
	return priorityScore, nil
}

func (ssn *Session) IsRestrictNodeSchedulingEnabled() bool {
	return ssn.SchedulerParams.RestrictSchedulingNodes
}
