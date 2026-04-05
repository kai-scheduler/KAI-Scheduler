// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"context"
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	resourceapi "k8s.io/api/resource/v1"
)

type MaxNodeResourcesPredicate struct {
	maxResources       resource_info.ResourceVector
	vectorMap          *resource_info.ResourceVectorMap
	resourceClaimsMap  map[string]*resourceapi.ResourceClaim
	podsToClaimsMap    map[types.UID]map[types.UID]*resourceapi.ResourceClaim
	schedulerShardName string
}

func NewMaxNodeResourcesPredicate(nodesMap map[string]*node_info.NodeInfo, resourceClaims []*resourceapi.ResourceClaim, nodePoolName string) *MaxNodeResourcesPredicate {
	resourceClaimsMap := resource_info.ResourceClaimSliceToMap(resourceClaims)
	podsToClaimsMap := resource_info.CalcClaimsToPodsBaseMap(resourceClaimsMap)

	predicate := &MaxNodeResourcesPredicate{
		resourceClaimsMap:  resourceClaimsMap,
		podsToClaimsMap:    podsToClaimsMap,
		schedulerShardName: nodePoolName,
	}

	for _, node := range nodesMap {
		if predicate.vectorMap == nil {
			predicate.vectorMap = node.VectorMap
			predicate.maxResources = node.AllocatableVector.Clone()
		} else {
			predicate.maxResources.SetMax(node.AllocatableVector)
		}
	}
	if predicate.vectorMap == nil {
		predicate.vectorMap = resource_info.NewResourceVectorMap()
		predicate.maxResources = resource_info.NewResourceVector(predicate.vectorMap)
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

	draPodClaims := resource_info.GetDraPodClaims(pod, mnr.resourceClaimsMap, mnr.podsToClaimsMap)
	podInfo := pod_info.NewTaskInfo(pod, draPodClaims, mnr.vectorMap)
	gpuIdx := mnr.vectorMap.GetIndex("gpu")
	cpuIdx := mnr.vectorMap.GetIndex(v1.ResourceCPU)
	memIdx := mnr.vectorMap.GetIndex(v1.ResourceMemory)

	if podInfo.ResReqVector.Get(gpuIdx) > mnr.maxResources.Get(gpuIdx) {
		return nil, ksf.NewStatus(ksf.Unschedulable,
			mnr.buildUnschedulableMessage(podInfo, "GPU", mnr.maxResources.Get(gpuIdx), ""))
	}
	if podInfo.ResReqVector.Get(cpuIdx) > mnr.maxResources.Get(cpuIdx) {
		return nil, ksf.NewStatus(ksf.Unschedulable,
			mnr.buildUnschedulableMessage(podInfo, "CPU",
				mnr.maxResources.Get(cpuIdx)/resource_info.MilliCPUToCores, "cores"))
	}
	if podInfo.ResReqVector.Get(memIdx) > mnr.maxResources.Get(memIdx) {
		return nil, ksf.NewStatus(ksf.Unschedulable,
			mnr.buildUnschedulableMessage(podInfo, "memory",
				mnr.maxResources.Get(memIdx)/resource_info.MemoryToGB, "GB"))
	}
	for i := range mnr.vectorMap.Len() {
		if i == cpuIdx || i == memIdx || i == gpuIdx {
			continue
		}
		podVal := podInfo.ResReqVector.Get(i)
		maxVal := mnr.maxResources.Get(i)
		if podVal > 0 && maxVal < podVal {
			rName := mnr.vectorMap.ResourceAt(i)
			units := ""
			displayMax := float64(0)
			if rName == v1.ResourceEphemeralStorage || rName == v1.ResourceStorage {
				units = "GB"
				displayMax = maxVal / resource_info.MemoryToGB
			}
			return nil, ksf.NewStatus(ksf.Unschedulable,
				mnr.buildUnschedulableMessage(podInfo, string(rName), displayMax, units))
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
