/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package proportion

import (
	"math"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
	cp "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/capacity_policy"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/queue_order"
	rec "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/reclaimable"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_division"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

const (
	mebibytes = 1000 * 1000
)

type proportionPlugin struct {
	totalResource       rs.ResourceQuantities
	queues              map[common_info.QueueID]*rs.QueueAttributes
	jobSimulationQueues map[common_info.QueueID]*rs.QueueAttributes
	// Arguments given for the plugin
	pluginArguments               framework.PluginArguments
	subGroupOrderFn               common_info.LessFn
	taskOrderFunc                 common_info.LessFn
	reclaimablePlugin             *rec.Reclaimable
	allowConsolidatingReclaim     bool
	relcaimerSaturationMultiplier float64
	kValue                        float64
	minNodeGPUMemory              *int64
	// lastSemiPreemptibleCore tracks, per semi-preemptible job, the core (non-preemptible) resource
	// quantities last counted into AllocatedNotPreemptible. The allocate/deallocate handlers recompute
	// the tree-aware core set and apply only the delta, so quota stays correct as elastic subgroups
	// fill in and "flip" from transient-core to elastic.
	lastSemiPreemptibleCore map[common_info.PodGroupID]rs.ResourceQuantities
}

func New(arguments framework.PluginArguments) framework.Plugin {
	multiplier, err := arguments.GetFloat64("relcaimerSaturationMultiplier", 1.0)
	if err != nil {
		log.InfraLogger.Warningf("Failed to parse relcaimerSaturationMultiplier: %v. Using default value of 1.0", err)
	}
	if multiplier < 1.0 {
		log.InfraLogger.Warningf("relcaimerSaturationMultiplier must be >= 1.0, got %v. Using default value of 1.0", multiplier)
		multiplier = 1.0
	}

	kValue, err := arguments.GetFloat64("kValue", 1.0)
	if err != nil {
		log.InfraLogger.Warningf("Failed to parse kValue: %v. Using default value of 1.0", err)
	}
	if kValue <= 0.0 {
		log.InfraLogger.Warningf("kValue must be > 0.0, got %v. Setting as 0", kValue)
		kValue = 0.0
	}

	return &proportionPlugin{
		totalResource:                 rs.EmptyResourceQuantities(),
		queues:                        map[common_info.QueueID]*rs.QueueAttributes{},
		pluginArguments:               arguments,
		relcaimerSaturationMultiplier: multiplier,
		kValue:                        kValue,
	}
}

func (pp *proportionPlugin) Name() string {
	return "proportion"
}

func (pp *proportionPlugin) OnSessionOpen(ssn *framework.Session) {
	pp.lastSemiPreemptibleCore = map[common_info.PodGroupID]rs.ResourceQuantities{}
	pp.calculateResourcesProportion(ssn)
	pp.subGroupOrderFn = ssn.SubGroupOrderFn
	pp.taskOrderFunc = ssn.TaskOrderFn
	pp.minNodeGPUMemory = ssn.ClusterInfo.MinNodeGPUMemoryMiB
	pp.reclaimablePlugin = rec.New(pp.relcaimerSaturationMultiplier)
	capacityPolicy := cp.New(pp.queues, ssn.ClusterInfo.MaxNodeGPUMemoryMiB)
	ssn.AddQueueOrderFn(pp.queueOrder)
	ssn.AddCanReclaimResourcesFn(pp.CanReclaimResourcesFn)
	ssn.AddReclaimVictimFilterFn(pp.reclaimVictimFilterFn)
	ssn.AddReclaimScenarioValidatorFn(pp.reclaimableFn)
	ssn.AddOnJobSolutionStartFn(pp.OnJobSolutionStartFn)
	ssn.AddIsNonPreemptibleJobOverQueueQuotaFns(capacityPolicy.IsNonPreemptibleJobOverQuota)
	ssn.AddIsJobOverCapacityFn(capacityPolicy.IsJobOverQueueCapacity)
	ssn.AddIsTaskAllocationOnNodeOverCapacityFn(capacityPolicy.IsTaskAllocationOnNodeOverCapacity)

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   pp.allocateHandlerFn(ssn),
		DeallocateFunc: pp.deallocateHandlerFn(ssn),
	})

	ssn.AddGetQueueAllocatedResourcesFn(pp.getQueueAllocatedResourceFn)
	ssn.AddGetQueueDeservedResourcesFn(pp.getQueueDeservedResourcesFn)
	ssn.AddGetQueueFairShareFn(pp.getQueueFairShareFn)
	pp.allowConsolidatingReclaim = ssn.AllowConsolidatingReclaim()
}

