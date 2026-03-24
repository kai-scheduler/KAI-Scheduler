// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ksf "k8s.io/kube-scheduler/framework"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/configmap_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal"
	k8splugins "github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/metrics"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/scheduler_util"
)

var server *PluginServer

type Session struct {
	UID   types.UID
	Cache cache.Cache

	PodGroupInfos    map[common_info.PodGroupID]*podgroup_info.PodGroupInfo
	Nodes            map[string]*node_info.NodeInfo
	Queues           map[common_info.QueueID]*queue_info.QueueInfo
	ConfigMaps       map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo
	MinNodeGPUMemory int64

	GpuOrderFns                           []api.GpuOrderFn
	NodePreOrderFns                       []api.NodePreOrderFn
	NodeOrderFns                          []api.NodeOrderFn
	TaskOrderFns                          []common_info.CompareFn
	JobOrderFns                           []common_info.CompareFn
	QueueOrderFns                         []api.CompareQueueFn
	CanReclaimResourcesFns                []api.CanReclaimResourcesFn
	ReclaimVictimFilterFns                []api.VictimFilterFn
	PreemptVictimFilterFns                []api.VictimFilterFn
	ReclaimScenarioValidatorFns           []api.ScenarioValidatorFn
	PreemptScenarioValidatorFns           []api.ScenarioValidatorFn
	OnJobSolutionStartFns                 []api.OnJobSolutionStartFn
	GetQueueAllocatedResourcesFns         []api.QueueResource
	GetQueueDeservedResourcesFns          []api.QueueResource
	GetQueueFairShareFns                  []api.QueueResource
	IsNonPreemptibleJobOverQueueQuotaFns  []api.IsJobOverCapacityFn
	IsJobOverCapacityFns                  []api.IsJobOverCapacityFn
	IsTaskAllocationOnNodeOverCapacityFns []api.IsTaskAllocationOverCapacityFn
	PrePredicateFns                       []api.PrePredicateFn
	PredicateFns                          []api.PredicateFn

	Config          *conf.SchedulerConfiguration
	plugins         map[string]Plugin
	eventHandlers   []*EventHandler
	SchedulerParams conf.SchedulerParams
	mux             *http.ServeMux

	k8sPodState map[types.UID]k8s_internal.SessionState
}

func (ssn *Session) Statement() *Statement {
	return &Statement{ssn: ssn, sessionUID: ssn.UID}
}

func (ssn *Session) GetK8sStateForPod(uid types.UID) k8s_internal.SessionState {
	if ssn.k8sPodState == nil {
		ssn.k8sPodState = make(map[types.UID]k8s_internal.SessionState)
	}
	state, found := ssn.k8sPodState[uid]
	if found {
		return state
	}
	ssn.k8sPodState[uid] = k8s_internal.NewSessionState()
	return ssn.k8sPodState[uid]
}

func (ssn *Session) GetNodes() []ksf.NodeInfo {
	nodes, err := ssn.Cache.SnapshotSharedLister().List()
	if err != nil {
		log.InfraLogger.Errorf("Failed to list nodes: ", err)
		return nil
	}

	return nodes
}

func (ssn *Session) BindPod(pod *pod_info.PodInfo) error {
	if err := ssn.Cache.Bind(pod, pod.NodeName); err != nil {
		return err
	}

	if err := ssn.updatePodOnSession(pod, pod_status.Binding); err != nil {
		log.InfraLogger.Errorf("Failed to update pod <%s/%s> status from %s to %s in session: %v",
			pod.Namespace, pod.Name, pod.Status, pod_status.Binding, err)
		return err
	}

	metrics.UpdateTaskScheduleDuration(metrics.Duration(pod.Pod.CreationTimestamp.Time))
	return nil
}

func (ssn *Session) Evict(pod *pod_info.PodInfo, message string, evictionMetadata eviction_info.EvictionMetadata) error {
	podGroup, found := ssn.PodGroupInfos[pod.Job]
	if !found {
		return fmt.Errorf("could not evict pod <%v/%v> without podGroup. podGroupId: <%v>",
			pod.Namespace, pod.Name, pod.Job)
	}
	if err := ssn.Cache.Evict(pod.Pod, podGroup, evictionMetadata, message); err != nil {
		return err
	}
	if err := ssn.updatePodOnSession(pod, pod_status.Releasing); err != nil {
		return err
	}
	if err := ssn.updatePodOnNode(pod); err != nil {
		return err
	}
	for _, eh := range ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: pod,
			})
		}
	}
	return nil
}

