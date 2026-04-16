// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

type jobsQueueMetadata struct {
	jobsInQueue            *scheduler_util.PriorityQueue
	shouldUpdateQueueShare bool
	departmentId           common_info.QueueID
}

type departmentMetadata struct {
	queuesPriorityQueue    *scheduler_util.PriorityQueue
	shouldUpdateQueueShare bool
}

type JobsOrderByQueues struct {
	activeDepartments                *scheduler_util.PriorityQueue
	queueIdToQueueMetadata           map[common_info.QueueID]*jobsQueueMetadata
	departmentIdToDepartmentMetadata map[common_info.QueueID]*departmentMetadata
	ssn                              *framework.Session
	jobsOrderInitOptions             JobsOrderInitOptions
	queuePopsMap                     map[common_info.QueueID][]*podgroup_info.PodGroupInfo
}

func NewJobsOrderByQueues(ssn *framework.Session, options JobsOrderInitOptions) JobsOrderByQueues {
	return JobsOrderByQueues{
		ssn:                              ssn,
		queueIdToQueueMetadata:           map[common_info.QueueID]*jobsQueueMetadata{},
		departmentIdToDepartmentMetadata: map[common_info.QueueID]*departmentMetadata{},
		jobsOrderInitOptions:             options,
		queuePopsMap:                     map[common_info.QueueID][]*podgroup_info.PodGroupInfo{},
	}
}

func (jobsOrder *JobsOrderByQueues) IsEmpty() bool {
	return jobsOrder.activeDepartments == nil || jobsOrder.activeDepartments.Empty()
}

func (jobsOrder *JobsOrderByQueues) Len() int {
	l := 0
	for _, metadata := range jobsOrder.queueIdToQueueMetadata {
		l += metadata.jobsInQueue.Len()
	}
	return l
}

func (jobsOrder *JobsOrderByQueues) PopNextJob() *podgroup_info.PodGroupInfo {
	if jobsOrder.IsEmpty() {
		log.InfraLogger.V(7).Infof("No active departments")
		return nil
	}

	department := jobsOrder.getNextDepartment()
	if department == nil {
		return nil
	}

	queue := jobsOrder.getNextQueue(department)
	if queue == nil {
		return nil
	}

	job := jobsOrder.queueIdToQueueMetadata[queue.UID].jobsInQueue.Pop().(*podgroup_info.PodGroupInfo)
	if jobsOrder.jobsOrderInitOptions.VictimQueue {
		if _, found := jobsOrder.queuePopsMap[queue.UID]; !found {
			jobsOrder.queuePopsMap[queue.UID] = []*podgroup_info.PodGroupInfo{}
		}
		jobsOrder.queuePopsMap[queue.UID] = append(jobsOrder.queuePopsMap[queue.UID], job)
	}

	jobsOrder.handleJobPopOutOfQueue(queue, department)
	jobsOrder.handleJobPopOutOfDepartment(department)

	log.InfraLogger.V(7).Infof("Popped job: %v", job.Name)
	return job
}

func (jobsOrder *JobsOrderByQueues) PushJob(job *podgroup_info.PodGroupInfo) {
	queue := jobsOrder.ssn.Queues[job.Queue]
	department := jobsOrder.ssn.Queues[queue.ParentQueue]

	if _, found := jobsOrder.departmentIdToDepartmentMetadata[department.UID]; !found {
		jobsOrder.initializePriorityQueueForDepartment(department, jobsOrder.jobsOrderInitOptions.VictimQueue)
		jobsOrder.activeDepartments.Push(department)
	}
	if _, found := jobsOrder.queueIdToQueueMetadata[job.Queue]; !found {
		jobsOrder.initializePriorityQueue(job, jobsOrder.jobsOrderInitOptions.VictimQueue)
		jobsOrder.departmentIdToDepartmentMetadata[department.UID].queuesPriorityQueue.Push(queue)
	}

	jobsOrder.queueIdToQueueMetadata[job.Queue].jobsInQueue.Push(job)

	jobsOrder.queueIdToQueueMetadata[queue.UID].shouldUpdateQueueShare = true
	jobsOrder.departmentIdToDepartmentMetadata[department.UID].shouldUpdateQueueShare = true

	log.InfraLogger.V(7).Infof("Pushed job: %v for queue %v, department %v", job.Name, queue.Name,
		department.Name)
}

func (jobsOrder *JobsOrderByQueues) handleJobPopOutOfDepartment(department *queue_info.QueueInfo) {
	if jobsOrder.departmentIdToDepartmentMetadata[department.UID].queuesPriorityQueue.Len() == 0 {
		jobsOrder.activeDepartments.Pop()
		delete(jobsOrder.departmentIdToDepartmentMetadata, department.UID)
		return
	}

	jobsOrder.departmentIdToDepartmentMetadata[department.UID].shouldUpdateQueueShare = true
}