func (pp *proportionPlugin) OnSessionClose(*framework.Session) {
	pp.totalResource = nil
	pp.queues = nil
}

func (pp *proportionPlugin) OnJobSolutionStartFn() {
	pp.jobSimulationQueues = map[common_info.QueueID]*rs.QueueAttributes{}
	for queueId, queue := range pp.queues {
		pp.jobSimulationQueues[queueId] = queue.Clone()
	}
}

func (pp *proportionPlugin) CanReclaimResourcesFn(reclaimer *podgroup_info.PodGroupInfo) bool {
	reclaimerInfo := pp.buildReclaimerInfo(reclaimer, pp.minNodeGPUMemory)
	return pp.reclaimablePlugin.CanReclaimResources(pp.queues, reclaimerInfo)
}

func (pp *proportionPlugin) reclaimVictimFilterFn(
	reclaimer *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo,
) bool {
	reclaimerInfo := pp.buildReclaimerInfo(reclaimer, pp.minNodeGPUMemory)
	return pp.reclaimablePlugin.FilterVictim(pp.queues, reclaimerInfo, victim.Queue)
}

func (pp *proportionPlugin) reclaimableFn(
	scenario api.ScenarioInfo,
) bool {
	reclaimerInfo := pp.buildReclaimerInfo(scenario.GetPreemptor(), pp.minNodeGPUMemory)
	totalVictimsResources := make(map[common_info.QueueID][]resource_info.ResourceVector)
	victims := scenario.GetVictims()
	for _, victim := range victims {
		totalJobResources := pp.getVictimResources(victim)
		if len(totalJobResources) == 0 {
			continue
		}

		totalVictimsResources[victim.Job.Queue] = append(
			totalVictimsResources[victim.Job.Queue],
			totalJobResources...,
		)
	}

	return pp.reclaimablePlugin.Reclaimable(pp.jobSimulationQueues, reclaimerInfo, totalVictimsResources)
}

func (pp *proportionPlugin) getVictimResources(victim *api.VictimInfo) []resource_info.ResourceVector {
	var victimResources []resource_info.ResourceVector

	elasticTasks, coreTasks := pp.splitVictimTasks(victim)

	// Process elastic tasks individually
	for _, task := range elasticTasks {
		resources := getResources(pp.allowConsolidatingReclaim, task)
		if resources == nil {
			continue
		}
		victimResources = append(victimResources, resources)
	}

	// Process core tasks as a group
	resources := getResources(pp.allowConsolidatingReclaim, coreTasks...)
	if resources != nil {
		victimResources = append(victimResources, resources)
	}

	return victimResources
}

// splitVictimTasks splits victim tasks into (elastic, core). Semi-preemptible jobs use the tree-aware
// core set (minimal satisfying set honoring minSubGroup + minMember), so a whole elastic subgroup
// (e.g. a segment internally at its minMember) is correctly treated as elastic. All other jobs keep
// the per-PodSet minMember split.
func (pp *proportionPlugin) splitVictimTasks(victim *api.VictimInfo) ([]*pod_info.PodInfo, []*pod_info.PodInfo) {
	if victim.Job.IsSemiPreemptibleJob() {
		coreSet := podgroup_info.GetCoreTasks(victim.Job, pp.subGroupOrderFn, pp.taskOrderFunc)
		return splitVictimTasksByCoreSet(victim.Tasks, coreSet)
	}
	return splitVictimTasksByPodSet(victim.Tasks, victim.Job.GetAllPodSets())
}