func (ssn *Session) AddEventHandler(eh *EventHandler) {
	ssn.eventHandlers = append(ssn.eventHandlers, eh)
}

// FittingGPUs returns a list of GPUs that fit the pod, sorted by fit score (descending)
// Returned list will consist of:
// 1. Shared GPUs
// 2. api.WholeGpuIndicator (to indicate fit order of whole GPUs compared to shared ones)
// (For example:
// [api.WholeGpuIndicator, 0, 1]
// means that a whole (non-shared) GPU fits the best, then GPU 0, then GPU 1)
func (ssn *Session) FittingGPUs(node *node_info.NodeInfo, pod *pod_info.PodInfo) []string {
	filteredGPUs := filterGpusByEnoughResources(node, pod)
	sortedGPUs := ssn.sortGPUs(filteredGPUs, pod, node)

	return sortedGPUs
}

func filterGpusByEnoughResources(node *node_info.NodeInfo, pod *pod_info.PodInfo) []string {
	filteredGPUs := []string{}
	for gpuIdx := range node.UsedSharedGPUsMemory {
		if node.IsTaskFitOnGpuGroup(pod.ResReq, gpuIdx) {
			filteredGPUs = append(filteredGPUs, gpuIdx)
		}
	}
	if node.Idle.GPUs() > 0 || node.Releasing.GPUs() > 0 {
		for range int(node.Idle.GPUs()) + int(node.Releasing.GPUs()) {
			filteredGPUs = append(filteredGPUs, pod_info.WholeGpuIndicator)
		}
	}
	return filteredGPUs
}

func (ssn *Session) sortGPUs(filteredGPUs []string, pod *pod_info.PodInfo, node *node_info.NodeInfo) []string {
	gpuScores := map[float64][]string{}
	for _, gpuIdx := range filteredGPUs {
		score, err := ssn.GpuOrderFn(pod, node, gpuIdx)
		if err != nil {
			log.InfraLogger.Errorf("Error in calculating score for node/gpu %s/%d:%v", node.Name, gpuIdx, err)
			continue
		}

		gpuScores[score] = append(gpuScores[score], gpuIdx)
	}

	sortedGPUs := sortGPUs(gpuScores)
	return sortedGPUs
}

func (ssn *Session) FittingNode(task *pod_info.PodInfo, node *node_info.NodeInfo, writeFittingDelta bool) bool {
	var fitErrors *common_info.FitErrors
	if writeFittingDelta {
		fitErrors = common_info.NewFitErrors()
	}

	job := ssn.PodGroupInfos[task.Job]

	log.InfraLogger.V(6).Infof("Checking if task <%v/%v> is allocatable on node <%v>: <%v> vs. <%v>",
		task.Namespace, task.Name, node.Name, task.ResReq, node.Idle)
	allocatable, fitError := ssn.isTaskAllocatableOnNode(task, job, node, writeFittingDelta)
	if !allocatable {
		if fitError != nil && writeFittingDelta {
			fitErrors.SetNodeError(node.Name, fitError)
			job.SetTaskFitError(task, fitErrors)
		}
		return false
	}

	log.InfraLogger.V(6).Infof("Running predicates for task <%v/%v> on node <%v>",
		task.Namespace, task.Name, node.Name)
	if err := ssn.PredicateFn(task, job, node); err != nil {
		log.InfraLogger.V(6).Infof("Predicates failed for task <%s/%s> on node <%s>: %v",
			task.Namespace, task.Name, node.Name, err)
		if writeFittingDelta {
			fitErrors.SetNodeError(node.Name, err)
			job.SetTaskFitError(task, fitErrors)
		}
		return false
	}
	return true
}

func (ssn *Session) OrderedNodesByTask(nodes []*node_info.NodeInfo, task *pod_info.PodInfo) []*node_info.NodeInfo {
	var (
		nodeScores = make(map[float64][]*node_info.NodeInfo)
		mutex      sync.Mutex
		wg         sync.WaitGroup
	)

	ssn.NodePreOrderFn(task, nodes)

	for _, node := range nodes {
		wg.Add(1)
		go func(node *node_info.NodeInfo) {
			defer wg.Done()
			score, err := ssn.NodeOrderFn(task, node)
			if err != nil {
				log.InfraLogger.Errorf("Error in Calculating Priority for the node:%v", err)
				return
			}

			mutex.Lock()
			nodeScores[score] = append(nodeScores[score], node)
			mutex.Unlock()

			log.InfraLogger.V(5).Infof("Overall priority node score of node <%v> for task <%v/%v> is: %f",
				node.Name, task.Namespace, task.Name, score)
		}(node)
	}

	wg.Wait()
	return sortNodesByScore(nodeScores)
}

