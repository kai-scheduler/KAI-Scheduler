// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"context"
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	v1 "k8s.io/api/core/v1"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/resource_info"
)

type MaxNodeResourcesPredicate struct {
	maxResources       *resource_info.Resource
	schedulerShardName string
}

func NewMaxNodeResourcesPredicate(nodesMap map[string]*node_info.NodeInfo, nodePoolName string) *MaxNodeResourcesPredicate {
	predicate := &MaxNodeResourcesPredicate{
		maxResources:       resource_info.EmptyResource(),
		schedulerShardName: nodePoolName,
	}

	for _, node := range nodesMap {
		predicate.maxResources.SetMaxResource(node.Allocatable)
	}
	if nodePoolName == "" {
		predicate.schedulerShardName = "default"
	}

	return predicate
}

func (_ *MaxNodeResourcesPredicate) isPreFilterRequired(_ *v1.Pod) bool {
	return true
}

func (_ *MaxNodeResourcesPredicate) isFilterRequired(_ *v1.Pod) bool {
	return false
}

func (mnr *MaxNodeResourcesPredicate) PreFilter(_ context.Context, _ ksf.CycleState, pod *v1.Pod, _ []ksf.NodeInfo) (
	*k8sframework.PreFilterResult, *ksf.Status) {

	podInfo := pod_info.NewTaskInfo(pod)

	if podInfo.ResReq.GPUs() > mnr.maxResources.GPUs() {
		return nil, ksf.NewStatus(ksf.Unschedulable,
			mnr.buildUnschedulableMessage(podInfo, "GPU", mnr.maxResources.GPUs(), ""))
	}
	if podInfo.ResReq.Cpu() > mnr.maxResources.Cpu() {
		return nil, ksf.NewStatus(ksf.Unschedulable,
			mnr.buildUnschedulableMessage(podInfo, "CPU",
				mnr.maxResources.Cpu()/resource_info.MilliCPUToCores, "cores"))
	}
	if podInfo.ResReq.Memory() > mnr.maxResources.Memory() {
		return nil, ksf.NewStatus(ksf.Unschedulable,
			mnr.buildUnschedulableMessage(podInfo, "memory",
				mnr.maxResources.Memory()/resource_info.MemoryToGB, "GB"))
	}
	for rName, rQuant := range podInfo.ResReq.ScalarResources() {
		rrQuant, found := mnr.maxResources.ScalarResources()[rName]
		if !found || rQuant > rrQuant {
			return nil, ksf.NewStatus(ksf.Unschedulable,
				mnr.buildUnschedulableMessage(podInfo, string(rName), float64(rrQuant), ""))
		}
	}

	return nil, nil
}

func (mnr *MaxNodeResourcesPredicate) buildUnschedulableMessage(podInfo *pod_info.PodInfo, resourcesName string,
	resourceQuantity float64, resourceUnits string) string {
	messageBuilder := strings.Builder{}

	messageBuilder.WriteString(fmt.Sprintf("The pod %s/%s requires %s. ", podInfo.Namespace, podInfo.Name,
		podInfo.ResReq.DetailedString()))
	if resourceQuantity == 0 {
		messageBuilder.WriteString(fmt.Sprintf("No node in the %s node-pool has %s resources",
			mnr.schedulerShardName, resourcesName))
	} else {
		message := fmt.Sprintf("Max %s resources available in a single node in the %s node-pool is topped at %s",
			resourcesName,
			mnr.schedulerShardName,
			humanize.FtoaWithDigits(resourceQuantity, 3),
		)
		if resourceUnits != "" {
			message += fmt.Sprintf(" %s", resourceUnits)
		}
		messageBuilder.WriteString(message)
	}

	return messageBuilder.String()
}
