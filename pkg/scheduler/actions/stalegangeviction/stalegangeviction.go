// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package stalegangeviction

import (
	"time"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
)

type staleGangEviction struct {
}

func New() *staleGangEviction {
	return &staleGangEviction{}
}

func (action *staleGangEviction) Name() framework.ActionType {
	return framework.StaleGangEviction
}

func (action *staleGangEviction) Execute(ssn *framework.Session) {
	log.InfraLogger.V(2).Infof("Enter StaleGangEviction ...")
	defer log.InfraLogger.V(2).Infof("Leaving StaleGangEviction ...")
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if job.IsStale() {
			handleStaleJob(ssn, job)
		} else {
			handleNonStaleJob(job)
		}
	}
}

func handleStaleJob(ssn *framework.Session, job *podgroup_info.PodGroupInfo) {
	if job.StalenessInfo.TimeStamp == nil {
		timeNow := time.Now()
		job.StalenessInfo.TimeStamp = &timeNow
	}

	defaultGracePeriod := ssn.GetGlobalDefaultStalenessGracePeriod()
	if defaultGracePeriod < 0 { // negative duration means no eviction
		return
	}

	timeInStaleStatus := time.Since(*job.StalenessInfo.TimeStamp)
	if timeInStaleStatus < ssn.GetGlobalDefaultStalenessGracePeriod() {
		return
	}

	job.StalenessInfo.Stale = true

	var tasksToEvict []*pod_info.PodInfo
	for _, task := range job.GetAllPodsMap() {
		if pod_status.IsActiveAllocatedStatus(task.Status) {
			tasksToEvict = append(tasksToEvict, task)
		} else {
			log.InfraLogger.V(6).Infof("Not evicting task: <%v/%v> its status: <%v>",
				task.Namespace, task.Name, task.Status)
		}
	}
	evictionMetadata := eviction_info.EvictionMetadata{
		EvictionGangSize: len(tasksToEvict),
		Action:           string(framework.StaleGangEviction),
		Preemptor:        nil,
	}
	for _, task := range tasksToEvict {
		reason := api.GetGangEvictionMessage(task, job)
		if err := ssn.Evict(task, reason, evictionMetadata); err != nil {
			log.InfraLogger.Errorf("Failed to evict task: <%s/%s> of job <%s> err: %v",
				task.Namespace, task.Name, job.Name, err)
			continue
		}
		log.InfraLogger.V(3).Infof("Evicted task: <%v/%v> due its job being a stale job, its status: <%v>",
			task.Namespace, task.Name, task.Status)
	}
}

func handleNonStaleJob(job *podgroup_info.PodGroupInfo) {
	if job.StalenessInfo.TimeStamp != nil {
		job.StalenessInfo.TimeStamp = nil
		job.StalenessInfo.Stale = false
	}
}