func (ssn *Session) isTaskAllocatableOnNode(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo,
	node *node_info.NodeInfo, writeFittingDelta bool) (bool, *common_info.FitError) {
	allocatable := true
	var fitError *common_info.FitError = nil

	if !node.IsTaskAllocatableOnReleasingOrIdle(task) {
		allocatable = false
		log.InfraLogger.V(6).Infof("Not enough resources for task: <%s/%s>, init requested: <%v>. "+
			"Node <%s> with limited resources, releasing: <%v>, idle: <%v>",
			task.Namespace, task.Name, task.ResReq, node.Name, node.Releasing, node.Idle)
		if writeFittingDelta {
			if taskAllocatable := node.IsTaskAllocatable(task); !taskAllocatable {
				fitError = node.FittingError(task, len(job.PodInfos) > 1)
			}
		}
	}
	return allocatable, fitError
}

func (ssn *Session) String() string {
	msg := fmt.Sprintf("Session %v: \n", ssn.UID)

	for _, job := range ssn.PodGroupInfos {
		msg = fmt.Sprintf("%s%v\n", msg, job)
	}

	for _, node := range ssn.Nodes {
		msg = fmt.Sprintf("%s%v\n", msg, node)
	}

	return msg

}

func (ssn *Session) updatePodOnNode(pod *pod_info.PodInfo) error {
	node, found := ssn.Nodes[pod.NodeName]
	if !found {
		log.InfraLogger.Errorf("Failed to find node: %v", pod.NodeName)
		return fmt.Errorf("node doesnt exist on cluster")
	}
	err := node.UpdateTask(pod)
	if err != nil {
		log.InfraLogger.Errorf("Failed to update task <%v/%v> in Session <%v>: %v",
			pod.Namespace, pod.Name, ssn.UID, err)
	}
	return err
}

func (ssn *Session) updatePodOnSession(pod *pod_info.PodInfo, status pod_status.PodStatus) error {
	job, found := ssn.PodGroupInfos[pod.Job]
	if !found {
		log.InfraLogger.Errorf("Failed to found Job <%s> in Session <%s> index when binding.",
			pod.Job, ssn.UID)
		return fmt.Errorf("failed to find job %s", pod.Job)
	}

	err := job.UpdateTaskStatus(pod, status)
	if err != nil {
		log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
			pod.Namespace, pod.Name, status, ssn.UID, err)
	}
	return err
}

func (ssn *Session) clear() {
	ssn.PodGroupInfos = nil
	ssn.Nodes = nil
	ssn.plugins = nil
	ssn.eventHandlers = nil
	ssn.TaskOrderFns = nil
	ssn.JobOrderFns = nil
}

func openSession(cache cache.Cache, sessionId types.UID, schedulerParams conf.SchedulerParams, mux *http.ServeMux) (*Session, error) {
	ssn := &Session{
		UID:   sessionId,
		Cache: cache,

		PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{},
		Nodes:         map[string]*node_info.NodeInfo{},
		Queues:        map[common_info.QueueID]*queue_info.QueueInfo{},

		plugins:         map[string]Plugin{},
		SchedulerParams: schedulerParams,
		mux:             mux,
		k8sPodState:     map[types.UID]k8s_internal.SessionState{},
	}

	log.InfraLogger.V(2).Infof("Taking cluster snapshot ...")
	snapshot, err := cache.Snapshot()
	if err != nil {
		return nil, err
	}

	ssn.PodGroupInfos = snapshot.PodGroupInfos
	ssn.Nodes = snapshot.Nodes
	ssn.Queues = snapshot.Queues
	ssn.ConfigMaps = snapshot.ConfigMaps
	ssn.MinNodeGPUMemory = snapshot.MinNodeGPUMemory

	log.InfraLogger.V(2).Infof("Session %v with <%d> Jobs, <%d> Queues and <%d> Nodes",
		ssn.UID, len(ssn.PodGroupInfos), len(ssn.Queues), len(ssn.Nodes))

	return ssn, nil
}

