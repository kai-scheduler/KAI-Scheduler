// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package minruntime

import (
	"slices"
	"time"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultReclaimMinRuntimeConfig = "defaultReclaimMinRuntime"
	defaultPreemptMinRuntimeConfig = "defaultPreemptMinRuntime"
	reclaimResolveMethod           = "reclaimResolveMethod"
	resolveMethodLCA               = "lca"
	resolveMethodQueue             = "queue"
)

type minruntimePlugin struct {
	defaultReclaimMinRuntime metav1.Duration
	defaultPreemptMinRuntime metav1.Duration
	reclaimResolveMethod     string
	queues                   map[common_info.QueueID]*queue_info.QueueInfo

	preemptProtectionCache map[common_info.PodGroupID]bool
	reclaimProtectionCache map[common_info.PodGroupID]map[common_info.PodGroupID]bool

	resolver *resolver
}

func parseMinRuntime(arguments framework.PluginArguments, minRuntimeConfig string) metav1.Duration {
	minRuntime, err := arguments.GetDuration(minRuntimeConfig, 0*time.Second)
	if err != nil {
		log.InfraLogger.Errorf("Failed to parse %v as duration: %v, using default value 0s", minRuntimeConfig, err)
		return metav1.Duration{Duration: 0 * time.Second}
	}
	if minRuntime < 0 {
		log.InfraLogger.Errorf("Parsed %v (%v) is negative, using default value 0s", minRuntimeConfig, minRuntime)
		return metav1.Duration{Duration: 0 * time.Second}
	}
	return metav1.Duration{Duration: minRuntime}
}

func New(arguments framework.PluginArguments) framework.Plugin {
	plugin := &minruntimePlugin{}

	plugin.defaultReclaimMinRuntime = parseMinRuntime(arguments, defaultReclaimMinRuntimeConfig)
	plugin.defaultPreemptMinRuntime = parseMinRuntime(arguments, defaultPreemptMinRuntimeConfig)

	validResolveMethods := []string{resolveMethodLCA, resolveMethodQueue}
	plugin.reclaimResolveMethod = arguments[reclaimResolveMethod]
	if len(plugin.reclaimResolveMethod) == 0 {
		plugin.reclaimResolveMethod = resolveMethodLCA
	}
	if !slices.Contains(validResolveMethods, plugin.reclaimResolveMethod) {
		log.InfraLogger.Errorf("Invalid reclaim resolve method %v, using default value %v", plugin.reclaimResolveMethod, resolveMethodLCA)
		plugin.reclaimResolveMethod = resolveMethodLCA
	}
	// setup caches on plugin init, but they will be reset on session open anyway
	plugin.preemptProtectionCache = make(map[common_info.PodGroupID]bool)
	plugin.reclaimProtectionCache = make(map[common_info.PodGroupID]map[common_info.PodGroupID]bool)

	return plugin
}

func (mr *minruntimePlugin) Name() string {
	return "minruntime"
}

func (mr *minruntimePlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddReclaimVictimFilterFn(mr.reclaimFilterFn)
	ssn.AddPreemptVictimFilterFn(mr.preemptFilterFn)
	ssn.AddReclaimScenarioValidatorFn(mr.reclaimScenarioValidatorFn)
	ssn.AddPreemptScenarioValidatorFn(mr.preemptScenarioValidatorFn)
	mr.queues = ssn.ClusterInfo.Queues
	mr.preemptProtectionCache = make(map[common_info.PodGroupID]bool)
	mr.reclaimProtectionCache = make(map[common_info.PodGroupID]map[common_info.PodGroupID]bool)
	mr.resolver = NewResolver(mr.queues, mr.defaultPreemptMinRuntime, mr.defaultReclaimMinRuntime)
}

func (mr *minruntimePlugin) OnSessionClose(ssn *framework.Session) {
	mr.queues = nil
	mr.preemptProtectionCache = nil
	mr.reclaimProtectionCache = nil
	mr.resolver = nil
}

func (mr *minruntimePlugin) reclaimFilterFn(pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
	// always return true for elastic jobs, they are checked in scenario validator
	if victim.IsElastic() {
		return true
	}
	return !mr.isReclaimMinRuntimeProtected(pendingJob, victim)
}

func (mr *minruntimePlugin) preemptFilterFn(pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
	// always return true for elastic jobs, they are checked in scenario validator
	if victim.IsElastic() {
		return true
	}
	return !mr.isPreemptMinRuntimeProtected(pendingJob, victim)
}