// splitVictimTasksByCoreSet classifies a task as core iff it belongs to the job's tree-aware core set.
func splitVictimTasksByCoreSet(
	tasks []*pod_info.PodInfo, coreSet map[common_info.PodID]*pod_info.PodInfo,
) ([]*pod_info.PodInfo, []*pod_info.PodInfo) {
	coreTasks := []*pod_info.PodInfo{}
	elasticTasks := []*pod_info.PodInfo{}
	for _, task := range tasks {
		if _, isCore := coreSet[task.UID]; isCore {
			coreTasks = append(coreTasks, task)
		} else {
			elasticTasks = append(elasticTasks, task)
		}
	}
	return elasticTasks, coreTasks
}

// splitVictimTasksByPodSet safely splits victim tasks into elastic and core tasks per PodSet.
// Returns (elasticTasks, coreTasks)
func splitVictimTasksByPodSet(tasks []*pod_info.PodInfo, subGroups map[string]*subgroup_info.PodSet) ([]*pod_info.PodInfo, []*pod_info.PodInfo) {
	subGroupsToTasks := map[string][]*pod_info.PodInfo{}
	for _, task := range tasks {
		subGroupName := podgroup_info.DefaultSubGroup
		if task.SubGroupName != "" {
			subGroupName = task.SubGroupName
		}
		if _, found := subGroupsToTasks[subGroupName]; !found {
			subGroupsToTasks[subGroupName] = []*pod_info.PodInfo{}
		}
		subGroupsToTasks[subGroupName] = append(subGroupsToTasks[subGroupName], task)
	}

	coreTasks := []*pod_info.PodInfo{}
	elasticTasks := []*pod_info.PodInfo{}
	for subGroupName, subGroupTasks := range subGroupsToTasks {
		subGroup := subGroups[subGroupName]

		// Handle case where minAvailable is greater than or equal to the number of tasks
		if subGroup.GetMinAvailable() >= int32(len(subGroupTasks)) {
			// All tasks are considered core tasks, no elastic tasks
			coreTasks = append(coreTasks, subGroupTasks...)
			continue
		}

		coreTasks = append(coreTasks, subGroupTasks[:subGroup.GetMinAvailable()]...)
		elasticTasks = append(elasticTasks, subGroupTasks[subGroup.GetMinAvailable():]...)
	}

	return elasticTasks, coreTasks
}

func getResources(ignoreReallocatedTasks bool, pods ...*pod_info.PodInfo) resource_info.ResourceVector {
	var vectors []resource_info.ResourceVector
	for _, task := range pods {
		if ignoreReallocatedTasks && pod_status.IsActiveAllocatedStatus(task.Status) {
			continue
		}
		vectors = append(vectors, task.AcceptedResourceVector)
	}

	if len(vectors) == 0 {
		return nil
	}

	total := vectors[0].Clone()
	for _, vec := range vectors[1:] {
		total.Add(vec)
	}

	return total
}

func (pp *proportionPlugin) calculateResourcesProportion(ssn *framework.Session) {
	log.InfraLogger.V(6).Infof("Calculating resource proportion")

	pp.setTotalResources(ssn)

	pp.createQueueAttributes(ssn)
	log.InfraLogger.V(3).Infof("Total allocatable resources are <%s>, number of nodes: <%d>, number of "+
		"queues: <%d>", pp.totalResource, len(ssn.ClusterInfo.Nodes), len(pp.queues))
}

func (pp *proportionPlugin) setTotalResources(ssn *framework.Session) {
	for _, node := range ssn.ClusterInfo.Nodes {
		pp.totalResource.Add(getNodeResources(ssn, node))
	}
}

