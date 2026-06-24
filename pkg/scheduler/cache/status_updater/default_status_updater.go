// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"gomodules.xyz/jsonpatch/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	kai "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/utils"
)

const (
	podType      = "pod"
	podGroupType = "podgroup"

	// Eviction event annotations
	evictionGangSize                    = "num-evicted-pods"
	evictorPodGroupNameAnnotations      = "evictor-pod-group-name"
	evictorPodGroupNamespaceAnnotations = "evictor-pod-group-namespace"
	evictorActionType                   = "evictor-action-type"
)

type updatePayloadKey string

type updatePayload struct {
	key        updatePayloadKey
	objectType string
}

type inflightUpdate struct {
	object       runtime.Object
	patchData    []byte
	updateStatus bool
	subResources []string
}

type defaultStatusUpdater struct {
	kubeClient        kubernetes.Interface
	kaiClient         kai.Interface
	recorder          record.EventRecorder
	detailedFitErrors bool
	nodePoolLabelKey  string

	numberOfWorkers   int
	updateQueueIn     chan *updatePayload
	updateQueueOut    chan *updatePayload
	updateQueueBuffer []*updatePayload

	inFlightPodGroups sync.Map
	inFlightPods      sync.Map

	appliedPodGroupUpdates sync.Map
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;update;patch;delete;list;get;watch

func New(
	kubeClient kubernetes.Interface,
	kaiClient kai.Interface,
	recorder record.EventRecorder,
	numberOfWorkers int,
	detailedFitErrors bool,
	nodePoolLabelKey string,
) *defaultStatusUpdater {
	return &defaultStatusUpdater{
		kubeClient:        kubeClient,
		kaiClient:         kaiClient,
		recorder:          recorder,
		detailedFitErrors: detailedFitErrors,
		nodePoolLabelKey:  nodePoolLabelKey,

		numberOfWorkers:   numberOfWorkers,
		updateQueueIn:     make(chan *updatePayload),
		updateQueueOut:    make(chan *updatePayload),
		updateQueueBuffer: make([]*updatePayload, 0, 1024),
	}
}

func (su *defaultStatusUpdater) Evicted(
	evictedPodGroup *enginev2alpha2.PodGroup,
	evictionMetadata eviction_info.EvictionMetadata,
	message string,
) {
	evictionEventMetadata := map[string]string{
		evictionGangSize:  strconv.Itoa(evictionMetadata.EvictionGangSize),
		evictorActionType: evictionMetadata.Action,
	}
	if evictionMetadata.Preemptor != nil {
		evictionEventMetadata[evictorPodGroupNameAnnotations] = evictionMetadata.Preemptor.Name
		evictionEventMetadata[evictorPodGroupNamespaceAnnotations] =
			evictionMetadata.Preemptor.Namespace
	}

	su.recorder.AnnotatedEventf(evictedPodGroup, evictionEventMetadata, v1.EventTypeNormal, "Evict",
		message)

	nodepool := utils.GetNodePoolNameFromLabels(evictedPodGroup.Labels, su.nodePoolLabelKey)
	metrics.RecordPodGroupEvictedPods(
		evictedPodGroup.Name,
		evictedPodGroup.Namespace,
		string(evictedPodGroup.UID),
		nodepool,
		evictionMetadata.Action,
		evictionMetadata.EvictionGangSize,
	)
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=update;patch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get;list;watch;create;delete;update;patch

func (su *defaultStatusUpdater) Bound(
	pod *v1.Pod, hostname string,
	bindError error, nodePoolName string,
) error {
	if bindError != nil {
		message := fmt.Sprintf("Failed to bind pod %v/%v to node %v. %v", pod.Namespace,
			pod.Name, hostname, bindError)
		log.InfraLogger.Errorf(message)
		su.recorder.Eventf(pod, v1.EventTypeWarning, "FailedBinding", message)
		conditionUpdateError := su.updatePodCondition(pod, &v1.PodCondition{
			Type:    v1.PodScheduled,
			Status:  v1.ConditionFalse,
			Reason:  "BindingError",
			Message: message,
		})
		if conditionUpdateError != nil {
			bindError = errors.Join(bindError, conditionUpdateError)
		}
		return bindError
	} else {
		su.recorder.Eventf(
			pod, v1.EventTypeNormal,
			"Scheduled", "Successfully assigned pod %v/%v to node %v at node-pool %v",
			pod.Namespace, pod.Name, hostname, nodePoolName,
		)
	}

	return bindError
}

func (su *defaultStatusUpdater) PreBind(pod *v1.Pod) {
	// Delete any pending status updates for this pod - after this binding, they will become no longer relevant
	su.inFlightPods.Delete(su.keyForPodStatusPayload(pod.Name, pod.Namespace, pod.UID))
}

func (su *defaultStatusUpdater) Pipelined(pod *v1.Pod, message string) {
	su.recorder.Eventf(pod, v1.EventTypeNormal, "Pipelined", message)
}

func (su *defaultStatusUpdater) PatchPodLabels(pod *v1.Pod, labels map[string]any) {
	log.InfraLogger.V(6).Infof("Patching pod labels for %s/%s", pod.Namespace, pod.Name)

	patchBytes, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"labels": labels,
		},
	})

	if err != nil {
		log.InfraLogger.Errorf("Failed to create patch for pod labels <%s/%s>: %v",
			pod.Namespace, pod.Name, err)
		return
	}

	su.pushToUpdateQueue(
		&updatePayload{
			key:        su.keyForPodLabelsPayload(pod.Name, pod.Namespace, pod.UID),
			objectType: podType,
		},
		&inflightUpdate{
			object:    pod,
			patchData: patchBytes,
		},
	)
}