func (mr *minruntimePlugin) reclaimScenarioValidatorFn(scenario api.ScenarioInfo) bool {
	reclaimer := scenario.GetPreemptor()
	for _, victimInfo := range scenario.GetVictims() {
		if !victimInfo.Job.IsElastic() {
			continue
		}
		protected := mr.isReclaimMinRuntimeProtected(reclaimer, victimInfo.Job)
		if !protected {
			continue
		}

		if !validVictimForMinAvailable(victimInfo) {
			return false
		}
	}
	return true
}

func (mr *minruntimePlugin) preemptScenarioValidatorFn(scenario api.ScenarioInfo) bool {
	preemptor := scenario.GetPreemptor()
	for _, victimInfo := range scenario.GetVictims() {
		if !victimInfo.Job.IsElastic() {
			continue
		}
		protected := mr.isPreemptMinRuntimeProtected(preemptor, victimInfo.Job)
		if !protected {
			continue
		}

		if !validVictimForMinAvailable(victimInfo) {
			return false
		}
	}
	return true
}

func (mr *minruntimePlugin) isReclaimMinRuntimeProtected(pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
	if cached, ok := mr.reclaimProtectionCache[pendingJob.UID][victim.UID]; ok {
		return cached
	}
	pendingQueue := mr.queues[pendingJob.Queue]
	victimQueue := mr.queues[victim.Queue]

	minRuntime, err := mr.resolver.getReclaimMinRuntime(mr.reclaimResolveMethod, pendingQueue, victimQueue)
	if err != nil {
		log.InfraLogger.Errorf("Failed to get reclaim min runtime for pending job %v and victim %v: %v", pendingJob.NamespacedName, victim.NamespacedName, err)
		minRuntime = mr.defaultReclaimMinRuntime
	}

	// If the victim's last start time plus minimum runtime is greater than current time,
	// the victim is protected from reclaim
	if victim.LastStartTimestamp != nil && !victim.LastStartTimestamp.IsZero() {
		protectedUntil := victim.LastStartTimestamp.Add(minRuntime.Duration)
		protected := time.Now().Before(protectedUntil)
		mr.cacheReclaimProtection(pendingJob, victim, protected)
		return protected
	}
	return false
}

func (mr *minruntimePlugin) isPreemptMinRuntimeProtected(_ *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo) bool {
	if cached, ok := mr.preemptProtectionCache[victim.UID]; ok {
		return cached
	}
	victimQueue := mr.queues[victim.Queue]

	minRuntime, err := mr.resolver.getPreemptMinRuntime(victimQueue)
	if err != nil {
		log.InfraLogger.Errorf("Failed to get preempt min runtime for victim %v: %v", victim.NamespacedName, err)
		minRuntime = mr.defaultPreemptMinRuntime
	}

	// If the victim's last start time plus minimum runtime is greater than current time,
	// the victim is protected from preemption
	if victim.LastStartTimestamp != nil && !victim.LastStartTimestamp.IsZero() {
		protectedUntil := victim.LastStartTimestamp.Add(minRuntime.Duration)
		protected := time.Now().Before(protectedUntil)
		mr.cachePreemptProtection(victim, protected)
		return protected
	}

	return false
}

func (mr *minruntimePlugin) cachePreemptProtection(victim *podgroup_info.PodGroupInfo, protected bool) {
	mr.preemptProtectionCache[victim.UID] = protected
}

func (mr *minruntimePlugin) cacheReclaimProtection(pendingJob *podgroup_info.PodGroupInfo, victim *podgroup_info.PodGroupInfo, protected bool) {
	if mr.reclaimProtectionCache[pendingJob.UID] == nil {
		mr.reclaimProtectionCache[pendingJob.UID] = make(map[common_info.PodGroupID]bool)
	}
	mr.reclaimProtectionCache[pendingJob.UID][victim.UID] = protected
}

func validVictimForMinAvailable(victimInfo *api.VictimInfo) bool {
	numVictimTasksPerSubGroup := map[string]int32{}
	for _, task := range victimInfo.Tasks {
		subGroupName := podgroup_info.DefaultSubGroup
		if task.SubGroupName != "" {
			subGroupName = task.SubGroupName
		}
		numVictimTasksPerSubGroup[subGroupName]++
	}

	numCurrentlyRunningSubGroup := map[string]int32{}
	for subGroupName := range numVictimTasksPerSubGroup {
		numCurrentlyRunningSubGroup[subGroupName] = int32(victimInfo.Job.GetSubGroups()[subGroupName].GetNumActiveUsedTasks())
	}

	for subGroupName, numVictims := range numVictimTasksPerSubGroup {
		subGroupCurrentlyRunning := numCurrentlyRunningSubGroup[subGroupName]
		if victimInfo.Job.GetSubGroups()[subGroupName].GetMinAvailable() > subGroupCurrentlyRunning-numVictims {
			return false
		}
	}
	return true
}
