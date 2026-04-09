/*
Copyright 2017 The Kubernetes Authors.

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

package allocate

import (
	"time"

	"golang.org/x/exp/maps"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

type allocateAction struct {
}

func New() *allocateAction {
	return &allocateAction{}
}

func (alloc *allocateAction) Name() framework.ActionType {
	return framework.Allocate
}

func (alloc *allocateAction) Execute(ssn *framework.Session) {
	log.InfraLogger.V(2).Infof("Enter Allocate ...")
	defer log.InfraLogger.V(2).Infof("Leaving Allocate ...")

	// Unsuspend workloads that were previously suspended by KAI's
	// preemption and now have resources available. Suspended workloads
	// have no pods, so they're invisible to the main allocate loop.
	unsuspendReadyWorkloads(ssn)

	jobsOrderByQueues := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
		FilterNonPending:  true,
		FilterUnready:     true,
		MaxJobsQueueDepth: ssn.GetJobsDepth(framework.Allocate),
	})
	jobsOrderByQueues.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)

	log.InfraLogger.V(2).Infof("There are <%d> PodGroupInfos and <%d> Queues in total for scheduling",
		jobsOrderByQueues.Len(), ssn.CountLeafQueues())
	for !jobsOrderByQueues.IsEmpty() {
		job := jobsOrderByQueues.PopNextJob()
		stmt := ssn.Statement()
		alreadyAllocated := job.GetNumAllocatedTasks() > 0
		if ok, pipelined := attemptToAllocateJob(ssn, stmt, job); ok {
			metrics.IncPodgroupScheduledByAction()
			err := stmt.Commit()
			if err == nil && !pipelined && !alreadyAllocated {
				setLastStartTimestamp(job)
			}
			if err == nil && podgroup_info.HasTasksToAllocate(job, true) {
				jobsOrderByQueues.PushJob(job)
				continue
			}
		} else {
			stmt.Discard()
		}
	}
}

func attemptToAllocateJob(ssn *framework.Session, stmt *framework.Statement, job *podgroup_info.PodGroupInfo) (allocated, pipelined bool) {
	queue := ssn.ClusterInfo.Queues[job.Queue]

	resReq := podgroup_info.GetTasksToAllocateInitResourceVector(job, ssn.PodSetOrderFn, ssn.TaskOrderFn, true, ssn.ClusterInfo.MinNodeGPUMemory)
	log.InfraLogger.V(3).Infof("Attempting to allocate job: <%v/%v> of queue <%v>, resources: <%v>",
		job.Namespace, job.Name, queue.Name, resReq)

	nodes := maps.Values(ssn.ClusterInfo.Nodes)
	if !common.AllocateJob(ssn, stmt, nodes, job, false) {
		log.InfraLogger.V(3).Infof("Could not allocate resources for job: <%v/%v> of queue <%v>",
			job.Namespace, job.Name, job.Queue)
		return false, false
	}
	pipelined = false
	if job.ShouldPipelineJob() {
		log.InfraLogger.V(3).Infof(
			"Some tasks were pipelined, setting all job to be pipelined for job: <%v/%v>",
			job.Namespace, job.Name)
		err := stmt.ConvertAllAllocatedToPipelined(job.UID)
		if err != nil {
			log.InfraLogger.Errorf(
				"Failed to covert tasks from allocated to pipelined for job: <%v/%v>, error: <%v>",
				job.Namespace, job.Name, err)
			return false, false
		}
		pipelined = true
	} else {
		log.InfraLogger.V(3).Infof("Succesfully allocated resources for job: <%v/%v>",
			job.Namespace, job.Name)
	}

	return true, pipelined
}

// unsuspendReadyWorkloads finds PodGroups that were suspended by KAI's
// preemption and unsuspends them so the workload controller can recreate
// pods. Suspended workloads have no pods, making them invisible to the
// main allocate loop. We unsuspend unconditionally — if resources are
// still contended, the workload will be preempted again in the next
// scheduling cycle.
func unsuspendReadyWorkloads(ssn *framework.Session) {
	if ssn.DynamicClient == nil {
		return
	}

	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if common.GetEvictionStrategy(job) != eviction_info.EvictionStrategySuspend {
			continue
		}
		// A suspended workload has zero alive tasks.
		if job.GetNumAliveTasks() > 0 {
			continue
		}
		suspended, err := framework.IsWorkloadSuspended(ssn.DynamicClient, job)
		if err != nil || !suspended {
			continue
		}

		log.InfraLogger.V(2).Infof("Unsuspending workload for PodGroup %s/%s",
			job.Namespace, job.Name)
		if err := framework.UnsuspendWorkload(ssn.DynamicClient, job); err != nil {
			log.InfraLogger.Errorf("Failed to unsuspend workload for PodGroup %s/%s: %v",
				job.Namespace, job.Name, err)
		}
	}
}

func setLastStartTimestamp(job *podgroup_info.PodGroupInfo) {
	timeNow := time.Now()
	job.LastStartTimestamp = &timeNow
}