func (jobsOrder *JobsOrderByQueues) handleJobPopOutOfQueue(queue, department *queue_info.QueueInfo) {
	if jobsOrder.queueIdToQueueMetadata[queue.UID].jobsInQueue.Len() == 0 {
		jobsOrder.departmentIdToDepartmentMetadata[department.UID].queuesPriorityQueue.Pop()
		delete(jobsOrder.queueIdToQueueMetadata, queue.UID)
		return
	}

	jobsOrder.queueIdToQueueMetadata[queue.UID].shouldUpdateQueueShare = true
}

func (jobsOrder *JobsOrderByQueues) getNextQueue(department *queue_info.QueueInfo) *queue_info.QueueInfo {
	queue := jobsOrder.departmentIdToDepartmentMetadata[department.UID].queuesPriorityQueue.Peek().(*queue_info.QueueInfo)
	if jobsOrder.queueIdToQueueMetadata[queue.UID].shouldUpdateQueueShare {
		jobsOrder.updateTopQueueShare(queue, department)
		return jobsOrder.getNextQueue(department)
	}

	if jobsOrder.queueIdToQueueMetadata[queue.UID].jobsInQueue.Len() == 0 {
		log.InfraLogger.V(7).Warnf("Queue: <%v> is active, yet no jobs in queue", queue.Name)
		return nil
	}

	log.InfraLogger.V(7).Infof("Get queue: %v", queue.Name)
	return queue
}

func (jobsOrder *JobsOrderByQueues) updateTopQueueShare(topQueue *queue_info.QueueInfo, department *queue_info.QueueInfo) {
	jobsOrder.departmentIdToDepartmentMetadata[department.UID].queuesPriorityQueue.Fix(0)
	jobsOrder.queueIdToQueueMetadata[topQueue.UID].shouldUpdateQueueShare = false
}

func (jobsOrder *JobsOrderByQueues) getNextDepartment() *queue_info.QueueInfo {
	department := jobsOrder.activeDepartments.Peek().(*queue_info.QueueInfo)
	if jobsOrder.departmentIdToDepartmentMetadata[department.UID].shouldUpdateQueueShare {
		jobsOrder.updateTopDepartmentShare(department)
		return jobsOrder.getNextDepartment()
	}
	if jobsOrder.departmentIdToDepartmentMetadata[department.UID].queuesPriorityQueue.Empty() {
		log.InfraLogger.V(7).Warnf("Department: <%v> is active, yet no queues in department", department.Name)
		return nil
	}

	log.InfraLogger.V(7).Infof("Popped department: %v", department.Name)
	return department
}

func (jobsOrder *JobsOrderByQueues) updateTopDepartmentShare(topDepartment *queue_info.QueueInfo) {
	jobsOrder.activeDepartments.Fix(0)
	jobsOrder.departmentIdToDepartmentMetadata[topDepartment.UID].shouldUpdateQueueShare = false
}

// addJobToQueue adds `job` to the jobs queue, creating that job's queue in the jobs order if needed
func (jobsOrder *JobsOrderByQueues) addJobToQueue(job *podgroup_info.PodGroupInfo, reverseOrder bool) {
	if _, found := jobsOrder.queueIdToQueueMetadata[job.Queue]; !found {
		jobsOrder.initializePriorityQueue(job, reverseOrder)
	}
	jobsOrder.queueIdToQueueMetadata[job.Queue].jobsInQueue.Push(job)
}

func (jobsOrder *JobsOrderByQueues) initializePriorityQueueForDepartment(department *queue_info.QueueInfo,
	reverseOrder bool) {
	jobsOrder.departmentIdToDepartmentMetadata[department.UID] = &departmentMetadata{
		queuesPriorityQueue: scheduler_util.NewPriorityQueue(
			jobsOrder.buildFuncOrderBetweenQueuesWithJobs(jobsOrder.queueIdToQueueMetadata, reverseOrder),
			scheduler_util.QueueCapacityInfinite,
		),
	}
}

func (jobsOrder *JobsOrderByQueues) initializePriorityQueue(job *podgroup_info.PodGroupInfo, reverseOrder bool) {
	queue := jobsOrder.ssn.Queues[job.Queue]
	jobsOrder.queueIdToQueueMetadata[job.Queue] = &jobsQueueMetadata{
		jobsInQueue: scheduler_util.NewPriorityQueue(func(l, r interface{}) bool {
			if reverseOrder {
				return !jobsOrder.ssn.JobOrderFn(l, r)
			}
			return jobsOrder.ssn.JobOrderFn(l, r)
		}, jobsOrder.jobsOrderInitOptions.MaxJobsQueueDepth),
		departmentId: queue.ParentQueue,
	}
}

