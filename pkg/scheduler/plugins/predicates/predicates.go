// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	ksf "k8s.io/kube-scheduler/framework"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal/predicates"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/scheduler_util"
)

const (
	predicatePluginName       = "predicates"
	prePredicateErrorFormat   = "%s: %v.%s\n"
	prePredicateReasonsFormat = " Reasons: %s"
)

type prePredicateError struct {
	name    string
	err     error
	reasons []string
}

func newPrePredicateError(name string, Status ksf.Status) prePredicateError {
	err := Status.AsError()
	reasons := Status.Reasons()
	if len(reasons) > 0 {
		if reasons[0] == err.Error() {
			reasons = reasons[1:]
		}
	}

	return prePredicateError{
		name:    name,
		err:     err,
		reasons: reasons,
	}
}

type SkipPredicates map[common_info.PodID]map[k8s_internal.PredicateName]bool

func (sp SkipPredicates) Add(podID common_info.PodID, predicateName k8s_internal.PredicateName) {
	if _, found := sp[podID]; !found {
		sp[podID] = map[k8s_internal.PredicateName]bool{}
	}
	sp[podID][predicateName] = true
}

func (sp SkipPredicates) ShouldSKip(podID common_info.PodID, predicateName k8s_internal.PredicateName) bool {
	if _, found := sp[podID]; !found {
		return false
	}
	skip, found := sp[podID][predicateName]
	return skip && found
}

type predicatesPlugin struct {
	// Arguments given for the plugin
	pluginArguments          map[string]string
	storageSchedulingEnabled bool

	skipPredicates SkipPredicates
}

func New(arguments map[string]string) framework.Plugin {
	return &predicatesPlugin{
		pluginArguments: arguments,
	}
}

func (pp *predicatesPlugin) Name() string {
	return predicatePluginName
}

func (pp *predicatesPlugin) OnSessionOpen(ssn *framework.Session) {
	k8sPredicates := predicates.NewSessionPredicates(ssn)

	pp.storageSchedulingEnabled = ssn.ScheduleCSIStorage()
	pp.skipPredicates = SkipPredicates{}

	ssn.AddPrePredicateFn(func(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo) error {
		return evaluateTaskOnPrePredicate(task, k8sPredicates, pp.skipPredicates)
	})

	ssn.AddPredicateFn(func(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
		return pp.evaluateTaskOnPredicates(task, job, node, k8sPredicates,
			ssn.IsTaskAllocationOnNodeOverCapacityFn, ssn.IsRestrictNodeSchedulingEnabled, pp.skipPredicates)
	})
}

func evaluateTaskOnPrePredicate(task *pod_info.PodInfo, k8sPredicates k8s_internal.SessionPredicates,
	skipPredicates SkipPredicates,
) error {
	var allErrors []prePredicateError
	var allowedNodes sets.Set[string] = nil
	for name, predicate := range k8sPredicates {
		if !predicate.IsPreFilterRequired(task.Pod) {
			continue
		}
		nodes, status := predicate.PreFilter(task.Pod)
		if status.IsSkip() {
			skipPredicates.Add(task.UID, name)
		}

		if status.AsError() != nil {
			allErrors = append(allErrors, newPrePredicateError(string(name), *status))
		} else {
			if allowedNodes == nil {
				allowedNodes = nodes
			}
			allowedNodes.Intersection(nodes)
		}
	}

	if len(allErrors) > 0 {
		fitErrors := common_info.NewFitErrors()
		fitErrors.SetError(fmt.Sprintf("Scheduling conditions were not met for pod %s/%s:\n%v",
			task.Namespace, task.Name, generateErrorLog(allErrors)))
		return fitErrors
	}

	return nil
}

func generateErrorLog(allErrors []prePredicateError) string {
	errorsLog := ""
	for _, prePredicateError := range allErrors {
		errorReasons := prePredicateError.reasons
		errorObj := prePredicateError.err
		errorReasonStr := ""

		hasMoreThanOneReason := len(errorReasons) > 1
		if hasMoreThanOneReason || len(errorReasons) == 1 && errorReasons[0] != errorObj.Error() {
			errorReasonStr = fmt.Sprintf(prePredicateReasonsFormat, strings.Join(errorReasons, ", "))
		}
		errorsLog += fmt.Sprintf(prePredicateErrorFormat, prePredicateError.name, errorObj, errorReasonStr)
	}
	return errorsLog
}

