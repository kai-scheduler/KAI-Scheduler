// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package proportion

import (
	"math"
	"strconv"

	commonconstants "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/metrics"
	cp "github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/proportion/capacity_policy"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/proportion/queue_order"
	rec "github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/proportion/reclaimable"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_division"
	rs "github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/scheduler_util"
)

const (
	mebibytes = 1000 * 1000
)

type proportionPlugin struct {
	totalResource       rs.ResourceQuantities
	queues              map[common_info.QueueID]*rs.QueueAttributes
	jobSimulationQueues map[common_info.QueueID]*rs.QueueAttributes
	// Arguments given for the plugin
	pluginArguments               map[string]string
	taskOrderFunc                 common_info.LessFn
	reclaimablePlugin             *rec.Reclaimable
	isInferencePreemptible        bool
	allowConsolidatingReclaim     bool
	relcaimerSaturationMultiplier float64
	minNodeGPUMemory              int64
}

func New(arguments map[string]string) framework.Plugin {
	multiplier := 1.0
	if val, exists := arguments["relcaimerSaturationMultiplier"]; exists {
		if m, err := strconv.ParseFloat(val, 64); err == nil {
			if m < 1.0 {
				log.InfraLogger.Warningf("relcaimerSaturationMultiplier must be >= 1.0, got %v. Using default value of 1.0", m)
			} else {
				multiplier = m
			}
		} else {
			log.InfraLogger.V(1).Errorf("Failed to parse relcaimerSaturationMultiplier: %s. Using default 1.", val)
		}
	}

	return &proportionPlugin{
		totalResource:                 rs.EmptyResourceQuantities(),
		queues:                        map[common_info.QueueID]*rs.QueueAttributes{},
		pluginArguments:               arguments,
		relcaimerSaturationMultiplier: multiplier,
	}
}

func (pp *proportionPlugin) Name() string {
	return "proportion"
}