func (su *defaultStatusUpdater) RecordJobStatusEvent(job *podgroup_info.PodGroupInfo) error {
	var err error
	var patchData []byte
	if patchData, err = su.updatePodGroupAnnotations(job); err != nil {
		log.InfraLogger.V(7).Warnf("Failed to update podgroup annotations, error: %s", err)
	}
	if job.StalenessInfo.Stale {
		su.recordStaleJobEvent(job)
	}
	if err := su.recordInvalidSubGroupPodsEvents(job); err != nil {
		return err
	}

	updatePodgroupStatus := false
	if job.GetNumPendingTasks() > 0 || job.GetNumGatedTasks() > 0 {
		if !job.IsReadyForScheduling() {
			su.recordJobNotReadyEvent(job)
			return nil
		}
		if job.ScenarioSearchUnresolved != nil {
			if err := su.recordUnschedulablePodsConditions(job); err != nil {
				return err
			}
			if err := su.recordScenarioSearchUnresolvedPodsEvents(job); err != nil {
				return err
			}
			updatePodgroupStatus = su.recordScenarioSearchUnresolvedPodGroup(job)
		} else {
			if err := su.recordUnschedulablePodsEvents(job); err != nil {
				return err
			}
		}
		updatePodgroupStatus = su.recordUnschedulablePodGroup(job) || updatePodgroupStatus
	}

	if len(patchData) > 0 || updatePodgroupStatus {
		su.pushToUpdateQueue(
			&updatePayload{
				key:        su.keyForPodGroupPayload(job.PodGroup.Name, job.PodGroup.Namespace, job.PodGroup.UID),
				objectType: podGroupType,
			},
			&inflightUpdate{
				object:       job.PodGroup,
				patchData:    patchData,
				updateStatus: updatePodgroupStatus,
			},
		)
	}

	return nil
}