func (jobsOrder *JobsOrderByQueues) buildActiveJobOrderPriorityQueues(reverseOrder bool) {
	jobsOrder.departmentIdToDepartmentMetadata = map[common_info.QueueID]*departmentMetadata{}
	for _, queue := range jobsOrder.ssn.Queues {
		if _, found := jobsOrder.queueIdToQueueMetadata[queue.UID]; !found || jobsOrder.queueIdToQueueMetadata[queue.UID].jobsInQueue.Len() == 0 {
			log.InfraLogger.V(7).Infof("Skipping queue <%s> because no jobs in it", queue.Name)
			continue
		}

		if _, found := jobsOrder.ssn.Queues[queue.ParentQueue]; !found {
			log.InfraLogger.V(7).Warnf("Queue's department doesn't exist. Queue: <%v>, Department: <%v>",
				queue.Name, queue.ParentQueue)
			continue
		}

		if _, found := jobsOrder.departmentIdToDepartmentMetadata[queue.ParentQueue]; !found {
			log.InfraLogger.V(7).Infof("Adding Department <%s> ", queue.ParentQueue)
			jobsOrder.departmentIdToDepartmentMetadata[queue.ParentQueue] = &departmentMetadata{}
			jobsOrder.departmentIdToDepartmentMetadata[queue.ParentQueue].queuesPriorityQueue =
				scheduler_util.NewPriorityQueue(
					jobsOrder.buildFuncOrderBetweenQueuesWithJobs(jobsOrder.queueIdToQueueMetadata, reverseOrder),
					scheduler_util.QueueCapacityInfinite,
				)
		}

		jobsOrder.departmentIdToDepartmentMetadata[queue.ParentQueue].queuesPriorityQueue.Push(queue)
		log.InfraLogger.V(7).Infof("Pushed queue to department's queue priority queue, department name: <%v>, queue name: <%v>, number of active jobs in queue: <%v>, reverseOrder: <%v>",
			queue.ParentQueue, queue.Name, jobsOrder.queueIdToQueueMetadata[queue.UID].jobsInQueue.Len(), reverseOrder)
	}

	log.InfraLogger.V(7).Infof("Building departments, reverse order: <%v>", reverseOrder)
	jobsOrder.activeDepartments = scheduler_util.NewPriorityQueue(
		jobsOrder.buildFuncOrderBetweenDepartmentsWithJobs(reverseOrder),
		scheduler_util.QueueCapacityInfinite)
	for departmentUID := range jobsOrder.departmentIdToDepartmentMetadata {
		log.InfraLogger.V(7).Infof("active Department <%s> ", departmentUID)
		jobsOrder.activeDepartments.Push(jobsOrder.ssn.Queues[departmentUID])
	}
}

func (jobsOrder *JobsOrderByQueues) buildFuncOrderBetweenQueuesWithJobs(jobsQueueMetadataPerQueue map[common_info.QueueID]*jobsQueueMetadata, reverseOrder bool) func(interface{}, interface{}) bool {
	return func(lQ, rQ interface{}) bool {
		lQueue := lQ.(*queue_info.QueueInfo)
		rQueue := rQ.(*queue_info.QueueInfo)

		if _, found := jobsQueueMetadataPerQueue[lQueue.UID]; !found || jobsQueueMetadataPerQueue[lQueue.UID].jobsInQueue.Len() == 0 {
			log.InfraLogger.V(7).Infof("Queue: %v, has no pending jobs", lQueue.Name)
			return !reverseOrder // When r has higher priority, return true
		}

		if _, found := jobsQueueMetadataPerQueue[rQueue.UID]; !found || jobsQueueMetadataPerQueue[rQueue.UID].jobsInQueue.Len() == 0 {
			log.InfraLogger.V(7).Infof("Queue: %v, has no pending jobs", rQueue.Name)
			return reverseOrder // When l has higher priority, return false
		}

		var lPending, rPending *podgroup_info.PodGroupInfo
		if !jobsOrder.jobsOrderInitOptions.VictimQueue {
			lPending = jobsQueueMetadataPerQueue[lQueue.UID].jobsInQueue.Pop().(*podgroup_info.PodGroupInfo)
			jobsQueueMetadataPerQueue[lQueue.UID].jobsInQueue.Push(lPending)
			rPending = jobsQueueMetadataPerQueue[rQueue.UID].jobsInQueue.Pop().(*podgroup_info.PodGroupInfo)
			jobsQueueMetadataPerQueue[rQueue.UID].jobsInQueue.Push(rPending)
		}

		var lVictims, rVictims []*podgroup_info.PodGroupInfo
		if jobsOrder.jobsOrderInitOptions.VictimQueue {
			var lPoppedJobs []*podgroup_info.PodGroupInfo
			if len(jobsOrder.queuePopsMap[lQueue.UID]) > 0 {
				lPoppedJobs = append(lPoppedJobs, jobsOrder.queuePopsMap[lQueue.UID]...)
			}
			lVictims = append(lPoppedJobs, jobsQueueMetadataPerQueue[lQueue.UID].jobsInQueue.Pop().(*podgroup_info.PodGroupInfo))
			jobsQueueMetadataPerQueue[lQueue.UID].jobsInQueue.Push(lVictims[len(lVictims)-1])

			var rPoppedJobs []*podgroup_info.PodGroupInfo
			if len(jobsOrder.queuePopsMap[rQueue.UID]) > 0 {
				rPoppedJobs = append(rPoppedJobs, jobsOrder.queuePopsMap[rQueue.UID]...)
			}
			rVictims = append(rPoppedJobs, jobsQueueMetadataPerQueue[rQueue.UID].jobsInQueue.Pop().(*podgroup_info.PodGroupInfo))
			jobsQueueMetadataPerQueue[rQueue.UID].jobsInQueue.Push(rVictims[len(rVictims)-1])
		}

		if reverseOrder {
			return !jobsOrder.ssn.QueueOrderFn(lQueue, rQueue, lPending, rPending, lVictims, rVictims)
		}

		return jobsOrder.ssn.QueueOrderFn(lQueue, rQueue, lPending, rPending, lVictims, rVictims)
	}
}