func getNodeResources(ssn *framework.Session, node *node_info.NodeInfo) rs.ResourceQuantities {
	nodeResource := rs.EmptyResourceQuantities()

	if !scheduler_util.ValidateIsNodeReady(node.Node) {
		log.InfraLogger.V(2).Infof("Node <%v> is not ready, not counting resource for proportion calculations", node.Name)
		return nodeResource
	}

	gpuWorkerLabelKey := conf.GetConfig().GPUWorkerNodeLabelKey
	_, found := node.Node.Labels[gpuWorkerLabelKey]
	shouldIgnoreGPUs := ssn.IsRestrictNodeSchedulingEnabled() && !found
	if shouldIgnoreGPUs {
		alloc := utils.QuantifyVector(node.AllocatableVector, node.VectorMap)
		alloc[rs.GpuResource] = 0
		nodeResource.Add(alloc)
	} else {
		nodeResource.Add(utils.QuantifyVector(node.AllocatableVector, node.VectorMap))
	}

	// Subtract resources of non-related pods
	schedulerName := ssn.GetSchedulerName()
	for _, podInfo := range node.PodInfos {
		if podInfo.Pod.Spec.SchedulerName != schedulerName &&
			pod_status.IsActiveUsedStatus(podInfo.Status) &&
			!pod_info.IsKaiUtilityPod(podInfo.Pod) {
			log.InfraLogger.V(7).Infof("Pod %s/%s is scheduled by a different scheduler, marking resources as unallocatable "+
				"on node %s", podInfo.Namespace, podInfo.Name, node.Name)
			nodeResource.Sub(utils.QuantifyVector(podInfo.ResReqVector, podInfo.VectorMap))
		}
	}

	return nodeResource
}

func (pp *proportionPlugin) createQueueAttributes(ssn *framework.Session) {
	pp.createQueueResourceAttrs(ssn)
	pp.updateQueuesCurrentResourceUsage(ssn)
	pp.setFairShare()
}

func (pp *proportionPlugin) buildReclaimerInfo(reclaimer *podgroup_info.PodGroupInfo, minNodeGPUMemory *int64) *rec.ReclaimerInfo {
	return &rec.ReclaimerInfo{
		Name:          reclaimer.Name,
		Namespace:     reclaimer.Namespace,
		Queue:         reclaimer.Queue,
		IsPreemptable: reclaimer.IsPreemptibleJob(),
		RequiredResources: podgroup_info.GetTasksToAllocateInitResourceVector(reclaimer, pp.subGroupOrderFn, pp.taskOrderFunc,
			false, minNodeGPUMemory),
		VectorMap: reclaimer.VectorMap,
	}
}

func (pp *proportionPlugin) createQueueResourceAttrs(ssn *framework.Session) {
	for _, queue := range ssn.ClusterInfo.Queues {
		queueAttributes := &rs.QueueAttributes{
			UID:               queue.UID,
			Name:              queue.Name,
			DisplayName:       queue.DisplayName,
			ParentQueue:       queue.ParentQueue,
			ChildQueues:       queue.ChildQueues,
			CreationTimestamp: queue.CreationTimestamp,
			QueueResourceShare: rs.QueueResourceShare{
				GPU:    rs.ResourceShare{},
				CPU:    rs.ResourceShare{},
				Memory: rs.ResourceShare{},
			},
			Priority: queue.Priority,
		}
		deserved := queue.Resources.CPU.Quota
		limit := queue.Resources.CPU.Limit
		overQuotaWeight := queue.Resources.CPU.OverQuotaWeight
		queueAttributes.SetQuotaResources(rs.CpuResource, deserved, limit, overQuotaWeight)

		deserved = math.Max(commonconstants.UnlimitedResourceQuantity, queue.Resources.Memory.Quota*mebibytes)
		limit = math.Max(commonconstants.UnlimitedResourceQuantity, queue.Resources.Memory.Limit*mebibytes)
		overQuotaWeight = queue.Resources.Memory.OverQuotaWeight
		queueAttributes.SetQuotaResources(rs.MemoryResource, deserved, limit, overQuotaWeight)

		deserved = queue.Resources.GPU.Quota
		limit = queue.Resources.GPU.Limit
		overQuotaWeight = queue.Resources.GPU.OverQuotaWeight
		queueAttributes.SetQuotaResources(rs.GpuResource, deserved, limit, overQuotaWeight)

		usage, found := ssn.ClusterInfo.QueueResourceUsage.Queues[queue.UID]
		if found {
			queueAttributes.SetResourceUsage(usage)
		}

		pp.queues[queue.UID] = queueAttributes
		log.InfraLogger.V(7).Infof("Added queue attributes for queue <%s>", queue.Name)
	}
}

