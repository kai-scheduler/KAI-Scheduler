// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resizeeviction

import (
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

// resizableResourceIndices are the vector indices of resources an in-place resize can change.
var resizableResourceIndices = []int{resource_info.CPUIndex, resource_info.MemoryIndex}

type resizeEvictionAction struct{}

func New() *resizeEvictionAction {
	return &resizeEvictionAction{}
}

func (a *resizeEvictionAction) Name() framework.ActionType {
	return framework.ResizeEviction
}

// deferredResize is a bound task whose in-place resize the kubelet reported as Deferred,
// together with the still-unallocated part of the resize as a resource vector.
type deferredResize struct {
	job   *podgroup_info.PodGroupInfo
	task  *pod_info.PodInfo
	delta resource_info.ResourceVector
}

func (a *resizeEvictionAction) Execute(ssn *framework.Session) {
	log.InfraLogger.V(2).Infof("Enter ResizeEviction ...")
	defer log.InfraLogger.V(2).Infof("Leaving ResizeEviction ...")

	deferredResizes := collectDeferredResizes(ssn)
	if len(deferredResizes) == 0 {
		return
	}
	log.InfraLogger.V(3).Infof("Found <%d> tasks with a deferred in-place resize", len(deferredResizes))

	// Deltas of still-deferred resizes per node. The kubelet has not allocated these targets,
	// so they are excluded when computing another pod's enactment shortfall on the same node.
	pendingDeltasByNode := map[string]resource_info.ResourceVector{}
	for _, dr := range deferredResizes {
		nodeDeltas, found := pendingDeltasByNode[dr.task.NodeName]
		if !found {
			nodeDeltas = resource_info.NewResourceVector(dr.task.VectorMap)
		}
		nodeDeltas.Add(dr.delta)
		pendingDeltasByNode[dr.task.NodeName] = nodeDeltas
	}

	for _, dr := range deferredResizes {
		if attemptEvictionForDeferredResize(ssn, dr, pendingDeltasByNode[dr.task.NodeName]) {
			// The kubelet can now allocate this target, so stop excluding its delta.
			nodeDeltas := pendingDeltasByNode[dr.task.NodeName]
			nodeDeltas.Sub(dr.delta)
			pendingDeltasByNode[dr.task.NodeName] = nodeDeltas
		}
	}
}

func collectDeferredResizes(ssn *framework.Session) []*deferredResize {
	var result []*deferredResize
	now := time.Now()
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if job.IsWithinPreemptionDelay(now) {
			continue
		}
		for _, task := range job.GetAllPodsMap() {
			if task.NodeName == "" || !pod_status.IsActiveAllocatedStatus(task.Status) {
				continue
			}
			if _, found := ssn.ClusterInfo.Nodes[task.NodeName]; !found {
				continue
			}
			delta := task.DeferredResizeDelta()
			if delta == nil {
				continue
			}
			result = append(result, &deferredResize{
				job:   job,
				task:  task,
				delta: deltaToVector(delta, task.VectorMap),
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].job.Priority != result[j].job.Priority {
			return result[i].job.Priority > result[j].job.Priority
		}
		if !result[i].job.CreationTimestamp.Equal(&result[j].job.CreationTimestamp) {
			return result[i].job.CreationTimestamp.Before(&result[j].job.CreationTimestamp)
		}
		return result[i].task.UID < result[j].task.UID
	})
	return result
}

func deltaToVector(delta v1.ResourceList, vectorMap *resource_info.ResourceVectorMap) resource_info.ResourceVector {
	vec := resource_info.NewResourceVector(vectorMap)
	if cpu, found := delta[v1.ResourceCPU]; found {
		vec.Set(resource_info.CPUIndex, float64(cpu.MilliValue()))
	}
	if memory, found := delta[v1.ResourceMemory]; found {
		vec.Set(resource_info.MemoryIndex, float64(memory.Value()))
	}
	return vec
}

// attemptEvictionForDeferredResize evicts victims on the task's node until the kubelet can
// enact the deferred resize. All-or-nothing: if the shortfall cannot be fully covered by
// eligible victims, or a scenario validator rejects the victim set, nothing is evicted.
// Returns true when the resize is enactable (immediately or after this attempt's evictions).
func attemptEvictionForDeferredResize(
	ssn *framework.Session, dr *deferredResize, nodePendingDeltas resource_info.ResourceVector,
) bool {
	node := ssn.ClusterInfo.Nodes[dr.task.NodeName]
	otherDeltas := nodePendingDeltas.Clone()
	otherDeltas.Sub(dr.delta)

	if shortfall := enactmentShortfall(node, dr.delta, otherDeltas); shortfall == nil {
		log.InfraLogger.V(4).Infof(
			"Deferred resize of task <%s/%s> on node <%s> is already enactable, skipping",
			dr.task.Namespace, dr.task.Name, node.Name)
		return true
	}

	log.InfraLogger.V(3).Infof(
		"Attempting to evict for deferred resize of task <%s/%s> on node <%s>, job: <%s/%s>, queue: <%v>",
		dr.task.Namespace, dr.task.Name, node.Name, dr.job.Namespace, dr.job.Name, dr.job.Queue)

	metrics.IncPodgroupsConsideredByAction()
	ssn.OnJobSolutionStart()
	canReclaim := ssn.CanReclaimResources(dr.job)
	victimsQueue := utils.GetVictimsQueue(ssn, buildVictimFilter(ssn, dr.job, node.Name, canReclaim))

	stmt := ssn.Statement()
	victims := map[common_info.PodGroupID]*api.VictimInfo{}
	satisfied := false
	for !victimsQueue.IsEmpty() && !satisfied {
		victimJob := victimsQueue.PopNextJob()
		if !evictFromJob(ssn, stmt, dr, victimJob, victims, node, otherDeltas, &satisfied) {
			stmt.Discard()
			return false
		}
	}

	if !satisfied {
		log.InfraLogger.V(3).Infof(
			"Not enough eligible victims on node <%s> for deferred resize of task <%s/%s>",
			node.Name, dr.task.Namespace, dr.task.Name)
		stmt.Discard()
		return false
	}
	if !validateVictims(ssn, dr.job, victims) {
		log.InfraLogger.V(3).Infof(
			"Victim set for deferred resize of task <%s/%s> was rejected by scenario validators",
			dr.task.Namespace, dr.task.Name)
		stmt.Discard()
		return false
	}

	victimNames := victimTaskNames(victims)
	if err := stmt.Commit(); err != nil {
		log.InfraLogger.Errorf("Failed to commit resize eviction statement: %v", err)
		return false
	}
	metrics.IncPodgroupScheduledByAction()
	log.InfraLogger.V(3).Infof(
		"Evicted tasks <%v> to free node <%s> capacity for deferred resize of task <%s/%s>",
		victimNames, node.Name, dr.task.Namespace, dr.task.Name)
	return true
}

// evictFromJob evicts task batches from victimJob into stmt until the shortfall is covered or
// the job has nothing more to give on the target node. Returns false on eviction error.
func evictFromJob(
	ssn *framework.Session, stmt *framework.Statement, dr *deferredResize,
	victimJob *podgroup_info.PodGroupInfo, victims map[common_info.PodGroupID]*api.VictimInfo,
	node *node_info.NodeInfo, otherDeltas resource_info.ResourceVector, satisfied *bool,
) bool {
	for !*satisfied {
		tasks, hasMoreTasks := podgroup_info.GetTasksToEvict(victimJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn)
		if len(tasks) == 0 || !anyTaskOnNode(tasks, node.Name) {
			return true
		}
		for _, task := range tasks {
			message := api.GetResizeEvictMessage(dr.job, task)
			metadata := eviction_info.EvictionMetadata{
				Action:           string(framework.ResizeEviction),
				EvictionGangSize: len(tasks),
				Preemptor:        &types.NamespacedName{Namespace: dr.job.Namespace, Name: dr.job.Name},
			}
			if err := stmt.Evict(task, message, metadata); err != nil {
				log.InfraLogger.Errorf("Failed to evict task <%s/%s> for deferred resize: %v",
					task.Namespace, task.Name, err)
				return false
			}
		}
		recordVictims(victims, victimJob, tasks)
		*satisfied = enactmentShortfall(node, dr.delta, otherDeltas) == nil
		if !hasMoreTasks {
			return true
		}
	}
	return true
}

// enactmentShortfall returns the per-resource amount still missing on the node for the
// kubelet to enact a deferred resize with the given delta, or nil when nothing is missing.
// Requests remaining after releasing pods terminate must fit allocatable; targets of other
// still-deferred resizes are excluded since the kubelet has not allocated them.
func enactmentShortfall(
	node *node_info.NodeInfo, delta, otherDeferredDeltas resource_info.ResourceVector,
) resource_info.ResourceVector {
	var shortfall resource_info.ResourceVector
	for _, idx := range resizableResourceIndices {
		if delta.Get(idx) <= 0 {
			continue
		}
		missing := node.UsedVector.Get(idx) - node.ReleasingVector.Get(idx) -
			otherDeferredDeltas.Get(idx) - node.AllocatableVector.Get(idx)
		if missing <= 0 {
			continue
		}
		if shortfall == nil {
			shortfall = resource_info.NewResourceVector(node.VectorMap)
		}
		shortfall.Set(idx, missing)
	}
	return shortfall
}

// buildVictimFilter admits jobs a new pod of the resizing job would be allowed to displace:
// same-queue jobs under preempt rules, other-queue jobs under reclaim rules.
func buildVictimFilter(
	ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo, nodeName string, canReclaim bool,
) func(*podgroup_info.PodGroupInfo) bool {
	return func(victim *podgroup_info.PodGroupInfo) bool {
		if victim.UID == preemptor.UID {
			return false
		}
		if !victim.IsPreemptibleJob() {
			return false
		}
		if victim.GetActiveAllocatedTasksCount() == 0 {
			return false
		}
		if !jobHasActiveAllocatedTaskOnNode(victim, nodeName) {
			return false
		}
		if victim.Queue == preemptor.Queue {
			return victim.Priority < preemptor.Priority && ssn.PreemptVictimFilter(preemptor, victim)
		}
		return canReclaim && ssn.ReclaimVictimFilter(preemptor, victim)
	}
}

func jobHasActiveAllocatedTaskOnNode(job *podgroup_info.PodGroupInfo, nodeName string) bool {
	for _, task := range job.GetAllPodsMap() {
		if task.NodeName == nodeName && pod_status.IsActiveAllocatedStatus(task.Status) {
			return true
		}
	}
	return false
}

func anyTaskOnNode(tasks []*pod_info.PodInfo, nodeName string) bool {
	for _, task := range tasks {
		if task.NodeName == nodeName {
			return true
		}
	}
	return false
}

func recordVictims(
	victims map[common_info.PodGroupID]*api.VictimInfo,
	job *podgroup_info.PodGroupInfo, tasks []*pod_info.PodInfo,
) {
	victim, found := victims[job.UID]
	if !found {
		victim = &api.VictimInfo{Job: job}
		victims[job.UID] = victim
	}
	victim.Tasks = append(victim.Tasks, tasks...)
}

// validateVictims runs the victim set through the same scenario validators preempt and
// reclaim use: same-queue victims under preempt validation, cross-queue under reclaim.
func validateVictims(
	ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo,
	victims map[common_info.PodGroupID]*api.VictimInfo,
) bool {
	sameQueue := map[common_info.PodGroupID]*api.VictimInfo{}
	crossQueue := map[common_info.PodGroupID]*api.VictimInfo{}
	for uid, victim := range victims {
		if victim.Job.Queue == preemptor.Queue {
			sameQueue[uid] = victim
		} else {
			crossQueue[uid] = victim
		}
	}

	if len(sameQueue) > 0 && !ssn.PreemptScenarioValidator(&evictionScenario{preemptor, sameQueue}) {
		return false
	}
	if len(crossQueue) > 0 && !ssn.ReclaimScenarioValidatorFn(&evictionScenario{preemptor, crossQueue}) {
		return false
	}
	return true
}

func victimTaskNames(victims map[common_info.PodGroupID]*api.VictimInfo) []string {
	var names []string
	for _, victim := range victims {
		for _, task := range victim.Tasks {
			names = append(names, task.Namespace+"/"+task.Name)
		}
	}
	sort.Strings(names)
	return names
}

type evictionScenario struct {
	preemptor *podgroup_info.PodGroupInfo
	victims   map[common_info.PodGroupID]*api.VictimInfo
}

func (s *evictionScenario) GetPreemptor() *podgroup_info.PodGroupInfo {
	return s.preemptor
}

func (s *evictionScenario) GetVictims() map[common_info.PodGroupID]*api.VictimInfo {
	return s.victims
}