func (su *defaultStatusUpdater) markTaskUnschedulable(pod *v1.Pod, message string, updatePodCondition bool) error {
	log.InfraLogger.V(6).Infof("setting message for task: %v", pod.Name)
	su.recorder.Eventf(pod, v1.EventTypeWarning, v1.PodReasonUnschedulable, message)

	if updatePodCondition {
		if err := su.updatePodCondition(pod, &v1.PodCondition{
			Type:    v1.PodScheduled,
			Status:  v1.ConditionFalse,
			Reason:  v1.PodReasonUnschedulable,
			Message: message,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (su *defaultStatusUpdater) markTaskScenarioSearchUnresolved(pod *v1.Pod, message string) error {
	log.InfraLogger.V(6).Infof("setting scenario search unresolved message for task: %v", pod.Name)
	su.recorder.Eventf(pod, v1.EventTypeWarning, string(enginev2alpha2.ScenarioSearchUnresolved), message)

	return su.updatePodCondition(pod, &v1.PodCondition{
		Type:    v1.PodConditionType(enginev2alpha2.ScenarioSearchUnresolved),
		Status:  v1.ConditionTrue,
		Reason:  string(enginev2alpha2.ScenarioSearchUnresolved),
		Message: message,
	})
}

func (su *defaultStatusUpdater) recordStaleJobEvent(job *podgroup_info.PodGroupInfo) {
	subGroupMessages := ""

	totalActivePods := 0
	totalMinAvailable := int32(0)
	for _, subGroup := range job.GetAllPodSets() {
		activeTasks := subGroup.GetNumActiveUsedTasks()
		minAvailable := subGroup.GetMinAvailable()
		totalActivePods += activeTasks
		totalMinAvailable += minAvailable

		if !subGroup.IsGangSatisfied() && subGroup.GetName() != podgroup_info.DefaultSubGroup {
			subGroupMessages += fmt.Sprintf(", subGroup %s minMember is %d and %d pods are active",
				subGroup.GetName(), minAvailable, activeTasks)
		}
	}

	message := fmt.Sprintf("Job is stale. %d pods are active, minMember is %d", totalActivePods, totalMinAvailable) + subGroupMessages

	su.recorder.Eventf(job.PodGroup, v1.EventTypeNormal, "StaleJob", message)
}

func (su *defaultStatusUpdater) recordJobNotReadyEvent(job *podgroup_info.PodGroupInfo) {
	message := fmt.Sprintf("Job is not ready for scheduling.")
	for _, subGroup := range job.GetAllPodSets() {
		if !subGroup.IsReadyForScheduling() {
			if subGroup.GetName() == podgroup_info.DefaultSubGroup {
				message = message + fmt.Sprintf(" Waiting for %d pods, currently %d exist, %d are gated",
					subGroup.GetMinAvailable(), subGroup.GetNumAliveTasks(), subGroup.GetNumGatedTasks())
			} else {
				message += fmt.Sprintf(" Waiting for %d pods for SubGroup %s, currently %d exist, %d are gated.",
					subGroup.GetMinAvailable(), subGroup.GetName(), subGroup.GetNumAliveTasks(), subGroup.GetNumGatedTasks())
			}
		}
	}

	su.recorder.Eventf(job.PodGroup, v1.EventTypeNormal, "NotReady", message)
}

func (su *defaultStatusUpdater) markPodGroupUnschedulable(job *podgroup_info.PodGroupInfo, message string) bool {
	su.recorder.Event(job.PodGroup, v1.EventTypeNormal, enginev2alpha2.PodGroupReasonUnschedulable, message)

	if job.GetActiveAllocatedTasksCount() > 0 {
		// Don't update podgroup condition if there are any allocated pods (RUN-20673)
		return false
	}

	unschedulableExplanations := make([]enginev2alpha2.UnschedulableExplanation, 0, len(job.JobFitErrors))
	for _, jobFitError := range job.JobFitErrors {
		unschedulableExplanations = append(unschedulableExplanations, jobFitError.ToUnschedulableExplanation())
	}

	return su.updatePodGroupSchedulingCondition(job.PodGroup, &enginev2alpha2.SchedulingCondition{
		Type:     enginev2alpha2.UnschedulableOnNodePool,
		NodePool: utils.GetNodePoolNameFromLabels(job.PodGroup.Labels, su.nodePoolLabelKey),
		Reason:   enginev2alpha2.PodGroupReasonUnschedulable,
		Message:  message,
		Status:   v1.ConditionTrue,
		Reasons:  unschedulableExplanations,
	})
}

func (su *defaultStatusUpdater) markPodGroupScenarioSearchUnresolved(job *podgroup_info.PodGroupInfo, message string) bool {
	su.recorder.Event(job.PodGroup, v1.EventTypeNormal, string(enginev2alpha2.ScenarioSearchUnresolved), message)

	return su.updatePodGroupSchedulingCondition(job.PodGroup, &enginev2alpha2.SchedulingCondition{
		Type:     enginev2alpha2.ScenarioSearchUnresolved,
		NodePool: utils.GetNodePoolNameFromLabels(job.PodGroup.Labels, su.nodePoolLabelKey),
		Reason:   string(enginev2alpha2.ScenarioSearchUnresolved),
		Message:  message,
		Status:   v1.ConditionTrue,
	})
}

func (su *defaultStatusUpdater) updatePodCondition(pod *v1.Pod, condition *v1.PodCondition) error {
	log.InfraLogger.V(6).Infof(
		"Updating pod condition for %s/%s to (%s==%s)",
		pod.Namespace, pod.Name, condition.Type, condition.Status)
	if k8s_internal.UpdatePodCondition(&pod.Status, condition) {
		statusPatchBaseObject := v1.PodStatus{}
		statusPatchBaseObject.Conditions = pod.Status.Conditions
		podStatusPatchBytes, err := json.Marshal(statusPatchBaseObject)
		if err != nil {
			return err
		}

		patchData := []byte(fmt.Sprintf(`{"status":%s}`, string(podStatusPatchBytes)))

		su.pushToUpdateQueue(
			&updatePayload{
				key:        su.keyForPodStatusPayload(pod.Name, pod.Namespace, pod.UID),
				objectType: podType,
			},
			&inflightUpdate{
				object:       pod,
				patchData:    patchData,
				subResources: []string{"status"},
			},
		)
	}
	return nil
}

func (su *defaultStatusUpdater) recordUnschedulablePodsEvents(job *podgroup_info.PodGroupInfo) error {
	// Update podCondition for tasks Allocated and Pending before job discarded
	var errs []error
	for _, taskInfo := range job.PodStatusIndex[pod_status.Pending] {
		if job.IsInvalidSubGroupTask(taskInfo.UID) {
			continue
		}

		msg := su.unschedulableTaskMessage(job, taskInfo)
		log.InfraLogger.V(6).Infof("setting message for task: %v, %v", taskInfo.Name, msg)
		updatePodCondition := utils.GetMarkUnschedulableValue(job.PodGroup.Spec.MarkUnschedulable)
		if err := su.markTaskUnschedulable(taskInfo.Pod, msg, updatePodCondition); err != nil {
			errs = append(errs, fmt.Errorf("failed to update unschedulable task status <%s/%s>: %v",
				taskInfo.Namespace, taskInfo.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (su *defaultStatusUpdater) recordUnschedulablePodsConditions(job *podgroup_info.PodGroupInfo) error {
	if !utils.GetMarkUnschedulableValue(job.PodGroup.Spec.MarkUnschedulable) {
		return nil
	}

	var errs []error
	for _, taskInfo := range job.PodStatusIndex[pod_status.Pending] {
		if job.IsInvalidSubGroupTask(taskInfo.UID) {
			continue
		}

		msg := su.unschedulableTaskMessage(job, taskInfo)
		if err := su.updatePodCondition(taskInfo.Pod, &v1.PodCondition{
			Type:    v1.PodScheduled,
			Status:  v1.ConditionFalse,
			Reason:  v1.PodReasonUnschedulable,
			Message: msg,
		}); err != nil {
			errs = append(errs, fmt.Errorf("failed to update unschedulable task status <%s/%s>: %v",
				taskInfo.Namespace, taskInfo.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (su *defaultStatusUpdater) unschedulableTaskMessage(
	job *podgroup_info.PodGroupInfo, taskInfo *pod_info.PodInfo,
) string {
	msg := common_info.DefaultPodError
	fitError := job.TasksFitErrors[taskInfo.UID]
	if fitError != nil {
		msg = fitError.Error()

		if su.detailedFitErrors {
			msg = fitError.DetailedError()
		} else {
			log.InfraLogger.V(6).Infof("Full fit error: %s", fitError.DetailedError())
		}
	} else if len(job.JobFitErrors) > 0 {
		msg = fmt.Sprintf("%s", common_info.JobFitErrorsToMessage(job.JobFitErrors))
	}

	return su.addNodePoolPrefixIfNeeded(job, msg)
}

func (su *defaultStatusUpdater) recordScenarioSearchUnresolvedPodsEvents(job *podgroup_info.PodGroupInfo) error {
	var errs []error
	message := scenarioSearchUnresolvedMessage(job.ScenarioSearchUnresolved)
	for _, taskInfo := range job.PodStatusIndex[pod_status.Pending] {
		if job.IsInvalidSubGroupTask(taskInfo.UID) {
			continue
		}
		if err := su.markTaskScenarioSearchUnresolved(taskInfo.Pod, message); err != nil {
			errs = append(errs, fmt.Errorf("failed to update scenario search unresolved task status <%s/%s>: %v",
				taskInfo.Namespace, taskInfo.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (su *defaultStatusUpdater) recordInvalidSubGroupPodsEvents(job *podgroup_info.PodGroupInfo) error {
	var errs []error

	for _, taskInfo := range job.GetInvalidSubGroupTasks() {
		msg := common_info.DefaultPodError
		if fitError := job.TasksFitErrors[taskInfo.UID]; fitError != nil {
			msg = fitError.Error()
			if su.detailedFitErrors {
				msg = fitError.DetailedError()
			}
		}

		msg = su.addNodePoolPrefixIfNeeded(job, msg)
		if err := su.markTaskUnschedulable(taskInfo.Pod, msg, true); err != nil {
			errs = append(errs, fmt.Errorf("failed to update invalid subgroup task status <%s/%s>: %v",
				taskInfo.Namespace, taskInfo.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (su *defaultStatusUpdater) updatePodGroupAnnotations(job *podgroup_info.PodGroupInfo) ([]byte, error) {
	old := job.PodGroup.DeepCopy()
	updatedStaleTime := setPodGroupStaleTimeStamp(job.PodGroup, job.StalenessInfo.TimeStamp)
	updatedStartTime := setPodGroupLastStartTimeStamp(job.PodGroup, job.LastStartTimestamp)
	if !updatedStaleTime && !updatedStartTime {
		return nil, nil
	}

	patchData, err := getPodGroupPatch(old, job.PodGroup)
	if err != nil {
		return nil, err
	}

	if patchData == nil {
		return nil, nil
	}
	return patchData, nil
}

func (su *defaultStatusUpdater) recordUnschedulablePodGroup(job *podgroup_info.PodGroupInfo) bool {
	var msg string
	msg = common_info.JobFitErrorsToMessage(job.JobFitErrors)
	if su.detailedFitErrors {
		msg = common_info.JobFitErrorsToDetailedMessage(job.JobFitErrors)
	} else {
		log.InfraLogger.V(6).Infof("Full job fit error: %s", common_info.JobFitErrorsToDetailedMessage(job.JobFitErrors))
	}

	if len(msg) == 0 {
		msg = string(common_info.DefaultPodgroupError)
	}

	msg = su.addNodePoolPrefixIfNeeded(job, msg)
	return su.markPodGroupUnschedulable(job, msg)
}

func (su *defaultStatusUpdater) recordScenarioSearchUnresolvedPodGroup(job *podgroup_info.PodGroupInfo) bool {
	return su.markPodGroupScenarioSearchUnresolved(job, scenarioSearchUnresolvedMessage(job.ScenarioSearchUnresolved))
}

func scenarioSearchUnresolvedMessage(unresolved *podgroup_info.ScenarioSearchUnresolved) string {
	if unresolved != nil && unresolved.ReducedBudget {
		return "KAI could not find a valid scenario within the remaining configured search time for this scheduling attempt because the action search budget was partly consumed by earlier jobs. The job remains pending and may be retried in a later scheduling cycle."
	}
	if unresolved == nil {
		return ""
	}
	switch unresolved.Reason {
	case podgroup_info.ScenarioSearchResultDeadlineExhausted:
		return "KAI could not find a valid reclaim scenario within the configured search budget for this scheduling attempt. The job remains pending and may be retried in a later scheduling cycle."
	case podgroup_info.ScenarioSearchResultGeneratorsExhausted:
		return "KAI tried the configured scenario-search policy and found no valid reclaim scenario for this scheduling attempt. The job remains pending and may be retried in a later scheduling cycle."
	case podgroup_info.ScenarioSearchResultNotAttempted:
		return "KAI did not attempt scenario search for this job in this scheduling cycle because the configured search budget was already exhausted."
	case podgroup_info.ScenarioSearchResultNoGenerator:
		return "KAI did not attempt scenario search for this job because no configured scenario generator applies to this action."
	default:
		return "KAI tried the configured scenario-search policy and found no valid reclaim scenario for this scheduling attempt. The job remains pending and may be retried in a later scheduling cycle."
	}
}

func (su *defaultStatusUpdater) updatePodGroupSchedulingCondition(
	podGroup *enginev2alpha2.PodGroup, schedulingCondition *enginev2alpha2.SchedulingCondition,
) bool {
	log.InfraLogger.V(6).Infof(
		"Updating pod group scheduling condition for %s/%s to (%s,nodepool=%s)",
		podGroup.Namespace, podGroup.Name, schedulingCondition.Type, schedulingCondition.NodePool)
	return setPodGroupSchedulingCondition(podGroup, schedulingCondition)
}

func (su *defaultStatusUpdater) addNodePoolPrefixIfNeeded(job *podgroup_info.PodGroupInfo, msg string) string {
	schedulingBackoff := utils.GetSchedulingBackoffValue(job.PodGroup.Spec.SchedulingBackoff)
	if schedulingBackoff == utils.SingleSchedulingBackoff {
		messagePrefix := fmt.Sprintf("Node-Pool '%s': ",
			utils.GetNodePoolNameFromLabels(job.PodGroup.Labels, su.nodePoolLabelKey))
		msg = fmt.Sprintf("%s%s", messagePrefix, msg)
	}
	return msg
}

func setPodGroupStaleTimeStamp(podGroup *enginev2alpha2.PodGroup, staleTimeStamp *time.Time) bool {
	if podGroup.Annotations == nil {
		podGroup.Annotations = make(map[string]string)
	}

	if staleTimeStamp == nil {
		if _, found := podGroup.Annotations[commonconstants.StalePodgroupTimeStamp]; !found {
			return false
		}

		delete(podGroup.Annotations, commonconstants.StalePodgroupTimeStamp)
		return true
	}

	currTimeStamp, found := podGroup.Annotations[commonconstants.StalePodgroupTimeStamp]
	if !found {
		podGroup.Annotations[commonconstants.StalePodgroupTimeStamp] = staleTimeStamp.UTC().Format(time.RFC3339)
		return true
	}

	if currTimeStamp == staleTimeStamp.Format(time.RFC3339) {
		return false
	}

	podGroup.Annotations[commonconstants.StalePodgroupTimeStamp] = staleTimeStamp.Format(time.RFC3339)
	return true
}

func setPodGroupLastStartTimeStamp(podGroup *enginev2alpha2.PodGroup, startTimeStamp *time.Time) bool {
	if podGroup.Annotations == nil {
		podGroup.Annotations = make(map[string]string)
	}

	if startTimeStamp == nil {
		if _, found := podGroup.Annotations[commonconstants.LastStartTimeStamp]; !found {
			return false
		}

		delete(podGroup.Annotations, commonconstants.LastStartTimeStamp)
		return true
	}

	currTimeStamp, found := podGroup.Annotations[commonconstants.LastStartTimeStamp]
	if !found {
		podGroup.Annotations[commonconstants.LastStartTimeStamp] = startTimeStamp.UTC().Format(time.RFC3339)
		return true
	}

	if currTimeStamp == startTimeStamp.Format(time.RFC3339) {
		return false
	}

	podGroup.Annotations[commonconstants.LastStartTimeStamp] = startTimeStamp.Format(time.RFC3339)
	return true
}

func setPodGroupSchedulingCondition(podGroup *enginev2alpha2.PodGroup, schedulingCondition *enginev2alpha2.SchedulingCondition) bool {
	currentSchedulingConditionIndex := getSchedulingConditionIndex(podGroup, schedulingCondition)
	lastSchedulingCondition := utils.GetLastSchedulingCondition(podGroup)

	setTransitionID(podGroup, schedulingCondition, lastSchedulingCondition)

	if !shouldUpdateCondition(podGroup, schedulingCondition, lastSchedulingCondition, currentSchedulingConditionIndex) {
		return false
	}

	// BC: older versions of pod group assigner rely on the most recent condition to be the last in the list.
	// Squash conditions of the same node pool and type and append ours to the end.
	squashAndAppendSchedulingCondition(podGroup, schedulingCondition)
	return true
}

func getSchedulingConditionIndex(
	podGroup *enginev2alpha2.PodGroup, schedulingCondition *enginev2alpha2.SchedulingCondition,
) int {
	for i, condition := range podGroup.Status.SchedulingConditions {
		if condition.NodePool == schedulingCondition.NodePool && condition.Type == schedulingCondition.Type {
			return i
		}
	}

	return -1
}

func setTransitionID(podGroup *enginev2alpha2.PodGroup, schedulingCondition, lastSchedulingCondition *enginev2alpha2.SchedulingCondition) {
	var lastTransitionID uint32 = 0
	if lastSchedulingCondition != nil {
		id, err := strconv.Atoi(lastSchedulingCondition.TransitionID)
		if err != nil || id < 0 {
			log.InfraLogger.Errorf(
				"Failed to parse transition ID for podgroup %s/%s, treating as 0. ID: %s, error: %v",
				podGroup.Namespace, podGroup.Name, lastSchedulingCondition.TransitionID, err)
			id = 0
		}
		lastTransitionID = uint32(id)
	}
	schedulingCondition.TransitionID = fmt.Sprintf("%d", lastTransitionID+1)
}

func shouldUpdateCondition(
	podGroup *enginev2alpha2.PodGroup,
	schedulingCondition, lastSchedulingCondition *enginev2alpha2.SchedulingCondition,
	currentSchedulingConditionIndex int) bool {
	// If the last scheduling condition is the same as the current one, we don't need to update the status.
	if !equalSchedulingConditions(lastSchedulingCondition, schedulingCondition) {
		return true
	}
	// BC: older versions of pod group assigner rely on the most recent condition to be the last in the list.
	// Only if ours is the last we can return false and not update the podgroup.
	return currentSchedulingConditionIndex != len(podGroup.Status.SchedulingConditions)-1
}

func equalSchedulingConditions(a, b *enginev2alpha2.SchedulingCondition) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Type == b.Type &&
		a.NodePool == b.NodePool &&
		a.Reason == b.Reason &&
		a.Message == b.Message &&
		a.Status == b.Status
}

func squashAndAppendSchedulingCondition(podGroup *enginev2alpha2.PodGroup, schedulingCondition *enginev2alpha2.SchedulingCondition) {
	var squashedConditions []enginev2alpha2.SchedulingCondition
	for _, condition := range podGroup.Status.SchedulingConditions {
		if condition.NodePool != schedulingCondition.NodePool || condition.Type != schedulingCondition.Type {
			squashedConditions = append(squashedConditions, condition)
		}
	}
	schedulingCondition.LastTransitionTime = metav1.Now()
	squashedConditions = append(squashedConditions, *schedulingCondition)
	podGroup.Status.SchedulingConditions = squashedConditions
}

func getPodGroupPatch(old *enginev2alpha2.PodGroup, new *enginev2alpha2.PodGroup) ([]byte, error) {
	origJSON, err := json.Marshal(old)
	if err != nil {
		return nil, err
	}

	mutatedJSON, err := json.Marshal(new)
	if err != nil {
		return nil, err
	}

	patches, err := jsonpatch.CreatePatch(origJSON, mutatedJSON)
	if err != nil {
		return nil, err
	}

	if len(patches) == 0 {
		return nil, nil
	}

	return json.Marshal(patches)
}