func (pp *proportionPlugin) updateQueuesCurrentResourceUsage(ssn *framework.Session) {
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		log.InfraLogger.V(7).Infof("Updateding queue consumed resources based on job <%s/%s>.",
			job.Namespace, job.Name)

		// For semi-preemptible jobs the non-preemptible "core" is the tree's minimal satisfying set
		// (minSubGroup children + minMember pods, honored at every level), not the per-PodSet minMember
		// sum. Seed the baseline core vector here so the allocate/deallocate handlers can apply deltas.
		var semiPreemptibleCore map[common_info.PodID]*pod_info.PodInfo
		if job.IsSemiPreemptibleJob() {
			semiPreemptibleCore = podgroup_info.GetCoreTasks(job, pp.subGroupOrderFn, pp.taskOrderFunc)
			pp.lastSemiPreemptibleCore[job.UID] = rs.EmptyResourceQuantities()
		}

		for status, tasks := range job.PodStatusIndex {
			if pod_status.AllocatedStatus(status) {
				for _, t := range tasks {
					resources := utils.QuantifyVector(t.AcceptedResourceVector, t.VectorMap)
					if job.IsSemiPreemptibleJob() {
						_, isCore := semiPreemptibleCore[t.UID]
						if isCore {
							pp.lastSemiPreemptibleCore[job.UID].Add(resources)
						}
						pp.updateQueuesResourceUsageForAllocatedJob(job.Queue, resources, !isCore)
						continue
					}
					pp.updateQueuesResourceUsageForAllocatedJob(job.Queue, resources, job.IsPreemptibleJob())
				}
			} else if status == pod_status.Pending {
				for _, t := range tasks {
					resources := utils.QuantifyVector(t.ResReqVector, t.VectorMap)
					if t.IsGpuMemoryRequest() && ssn.ClusterInfo.MinNodeGPUMemoryMiB != nil {
						resources.Add(rs.ResourceQuantities{
							rs.GpuResource: t.GpuRequirement.GpuMemoryAsGpuFraction(*ssn.ClusterInfo.MinNodeGPUMemoryMiB),
						})
					}
					pp.updateQueuesResourceUsageForPendingJob(job.Queue, resources)
				}
			}
		}
	}
}

func (pp *proportionPlugin) updateQueuesResourceUsageForAllocatedJob(queueId common_info.QueueID,
	resourceQuantities rs.ResourceQuantities, preemptibleJob bool) {

	for queueAttributes, ok := pp.queues[queueId]; ok; queueAttributes, ok = pp.queues[queueAttributes.ParentQueue] {
		for _, resource := range rs.AllResources {
			qResourceShare := queueAttributes.ResourceShare(resource)
			resourceRequestedQuota := resourceQuantities[resource]

			qResourceShare.Allocated += resourceRequestedQuota
			qResourceShare.Request += resourceRequestedQuota
			if !preemptibleJob {
				qResourceShare.AllocatedNotPreemptible += resourceRequestedQuota
			}
		}
	}
}