func (jobsOrder *JobsOrderByQueues) buildFuncOrderBetweenDepartmentsWithJobs(reverseOrder bool) func(interface{}, interface{}) bool {
	return func(l, r interface{}) bool {
		lDepartment := l.(*queue_info.QueueInfo)
		rDepartment := r.(*queue_info.QueueInfo)

		if jobsOrder.departmentIdToDepartmentMetadata[lDepartment.UID].queuesPriorityQueue.Empty() {
			return !reverseOrder // When r has higher priority, return true
		}

		if jobsOrder.departmentIdToDepartmentMetadata[rDepartment.UID].queuesPriorityQueue.Empty() {
			return reverseOrder // When l has higher priority, return false
		}
		lBestQueue := jobsOrder.departmentIdToDepartmentMetadata[lDepartment.UID].queuesPriorityQueue.Pop().(*queue_info.QueueInfo)
		rBestQueue := jobsOrder.departmentIdToDepartmentMetadata[rDepartment.UID].queuesPriorityQueue.Pop().(*queue_info.QueueInfo)

		lJobsInQueue := jobsOrder.queueIdToQueueMetadata[lBestQueue.UID].jobsInQueue
		rJobsInQueue := jobsOrder.queueIdToQueueMetadata[rBestQueue.UID].jobsInQueue

		var lPending, rPending *podgroup_info.PodGroupInfo
		if !jobsOrder.jobsOrderInitOptions.VictimQueue {
			lPending = lJobsInQueue.Pop().(*podgroup_info.PodGroupInfo)
			lJobsInQueue.Push(lPending)
			rPending = rJobsInQueue.Pop().(*podgroup_info.PodGroupInfo)
			rJobsInQueue.Push(rPending)
		}

		var lVictims, rVictims []*podgroup_info.PodGroupInfo
		if jobsOrder.jobsOrderInitOptions.VictimQueue {
			var lPoppedJobs []*podgroup_info.PodGroupInfo
			if len(jobsOrder.queuePopsMap[lBestQueue.UID]) > 0 {
				lPoppedJobs = append(lPoppedJobs, jobsOrder.queuePopsMap[lBestQueue.UID]...)
			}
			lVictims = append(lPoppedJobs, lJobsInQueue.Pop().(*podgroup_info.PodGroupInfo))
			lJobsInQueue.Push(lVictims[len(lVictims)-1])

			var rPoppedJobs []*podgroup_info.PodGroupInfo
			if len(jobsOrder.queuePopsMap[rBestQueue.UID]) > 0 {
				rPoppedJobs = append(rPoppedJobs, jobsOrder.queuePopsMap[rBestQueue.UID]...)
			}
			rVictims = append(rPoppedJobs, rJobsInQueue.Pop().(*podgroup_info.PodGroupInfo))
			rJobsInQueue.Push(rVictims[len(rVictims)-1])
		}
		jobsOrder.departmentIdToDepartmentMetadata[lDepartment.UID].queuesPriorityQueue.Push(lBestQueue)
		jobsOrder.departmentIdToDepartmentMetadata[rDepartment.UID].queuesPriorityQueue.Push(rBestQueue)

		if reverseOrder {
			return !jobsOrder.ssn.QueueOrderFn(lDepartment, rDepartment, lPending, rPending, lVictims, rVictims)
		}

		return jobsOrder.ssn.QueueOrderFn(lDepartment, rDepartment, lPending, rPending, lVictims, rVictims)
	}
}