func (pp *proportionPlugin) OnSessionOpen(ssn *framework.Session) {
	pp.calculateResourcesProportion(ssn)
	pp.taskOrderFunc = ssn.TaskOrderFn
	pp.minNodeGPUMemory = ssn.MinNodeGPUMemory
	pp.reclaimablePlugin = rec.New(pp.relcaimerSaturationMultiplier)
	pp.isInferencePreemptible = ssn.IsInferencePreemptible()
	capacityPolicy := cp.New(pp.queues, ssn.IsInferencePreemptible())
	ssn.AddQueueOrderFn(pp.queueOrder)
	ssn.AddCanReclaimResourcesFn(pp.CanReclaimResourcesFn)
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

func (pp *proportionPlugin) reclaimableFn(
	scenario api.ScenarioInfo,
) bool {
	reclaimerInfo := pp.buildReclaimerInfo(scenario.GetPreemptor(), pp.minNodeGPUMemory)
	totalVictimsResources := make(map[common_info.QueueID][]*resource_info.Resource)
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

func (pp *proportionPlugin) getVictimResources(victim *api.VictimInfo) []*resource_info.Resource {
	var victimResources []*resource_info.Resource
	elasticTasks, coreTasks := splitVictimTasks(victim.Tasks, victim.Job.MinAvailable)

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

// splitVictimTasks safely splits victim tasks into elastic and core tasks
// Returns (elasticTasks, coreTasks)
func splitVictimTasks(tasks []*pod_info.PodInfo, minAvailable int32) ([]*pod_info.PodInfo, []*pod_info.PodInfo) {
	totalTasks := len(tasks)
	minAvailableInt := int(minAvailable)

	// Handle case where minAvailable is greater than or equal to the number of tasks
	if minAvailableInt >= totalTasks {
		// All tasks are considered core tasks, no elastic tasks
		return nil, tasks
	}

	// Normal case: split tasks into elastic and core
	elasticTasks := tasks[minAvailableInt:]
	coreTasks := tasks[:minAvailableInt]

	return elasticTasks, coreTasks
}

func getResources(ignoreReallocatedTasks bool, pods ...*pod_info.PodInfo) *resource_info.Resource {
	resources := make([]*resource_info.ResourceRequirements, 0, len(pods))
	for _, task := range pods {
		if ignoreReallocatedTasks && pod_status.IsActiveAllocatedStatus(task.Status) {
			continue
		}
		resources = append(resources, task.AcceptedResource)
	}

	if len(resources) == 0 {
		return nil
	}

	totalResources := resource_info.EmptyResource()
	for _, resource := range resources {
		totalResources.AddResourceRequirements(resource)
	}

	return totalResources
}

func (pp *proportionPlugin) calculateResourcesProportion(ssn *framework.Session) {
	log.InfraLogger.V(6).Infof("Calculating resource proportion")

	pp.setTotalResources(ssn)
	pp.createQueueAttributes(ssn)
	log.InfraLogger.V(3).Infof("Total allocatable resources are <%v>, number of nodes: <%v>, number of "+
		"queues: <%v>", pp.totalResource.String(), len(ssn.Nodes), len(pp.queues))
}

func (pp *proportionPlugin) setTotalResources(ssn *framework.Session) {
	for _, node := range ssn.Nodes {
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
		nodeResource.Add(rs.NewResourceQuantities(node.Allocatable.Cpu(), node.Allocatable.Memory(), 0))
	} else {
		nodeResource.Add(utils.QuantifyResource(node.Allocatable))
	}

	// Subtract resources of non-related pods
	schedulerName := ssn.GetSchedulerName()
	for _, podInfo := range node.PodInfos {
		if podInfo.Pod.Spec.SchedulerName != schedulerName &&
			pod_status.IsActiveUsedStatus(podInfo.Status) &&
			!pod_info.IsKaiUtilityPod(podInfo.Pod) {
			log.InfraLogger.V(7).Infof("Pod %s/%s is scheduled by a different scheduler, marking resources as unallocatable "+
				"on node %s", podInfo.Namespace, podInfo.Name, node.Name)
			nodeResource.Sub(utils.QuantifyResourceRequirements(podInfo.ResReq))
		}
	}

	return nodeResource
}

func (pp *proportionPlugin) createQueueAttributes(ssn *framework.Session) {
	pp.createQueueResourceAttrs(ssn)
	pp.updateQueuesCurrentResourceUsage(ssn)
	pp.setFairShare()
}

func (pp *proportionPlugin) buildReclaimerInfo(reclaimer *podgroup_info.PodGroupInfo, minNodeGPUMemory int64) *rec.ReclaimerInfo {
	return &rec.ReclaimerInfo{
		Name:          reclaimer.Name,
		Namespace:     reclaimer.Namespace,
		Queue:         reclaimer.Queue,
		IsPreemptable: reclaimer.IsPreemptibleJob(pp.isInferencePreemptible),
		RequiredResources: podgroup_info.GetTasksToAllocateInitResource(
			reclaimer, pp.taskOrderFunc, false, minNodeGPUMemory),
	}
}

func (pp *proportionPlugin) createQueueResourceAttrs(ssn *framework.Session) {
	for _, queue := range ssn.Queues {
		queueAttributes := &rs.QueueAttributes{
			UID:               queue.UID,
			Name:              queue.Name,
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

		pp.queues[queue.UID] = queueAttributes
		log.InfraLogger.V(7).Infof("Added queue attributes for queue <%s>", queue.Name)
	}
}

func (pp *proportionPlugin) updateQueuesCurrentResourceUsage(ssn *framework.Session) {
	for _, job := range ssn.PodGroupInfos {
		log.InfraLogger.V(7).Infof("Updateding queue consumed resources based on job <%s/%s>.",
			job.Namespace, job.Name)

		for status, tasks := range job.PodStatusIndex {
			if pod_status.AllocatedStatus(status) {
				for _, t := range tasks {
					resources := utils.QuantifyResourceRequirements(t.AcceptedResource)
					isPreemptible := job.IsPreemptibleJob(ssn.IsInferencePreemptible())
					pp.updateQueuesResourceUsageForAllocatedJob(job.Queue, resources, isPreemptible)
				}
			} else if status == pod_status.Pending {
				for _, t := range tasks {
					resources := utils.QuantifyResourceRequirements(t.ResReq)
					if t.IsMemoryRequest() {
						resources.Add(rs.ResourceQuantities{
							rs.GpuResource: float64(t.ResReq.GpuResourceRequirement.GetNumOfGpuDevices()) * (float64(t.ResReq.GpuMemory()) / float64(ssn.MinNodeGPUMemory)),
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
	metrics.ResetQueueFairShare()
	pp.setFairShareForQueues(pp.totalResource, topQueues)
}

func (pp *proportionPlugin) setFairShareForQueues(totalResources rs.ResourceQuantities,
	queues map[common_info.QueueID]*rs.QueueAttributes) {

	if len(queues) == 0 {
		return
	}

	resource_division.SetResourcesShare(totalResources, queues)
	for _, queue := range queues {
		childQueues := pp.getChildQueues(queue)
		resources := queue.GetFairShare()
		pp.setFairShareForQueues(resources, childQueues)
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

func (pp *proportionPlugin) allocateHandlerFn(ssn *framework.Session) func(event *framework.Event) {
	return func(event *framework.Event) {
		job := ssn.PodGroupInfos[event.Task.Job]
		isPreemptibleJob := job.IsPreemptibleJob(ssn.IsInferencePreemptible())
		taskResources := utils.QuantifyResourceRequirements(event.Task.AcceptedResource)

		for queue, ok := pp.queues[job.Queue]; ok; queue, ok = pp.queues[queue.ParentQueue] {
			for _, resource := range rs.AllResources {
				resourceShare := queue.ResourceShare(resource)
				resourceShare.Allocated += taskResources[resource]

				if !isPreemptibleJob {
					resourceShare.AllocatedNotPreemptible += taskResources[resource]
				}
			}
		}

		leafQueue := pp.queues[job.Queue]
		log.InfraLogger.V(7).Infof("Proportion AllocateFunc: job <%v/%v>, task resources <%v>, "+
			"queue: <%v>, queue allocated resources: <%v>",
			job.Namespace, job.Name, taskResources, leafQueue.Name, leafQueue.GetAllocatedShare())
	}
}

func (pp *proportionPlugin) deallocateHandlerFn(ssn *framework.Session) func(event *framework.Event) {
	return func(event *framework.Event) {
		job := ssn.PodGroupInfos[event.Task.Job]
		isPreemptibleJob := job.IsPreemptibleJob(ssn.IsInferencePreemptible())
		taskResources := utils.QuantifyResourceRequirements(event.Task.AcceptedResource)

		for queue, ok := pp.queues[job.Queue]; ok; queue, ok = pp.queues[queue.ParentQueue] {
			for _, resource := range rs.AllResources {
				resourceShare := queue.ResourceShare(resource)
				resourceShare.Allocated -= taskResources[resource]

				if !isPreemptibleJob {
					resourceShare.AllocatedNotPreemptible -= taskResources[resource]
				}
			}
		}

		leafQueue := pp.queues[job.Queue]
		log.InfraLogger.V(7).Infof("Proportion DeallocateFunc: job <%v/%v>, task resources <%v>, "+
			"queue: <%v>, queue allocated resources: <%v>",
			job.Namespace, job.Name, taskResources.String(), leafQueue.Name, leafQueue.GetAllocatedShare())
	}
}

func (pp *proportionPlugin) queueOrder(lQ, rQ *queue_info.QueueInfo, lJob, rJob *podgroup_info.PodGroupInfo, lVictims, rVictims []*podgroup_info.PodGroupInfo, minNodeGPUMemory int64) int {
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

	return queue_order.GetQueueOrderResult(lQueueAttributes, rQueueAttributes, lJob, rJob, lVictims, rVictims, pp.taskOrderFunc, pp.totalResource, minNodeGPUMemory)
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