func (pp *proportionPlugin) updateQueuesResourceUsageForPendingJob(queueId common_info.QueueID,
	resourceQuantities rs.ResourceQuantities) {

	for queueAttributes, ok := pp.queues[queueId]; ok; queueAttributes, ok = pp.queues[queueAttributes.ParentQueue] {
		for _, resource := range rs.AllResources {
			qResourceShare := queueAttributes.ResourceShare(resource)
			resourceRequestedQuota := resourceQuantities[resource]
			qResourceShare.Request += resourceRequestedQuota
		}
	}
}

func (pp *proportionPlugin) setFairShare() {
	topQueues := pp.getTopQueues()
	metrics.ResetQueueUsage()
	metrics.ResetQueueFairShare()
	pp.setFairShareForQueues(pp.totalResource, pp.kValue, topQueues)
}

func (pp *proportionPlugin) setFairShareForQueues(totalResources rs.ResourceQuantities, kValue float64,
	queues map[common_info.QueueID]*rs.QueueAttributes) {

	if len(queues) == 0 {
		return
	}

	resource_division.SetResourcesShare(totalResources, kValue, queues)
	for _, queue := range queues {
		childQueues := pp.getChildQueues(queue)
		resources := queue.GetFairShare()
		pp.setFairShareForQueues(resources, kValue, childQueues)
	}
}

func (pp *proportionPlugin) getTopQueues() map[common_info.QueueID]*rs.QueueAttributes {
	topQueues := map[common_info.QueueID]*rs.QueueAttributes{}
	for _, queue := range pp.queues {
		if len(queue.ParentQueue) == 0 {
			topQueues[queue.UID] = queue
		}
	}
	return topQueues
}

func (pp *proportionPlugin) getChildQueues(parentQueue *rs.QueueAttributes) map[common_info.QueueID]*rs.QueueAttributes {
	childQueues := map[common_info.QueueID]*rs.QueueAttributes{}
	for _, queueId := range parentQueue.ChildQueues {
		childQueues[queueId] = pp.queues[queueId]
	}
	return childQueues
}

// coreResourceQuantities sums the resources of a semi-preemptible job's tree-aware core set
// (the minimal satisfying set honoring minSubGroup + minMember at every level).
func (pp *proportionPlugin) coreResourceQuantities(job *podgroup_info.PodGroupInfo) rs.ResourceQuantities {
	total := rs.EmptyResourceQuantities()
	for _, task := range podgroup_info.GetCoreTasks(job, pp.subGroupOrderFn, pp.taskOrderFunc) {
		total.Add(utils.QuantifyVector(task.AcceptedResourceVector, task.VectorMap))
	}
	return total
}

// semiPreemptibleCoreDelta recomputes the job's core vector, returns the signed delta versus the
// last recorded value, and stores the new value. This keeps AllocatedNotPreemptible correct as
// elastic subgroups fill in and pods "flip" between transient-core and elastic.
func (pp *proportionPlugin) semiPreemptibleCoreDelta(job *podgroup_info.PodGroupInfo) rs.ResourceQuantities {
	newCore := pp.coreResourceQuantities(job)
	delta := newCore.Clone()
	if last, ok := pp.lastSemiPreemptibleCore[job.UID]; ok {
		delta.Sub(last)
	}
	pp.lastSemiPreemptibleCore[job.UID] = newCore
	return delta
}

// addToQueues walks the queue hierarchy from queueId to the root, applying the given signed deltas
// to Allocated and AllocatedNotPreemptible.
func (pp *proportionPlugin) addToQueues(queueId common_info.QueueID, allocated, notPreemptible rs.ResourceQuantities) {
	for queue, ok := pp.queues[queueId]; ok; queue, ok = pp.queues[queue.ParentQueue] {
		for _, resource := range rs.AllResources {
			resourceShare := queue.ResourceShare(resource)
			resourceShare.Allocated += allocated[resource]
			resourceShare.AllocatedNotPreemptible += notPreemptible[resource]
		}
	}
}