func closeSession(ssn *Session) {
	log.InfraLogger.V(6).Infof("Close Session %v with <%d> Jobs and <%d> Queues",
		ssn.UID, len(ssn.PodGroupInfos), len(ssn.Queues))

	// Push all jobs for status update into the channel
	for _, job := range ssn.PodGroupInfos {
		if err := ssn.Cache.RecordJobStatusEvent(job); err != nil {
			log.InfraLogger.Errorf("Failed to record job status event for job <%s>: %v", job.Name, err)
		}
	}

	ssn.clear()
	stopCh := make(chan struct{})
	ssn.Cache.WaitForWorkers(stopCh)

	log.InfraLogger.V(6).Infof("Done updating job statuses for session: %v", ssn.UID)
}

func (ssn *Session) GetMaxNumberConsolidationPreemptees() int {
	return ssn.SchedulerParams.MaxNumberConsolidationPreemptees
}

func (ssn *Session) OverrideMaxNumberConsolidationPreemptees(maxPreemptees int) {
	ssn.SchedulerParams.MaxNumberConsolidationPreemptees = maxPreemptees
}

func (ssn *Session) IsInferencePreemptible() bool {
	return ssn.SchedulerParams.IsInferencePreemptible
}

// OverrideInferencePreemptible overrides the value returned by IsInferencePreemptible. Use for testing purposes.
func (ssn *Session) OverrideInferencePreemptible(isInferencePreemptible bool) {
	ssn.SchedulerParams.IsInferencePreemptible = isInferencePreemptible
}

func (ssn *Session) UseSchedulingSignatures() bool {
	return ssn.SchedulerParams.UseSchedulingSignatures
}

func (ssn *Session) GetJobsDepth(action ActionType) int {
	maxJobs, foundForAction := ssn.Config.QueueDepthPerAction[string(action)]
	if !foundForAction {
		return scheduler_util.QueueCapacityInfinite
	}
	return maxJobs
}

func (ssn *Session) CountLeafQueues() int {
	cnt := 0
	for _, queue := range ssn.Queues {
		if queue.IsLeafQueue() {
			cnt++
		}
	}
	return cnt
}

func (ssn *Session) ScheduleCSIStorage() bool {
	return ssn.SchedulerParams.ScheduleCSIStorage
}

func (ssn *Session) NodePoolName() string {
	if ssn.SchedulerParams.PartitionParams == nil {
		return ""
	}
	return ssn.SchedulerParams.PartitionParams.NodePoolLabelValue
}

func (ssn *Session) AllowConsolidatingReclaim() bool {
	return ssn.SchedulerParams.AllowConsolidatingReclaim
}

func (ssn *Session) GetGlobalDefaultStalenessGracePeriod() time.Duration {
	return ssn.SchedulerParams.GlobalDefaultStalenessGracePeriod
}

// OverrideGlobalDefaultStalenessGracePeriod overrides the value returned by GetGlobalDefaultStalenessGracePeriod. Use for testing purposes.
func (ssn *Session) OverrideGlobalDefaultStalenessGracePeriod(t time.Duration) {
	ssn.SchedulerParams.GlobalDefaultStalenessGracePeriod = t
}

// OverrideAllowConsolidatingReclaim overrides the value returned by allowConsolidatingReclaim. Use for testing purposes.
func (ssn *Session) OverrideAllowConsolidatingReclaim(allowConsolidatingReclaim bool) {
	ssn.SchedulerParams.AllowConsolidatingReclaim = allowConsolidatingReclaim
}

func (ssn *Session) GetSchedulerName() string {
	return ssn.SchedulerParams.SchedulerName
}

func (ssn *Session) OverrideSchedulerName(name string) {
	ssn.SchedulerParams.SchedulerName = name
}

func (ssn *Session) InternalK8sPlugins() *k8splugins.K8sPlugins {
	return ssn.Cache.InternalK8sPlugins()
}

func sortNodesByScore(nodeScores map[float64][]*node_info.NodeInfo) []*node_info.NodeInfo {
	var nodesInorder []*node_info.NodeInfo
	var keys []float64
	for key := range nodeScores {
		keys = append(keys, key)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(keys)))
	for _, key := range keys {
		nodes := sortNodesByName(nodeScores[key])
		nodesInorder = append(nodesInorder, nodes...)
	}
	return nodesInorder
}

func sortNodesByName(nodes []*node_info.NodeInfo) []*node_info.NodeInfo {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})
	return nodes
}

func sortGPUs(gpuScores map[float64][]string) []string {
	var scores []float64
	for k := range gpuScores {
		scores = append(scores, k)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(scores)))
	var sortedGPUs []string
	for _, gpuScore := range scores {
		sortedGPUs = append(sortedGPUs, gpuScores[gpuScore]...)
	}
	return sortedGPUs
}