func (pp *predicatesPlugin) evaluateTaskOnPredicates(
	task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo,
	k8sPredicates k8s_internal.SessionPredicates,
	isTaskAllocationOnNodeOverCapacityFn api.IsTaskAllocationOverCapacityFn,
	isRestrictNodeSchedulingEnabled func() bool,
	skipPredicates SkipPredicates,
) error {
	originalPodInfoNodeName := task.NodeName
	originalPodNodeName := task.Pod.Spec.NodeName
	task.NodeName = node.Name
	task.Pod.Spec.NodeName = node.Name

	defer func() {
		task.Pod.Spec.NodeName = originalPodNodeName
		task.NodeName = originalPodInfoNodeName
	}()

	k8sNodeInfo := node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo
	k8sNodeInfo.SetNode(node.Node)

	if result := isTaskAllocationOnNodeOverCapacityFn(task, job, node); !result.IsSchedulable {
		return common_info.NewFitError(task.Name, task.Namespace, node.Name,
			fmt.Sprintf("node does not have enough capacity. Reason: %s, Details: %s",
				result.Reason, result.Message))
	}

	fitError := node.PredicateByNodeResourcesType(task)
	if fitError != nil {
		return fitError
	}

	if task.ResReq.GpuMemory() > 0 && !node.GpuMemorySynced {
		return common_info.NewFitError(task.Name, task.Namespace, node.Name,
			fmt.Sprintf("node is not a gpu node or the gpu memory count on the node was not synced yet,"+
				" task: <%v/%v>, node: <%v>", task.Namespace, task.Name, node.Name))
	}

	podsCountForTask := 1
	if task.IsSharedGPURequest() {
		podsCountForTask += 1 // we need to include a *potential* reservation pod for fraction
	}
	if len(k8sNodeInfo.Pods)+podsCountForTask > node.MaxTaskNum {
		log.InfraLogger.V(6).Infof("NodePodNumber predicates Task <%s/%s> on Node <%s> failed",
			task.Namespace, task.Name, node.Name)
		return common_info.NewFitError(task.Name, task.Namespace, node.Name, api.NodePodNumberExceeded)
	}

	fit, reasons, err := scheduler_util.CheckNodeConditionPredicate(node.Node)
	log.InfraLogger.V(6).Infof("Check node condition predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
		task.Namespace, task.Name, node.Name, fit, err)

	if !fit {
		return common_info.NewFitErrorByReasons(task.Name, task.Namespace, node.Name, err, reasons...)
	}

	for name, predicate := range k8sPredicates {
		if !predicate.IsFilterRequired(task.Pod) {
			log.InfraLogger.V(6).Infof("Predicate %s not required for pod %s/%s", name, task.Namespace, task.Name)
			continue
		}

		if skipPredicates.ShouldSKip(task.UID, name) {
			log.InfraLogger.V(6).Infof("Skipping predicate %s for pod %s/%s", name, task.Namespace, task.Name)
			continue
		}

		fit, reasons, err := predicate.Filter(task.Pod, k8sNodeInfo)
		if err != nil {
			return err
		}

		if !fit {
			return common_info.NewFitErrorByReasons(task.Name, task.Namespace, node.Name, err, reasons...)
		}
	}

	if isRestrictNodeSchedulingEnabled() {
		gpuWorkerLabelKey := conf.GetConfig().GPUWorkerNodeLabelKey
		cpuWorkerLabelKey := conf.GetConfig().CPUWorkerNodeLabelKey
		if task.IsRequireAnyKindOfGPU() {
			if _, found := node.Node.Labels[gpuWorkerLabelKey]; !found {
				log.InfraLogger.V(6).Infof("Task <%s/%s> is a GPU job and will not be allocated to a non GPU <%s>",
					task.Namespace, task.Name, node.Name)
				return fmt.Errorf("gpu task: <%v/%v> can't run on non gpu nodes, node: <%v>", task.Namespace, task.Name, node.Name)
			}
		} else {
			if _, found := node.Node.Labels[cpuWorkerLabelKey]; !found {
				log.InfraLogger.V(6).Infof("Task <%s/%s> is a CPU job and will not be allocated to a GPU node <%s>",
					task.Namespace, task.Name, node.Name)
				return fmt.Errorf("cpu task: <%v/%v> can't run on non cpu nodes, node: <%v>", task.Namespace, task.Name, node.Name)
			}
		}
	}

	return nil
}

func (pp *predicatesPlugin) OnSessionClose(_ *framework.Session) {}