func (pp *proportionPlugin) allocateHandlerFn(ssn *framework.Session) func(event *framework.Event) {
	return func(event *framework.Event) {
		job := ssn.ClusterInfo.PodGroupInfos[event.Task.Job]
		taskResources := utils.QuantifyVector(event.Task.AcceptedResourceVector, event.Task.VectorMap)

		notPreemptible := rs.EmptyResourceQuantities()
		if job.IsSemiPreemptibleJob() {
			notPreemptible = pp.semiPreemptibleCoreDelta(job)
		} else if !job.IsPreemptibleJob() {
			notPreemptible = taskResources
		}
		pp.addToQueues(job.Queue, taskResources, notPreemptible)

		leafQueue := pp.queues[job.Queue]
		log.InfraLogger.V(7).Infof("Proportion AllocateFunc: job <%v/%v>, task resources <%s>, "+
			"queue: <%v>, queue allocated resources: <%v>",
			job.Namespace, job.Name, taskResources, leafQueue.Name, leafQueue.GetAllocatedShare())
	}
}

func (pp *proportionPlugin) deallocateHandlerFn(ssn *framework.Session) func(event *framework.Event) {
	return func(event *framework.Event) {
		job := ssn.ClusterInfo.PodGroupInfos[event.Task.Job]
		taskResources := utils.QuantifyVector(event.Task.AcceptedResourceVector, event.Task.VectorMap)

		allocatedDelta := rs.EmptyResourceQuantities()
		allocatedDelta.Sub(taskResources)

		notPreemptible := rs.EmptyResourceQuantities()
		if job.IsSemiPreemptibleJob() {
			// Recomputed after the task was removed; negative when a core slot was freed.
			notPreemptible = pp.semiPreemptibleCoreDelta(job)
		} else if !job.IsPreemptibleJob() {
			notPreemptible = allocatedDelta
		}
		pp.addToQueues(job.Queue, allocatedDelta, notPreemptible)

		leafQueue := pp.queues[job.Queue]
		log.InfraLogger.V(7).Infof("Proportion DeallocateFunc: job <%v/%v>, task resources <%s>, "+
			"queue: <%v>, queue allocated resources: <%v>",
			job.Namespace, job.Name, taskResources, leafQueue.Name, leafQueue.GetAllocatedShare())
	}
}

func (pp *proportionPlugin) queueOrder(lQ, rQ *queue_info.QueueInfo, lJob, rJob *podgroup_info.PodGroupInfo, lVictims, rVictims []*podgroup_info.PodGroupInfo, minNodeGPUMemory *int64) int {
	lQueueAttributes, found := pp.queues[lQ.UID]
	if !found {
		log.InfraLogger.Errorf("Failed to find queue: <%v>", lQ.Name)
		return 1
	}

	rQueueAttributes, found := pp.queues[rQ.UID]
	if !found {
		log.InfraLogger.Errorf("Failed to find queue: <%v>", rQ.Name)
		return -1
	}

	return queue_order.GetQueueOrderResult(lQueueAttributes, rQueueAttributes, lJob, rJob, lVictims, rVictims,
		pp.subGroupOrderFn, pp.taskOrderFunc, pp.totalResource, minNodeGPUMemory)
}

func (pp *proportionPlugin) getQueueDeservedResourcesFn(queue *queue_info.QueueInfo) *resource_info.ResourceRequirements {
	queueAttributes := pp.queues[queue.UID]
	return utils.ResourceRequirementsFromQuantities(queueAttributes.GetDeservedShare())
}

func (pp *proportionPlugin) getQueueFairShareFn(queue *queue_info.QueueInfo) *resource_info.ResourceRequirements {
	queueAttributes := pp.queues[queue.UID]
	return utils.ResourceRequirementsFromQuantities(queueAttributes.GetFairShare())
}

func (pp *proportionPlugin) getQueueAllocatedResourceFn(queue *queue_info.QueueInfo) *resource_info.ResourceRequirements {
	queueAttributes := pp.queues[queue.UID]
	return utils.ResourceRequirementsFromQuantities(queueAttributes.GetAllocatedShare())
}
