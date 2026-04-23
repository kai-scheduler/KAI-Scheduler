/*
Copyright 2019 The Kubernetes Authors.

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

package pod_info

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	clientcache "k8s.io/client-go/tools/cache"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	GpuMemoryAnnotationName            = "gpu-memory"
	GPUGroup                           = "runai-gpu-group"
	ReceivedResourceTypeAnnotationName = "received-resource-type"
	WholeGpuIndicator                  = "-2"
)

type ResourceRequestType string

const (
	RequestTypeGpuMemory   ResourceRequestType = "GpuMemory"
	RequestTypeMigInstance ResourceRequestType = "MigInstance"
	RequestTypeFraction    ResourceRequestType = "Fraction"
	RequestTypeRegular     ResourceRequestType = "Regular"
)

type ResourceReceivedType string

const (
	ReceivedTypeMigInstance ResourceReceivedType = "MigInstance"
	ReceivedTypeFraction    ResourceReceivedType = "Fraction"
	ReceivedTypeRegular     ResourceReceivedType = "Regular"
	ReceivedTypeNone        ResourceReceivedType = ""
)

type PodsMap map[common_info.PodID]*PodInfo

type PodInfo struct {
	UID common_info.PodID
	Job common_info.PodGroupID

	Name      string
	Namespace string

	SubGroupName string

	ResourceRequestType  ResourceRequestType
	ResourceReceivedType ResourceReceivedType

	// Vector representation of resource requirements
	ResReqVector           resource_info.ResourceVector
	AcceptedResourceVector resource_info.ResourceVector
	VectorMap              *resource_info.ResourceVectorMap

	// GPU-specific resource requirements (portion, count, gpu memory, MIG resources)
	GpuRequirement         resource_info.GpuResourceRequirement
	AcceptedGpuRequirement resource_info.GpuResourceRequirement

	schedulingConstraintsSignature common_info.SchedulingConstraintsSignature

	GPUGroups []string

	NodeName        string
	Status          pod_status.PodStatus
	IsVirtualStatus bool
	IsLegacyMIGtask bool

	BindRequest *bindrequest_info.BindRequestInfo

	ResourceClaimInfo bindrequest_info.ResourceClaimInfo

	// OwnedStorageClaims are StorageClaims that are owned exclusively by the pod, and we can count on them being deleted
	// if the pod is evicted
	ownedStorageClaims map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo

	// storageClaims are all storage claims used by the pod, with any status and any ownership situation. Not mutually exclusive
	// with ownedStorageClaims
	storageClaims map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo

	Pod *v1.Pod
}

func (pi *PodInfo) GetAllStorageClaims() map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo {
	return pi.storageClaims
}

func (pi *PodInfo) GetDeletedStorageClaimsNames() string {
	var deletedClaims []*storageclaim_info.StorageClaimInfo
	for _, claim := range pi.storageClaims {
		if claim.HasDeletedOwner() {
			deletedClaims = append(deletedClaims, claim)
		}
	}

	result := make([]string, len(deletedClaims))
	for _, claim := range deletedClaims {
		result = append(result, string(claim.Key))
	}

	return strings.Join(result, ", ")
}

func (pi *PodInfo) GetUnboundOrReleasingStorageClaimsByStorageClass() map[common_info.StorageClassID][]*storageclaim_info.StorageClaimInfo {
	result := map[common_info.StorageClassID][]*storageclaim_info.StorageClaimInfo{}

	for _, claim := range pi.GetAllStorageClaims() {
		if claim.Phase != v1.ClaimPending {
			continue
		}
		result[claim.StorageClass] = append(result[claim.StorageClass], claim)
	}

	// If a pod is pending virtually - it means that it was just evicted virtually. So all the owned storage claims are about
	// to be deleted - which means we need to consider them as pending.
	if pi.IsVirtualStatus && !pod_status.AllocatedStatus(pi.Status) {
		for _, claim := range pi.GetOwnedStorageClaims() {
			result[claim.StorageClass] = append(result[claim.StorageClass], claim)
		}
	}

	return result
}

func (pi *PodInfo) GetOwnedStorageClaims() map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo {
	return pi.ownedStorageClaims
}

func (pi *PodInfo) UpsertStorageClaim(claimInfo *storageclaim_info.StorageClaimInfo) {
	if claimInfo.PodOwnerReference != nil && claimInfo.PodOwnerReference.PodID == pi.UID {
		pi.ownedStorageClaims[claimInfo.Key] = claimInfo

		if pi.Pod != nil && pi.Pod.DeletionTimestamp == nil {
			claimInfo.MarkOwnerAlive()
		}
	}
	pi.storageClaims[claimInfo.Key] = claimInfo
}

func NewTaskInfo(pod *v1.Pod, draPodClaims []*resourceapi.ResourceClaim, vectorMap *resource_info.ResourceVectorMap) *PodInfo {
	return NewTaskInfoWithBindRequest(pod, nil, draPodClaims, vectorMap)
}

func NewTaskInfoWithBindRequest(pod *v1.Pod, bindRequest *bindrequest_info.BindRequestInfo, draPodClaims []*resourceapi.ResourceClaim, vectorMap *resource_info.ResourceVectorMap) *PodInfo {
	initResreq := getPodResourceRequest(pod)

	nodeName := pod.Spec.NodeName
	if nodeName == "" && bindRequest != nil {
		nodeName = bindRequest.BindRequest.Spec.SelectedNode
	}

	resourceClaimInfo, err := resourceClaimInfoFromPodClaims(draPodClaims, pod, bindRequest)
	if err != nil {
		log.InfraLogger.Errorf("PodInfo ctor failure - failed to calculate resource claim info for pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	podInfo := &PodInfo{
		UID:                            common_info.PodID(pod.UID),
		Job:                            getPodGroupID(pod),
		Name:                           pod.Name,
		Namespace:                      pod.Namespace,
		SubGroupName:                   pod.Labels[commonconstants.SubGroupLabelKey],
		NodeName:                       nodeName,
		Status:                         getTaskStatus(pod, bindRequest),
		IsVirtualStatus:                false,
		IsLegacyMIGtask:                false,
		Pod:                            pod,
		GpuRequirement:                 initResreq.GpuResourceRequirement,
		AcceptedGpuRequirement:         *resource_info.NewGpuResourceRequirement(),
		ResReqVector:                   initResreq.ToVector(vectorMap),
		AcceptedResourceVector:         resource_info.NewResourceVector(vectorMap),
		VectorMap:                      vectorMap,
		GPUGroups:                      []string{},
		ResourceRequestType:            RequestTypeRegular,
		ResourceReceivedType:           ReceivedTypeNone,
		BindRequest:                    bindRequest,
		ResourceClaimInfo:              resourceClaimInfo,
		schedulingConstraintsSignature: "",
		storageClaims:                  map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{},
		ownedStorageClaims:             map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{},
	}

	podInfo.updatePodAdditionalFields(bindRequest, draPodClaims...)

	return podInfo
}

func resourceClaimInfoFromPodClaims(draPodClaims []*resourceapi.ResourceClaim, pod *v1.Pod, bindRequest *bindrequest_info.BindRequestInfo) (bindrequest_info.ResourceClaimInfo, error) {
	resourceClaimInfo := make(bindrequest_info.ResourceClaimInfo)

	bindingRequestClaimUpdates := make(map[string]*schedulingv1alpha2.ResourceClaimAllocation)
	if bindRequest != nil {
		for _, claimAllocation := range bindRequest.BindRequest.Spec.ResourceClaimAllocations {
			bindingRequestClaimUpdates[claimAllocation.Name] = &schedulingv1alpha2.ResourceClaimAllocation{
				Name:       claimAllocation.Name,
				Allocation: claimAllocation.Allocation.DeepCopy(),
			}
		}
	}

	draPodClaimsMap := resource_info.ResourceClaimSliceToMap(draPodClaims)
	for _, podClaim := range pod.Spec.ResourceClaims {
		claimName, err := resources.GetResourceClaimName(pod, &podClaim)
		if err != nil {
			if podClaim.ResourceClaimTemplateName != nil {
				continue // The dra controller might not have created the claim yet - this is a valid state. The will fail on the dra plugin.
			}
			return resourceClaimInfo, fmt.Errorf("PodInfo ctor failure - failed to get resource claim name for pod %s/%s, claim %s: %v", pod.Namespace, pod.Name, podClaim.Name, err)
		}
		claim, found := draPodClaimsMap[types.NamespacedName{Namespace: pod.Namespace, Name: claimName}.String()]
		if !found || claim == nil {
			if podClaim.ResourceClaimTemplateName != nil {
				continue // The dra controller might not have created the claim yet - this is a valid state. The will fail on the dra plugin.
			}
			return resourceClaimInfo, fmt.Errorf("PodInfo ctor failure - failed to get claim from draPodClaimsMap for pod %s/%s, claim %s", pod.Namespace, pod.Name, podClaim.Name)
		}
		resourceClaimInfo[podClaim.Name] = &schedulingv1alpha2.ResourceClaimAllocation{
			Name:       podClaim.Name,
			Allocation: claim.Status.Allocation,
		}

		// If a binding claim already exists, assume this is the allocation that will happen for the pod
		if claimUpdate, found := bindingRequestClaimUpdates[podClaim.Name]; found {
			resourceClaimInfo[podClaim.Name].Allocation = claimUpdate.Allocation
		}
	}
	return resourceClaimInfo, nil
}

func (pi *PodInfo) SetVectorMap(vectorMap *resource_info.ResourceVectorMap) {
	pi.VectorMap = vectorMap
	// Rebuild vectors from pod spec - ResReqVector was already computed from the pod resource request
	// AcceptedResourceVector stays as-is since it's set by setAcceptedResources
}

func (pi *PodInfo) Clone() *PodInfo {
	var resReqVectorClone resource_info.ResourceVector
	if pi.ResReqVector != nil {
		resReqVectorClone = pi.ResReqVector.Clone()
	}
	var acceptedResourceVectorClone resource_info.ResourceVector
	if pi.AcceptedResourceVector != nil {
		acceptedResourceVectorClone = pi.AcceptedResourceVector.Clone()
	}

	return &PodInfo{
		UID:                    pi.UID,
		Job:                    pi.Job,
		Name:                   pi.Name,
		Namespace:              pi.Namespace,
		SubGroupName:           pi.SubGroupName,
		NodeName:               pi.NodeName,
		Status:                 pi.Status,
		Pod:                    pi.Pod,
		GpuRequirement:         *pi.GpuRequirement.Clone(),
		AcceptedGpuRequirement: *pi.AcceptedGpuRequirement.Clone(),
		ResReqVector:           resReqVectorClone,
		AcceptedResourceVector: acceptedResourceVectorClone,
		VectorMap:              pi.VectorMap,
		GPUGroups:              pi.GPUGroups,
		ResourceClaimInfo:      pi.ResourceClaimInfo.Clone(),
		ResourceRequestType:    pi.ResourceRequestType,
		ResourceReceivedType:   pi.ResourceReceivedType,
		IsVirtualStatus:        pi.IsVirtualStatus,
		IsLegacyMIGtask:        pi.IsLegacyMIGtask,
		storageClaims:          pi.storageClaims,
		ownedStorageClaims:     pi.ownedStorageClaims,
	}
}

func (pi PodInfo) String() string {
	return fmt.Sprintf("Pod (%v:%v/%v): job %v, status %v, resreq %v, gpu %v",
		pi.UID, pi.Namespace, pi.Name, pi.Job, pi.Status, pi.ResReqVector, pi.GpuRequirement.GpusAsString())
}

func (pi *PodInfo) IsMigProfileRequest() bool {
	return pi.ResourceRequestType == RequestTypeMigInstance
}

func (pi *PodInfo) IsMigProfileAllocation() bool {
	return pi.ResourceReceivedType == ReceivedTypeMigInstance
}

func (pi *PodInfo) IsFractionRequest() bool {
	return pi.ResourceRequestType == RequestTypeFraction
}

func (pi *PodInfo) IsFractionAllocation() bool {
	return pi.ResourceReceivedType == ReceivedTypeFraction
}

func (pi *PodInfo) IsFractionCandidate() bool {
	return pi.ResourceRequestType == RequestTypeFraction || pi.ResourceRequestType == RequestTypeGpuMemory
}

func (pi *PodInfo) IsMigCandidate() bool {
	return pi.ResourceRequestType == RequestTypeMigInstance
}

func (pi *PodInfo) IsMemoryRequest() bool {
	return pi.ResourceRequestType == RequestTypeGpuMemory
}

func (pi *PodInfo) IsRegularGPURequest() bool {
	return pi.ResourceRequestType == RequestTypeRegular
}

func (pi *PodInfo) IsSharedGPURequest() bool {
	return pi.IsFractionRequest() || pi.IsMemoryRequest()
}

func (pi *PodInfo) IsSharedGPUAllocation() bool {
	return pi.IsFractionAllocation()
}

func (pi *PodInfo) IsCPUOnlyRequest() bool {
	return !pi.IsRequireAnyKindOfGPU()
}

func (pi *PodInfo) IsRequireAnyKindOfGPU() bool {
	return pi.GpuRequirement.GPUs() > 0 || pi.GpuRequirement.GetDraGpusCount() > 0 ||
		pi.IsMemoryRequest() || pi.IsMigProfileRequest()
}

func (pi *PodInfo) GetSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	if pi.schedulingConstraintsSignature == "" {
		pi.schedulingConstraintsSignature = schedulingConstraintsSignature(pi.Pod, pi.storageClaims)
	}
	return pi.schedulingConstraintsSignature
}

// PodKey returns the string key of a pod.
func PodKey(pod *v1.Pod) common_info.PodID {
	if key, err := clientcache.MetaNamespaceKeyFunc(pod); err != nil {
		return common_info.PodID(fmt.Sprintf("%v/%v", pod.Namespace, pod.Name))
	} else {
		return common_info.PodID(key)
	}
}

func getPodGroupID(pod *v1.Pod) common_info.PodGroupID {
	if gn, found := pod.Annotations[commonconstants.PodGroupAnnotationForPod]; found && len(gn) != 0 {
		return common_info.PodGroupID(gn)
	}

	return ""
}

func getPodResourceRequest(pod *v1.Pod) *resource_info.ResourceRequirements {
	result := getPodResourceWithoutInitContainers(pod)

	// take max_resource(sum_pod, any_init_container)
	for _, container := range pod.Spec.InitContainers {
		err := result.SetMaxResource(resource_info.RequirementsFromResourceList(container.Resources.Requests))
		if err != nil {
			log.InfraLogger.Errorf("Failed to calculate pod required resources for pod %s/%s. Error: %s",
				pod.Namespace, pod.Name, err.Error())
		}
	}

	if pod.Spec.Overhead != nil {
		overheadReq := resource_info.RequirementsFromResourceList(pod.Spec.Overhead)
		result.Add(&overheadReq.BaseResource)
	}

	result.ScalarResources()[resource_info.PodsResourceName] = 1

	return result
}

// getPodResourceWithoutInitContainers returns Pod's resource request, it does not contain
// init containers' resource request.
func getPodResourceWithoutInitContainers(pod *v1.Pod) *resource_info.ResourceRequirements {
	podResourcesList := v1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		for key := range container.Resources.Requests {
			resourceSum := podResourcesList[key]
			resourceSum.Add(container.Resources.Requests[key])
			podResourcesList[key] = resourceSum
		}
	}

	return resource_info.RequirementsFromResourceList(podResourcesList)
}

func getTaskStatus(pod *v1.Pod, bindRequest *bindrequest_info.BindRequestInfo) pod_status.PodStatus {
	switch pod.Status.Phase {
	case v1.PodRunning:
		if pod.DeletionTimestamp != nil {
			return pod_status.Releasing
		}

		return pod_status.Running
	case v1.PodPending:
		if pod.DeletionTimestamp != nil {
			return pod_status.Releasing
		}

		if len(pod.Spec.NodeName) != 0 {
			return pod_status.Bound
		}

		if bindRequest != nil {
			return pod_status.Binding
		}

		if len(pod.Spec.SchedulingGates) > 0 {
			return pod_status.Gated
		}

		return pod_status.Pending
	case v1.PodUnknown:
		return pod_status.Unknown
	case v1.PodSucceeded:
		return pod_status.Succeeded
	case v1.PodFailed:
		return pod_status.Failed
	}

	return pod_status.Unknown
}

func (pi *PodInfo) updatePodAdditionalFields(bindRequest *bindrequest_info.BindRequestInfo, draPodClaims ...*resourceapi.ResourceClaim) {
	if bindRequest != nil && len(bindRequest.BindRequest.Spec.SelectedGPUGroups) > 0 {
		pi.GPUGroups = bindRequest.BindRequest.Spec.SelectedGPUGroups
	} else {
		pi.GPUGroups = resources.GetGpuGroups(pi.Pod)
	}

	if bindRequest != nil && len(bindRequest.BindRequest.Spec.ReceivedResourceType) > 0 {
		pi.ResourceReceivedType = ResourceReceivedType(bindRequest.BindRequest.Spec.ReceivedResourceType)
	} else {
		resourceReceivedType, found := pi.Pod.Annotations[ReceivedResourceTypeAnnotationName]
		if found && resourceReceivedType != string(ReceivedTypeNone) {
			pi.ResourceReceivedType = ResourceReceivedType(resourceReceivedType)
		}
	}

	gpuMemory, err := strconv.ParseInt(pi.Pod.Annotations[GpuMemoryAnnotationName], 10, 64)
	if err == nil && gpuMemory > 0 {
		pi.GpuRequirement = *resource_info.NewGpuResourceRequirementWithGpus(0, gpuMemory)
		pi.ResourceRequestType = RequestTypeGpuMemory
	}

	gpuFractionString := pi.Pod.Annotations[common_info.GPUFraction]
	gpuFraction, GPUFractionErr := strconv.ParseFloat(gpuFractionString, 64)
	if !(gpuFraction <= 0 || gpuFraction > 1 || GPUFractionErr != nil) {
		pi.GpuRequirement = *resource_info.NewGpuResourceRequirementWithGpus(gpuFraction, 0)
		pi.ResourceRequestType = RequestTypeFraction
	}

	if pi.ResourceRequestType == RequestTypeFraction || pi.ResourceRequestType == RequestTypeGpuMemory {
		numFractionDevicesStr, found := pi.Pod.Annotations[commonconstants.GpuFractionsNumDevices]
		if found && numFractionDevicesStr != "" {
			numFractionDevices, numFractionDevicesErr := strconv.ParseInt(numFractionDevicesStr, 10, 64)
			if numFractionDevicesErr == nil {
				pi.GpuRequirement = *resource_info.NewGpuResourceRequirementWithMultiFraction(
					numFractionDevices, gpuFraction, gpuMemory)
			}
		}
	}

	if len(draPodClaims) > 0 {
		draGpus := resources.ExtractDRAGPUResourcesFromClaims(draPodClaims)
		pi.GpuRequirement.SetDraGpus(draGpus)
	}

	pi.updateLegacyMigResourceRequestFromAnnotations()
	if len(pi.GpuRequirement.MigResources()) > 0 {
		pi.ResourceRequestType = RequestTypeMigInstance
	}
	pi.rebuildResReqVector()
}

// updateLegacyMigResourceRequestFromAnnotations updates the mig resource request of legacy MIG pods
func (pi *PodInfo) updateLegacyMigResourceRequestFromAnnotations() {
	for annotationName, annotationValue := range pi.Pod.Annotations {
		if resource_info.IsMigResource(v1.ResourceName(annotationName)) {
			value, err := strconv.ParseInt(annotationValue, 10, 64)
			if err != nil {
				log.InfraLogger.V(2).Infof("Could not parse pod annotation of mig resource as int64. Annotation: %v, Value: %v", annotationName, annotationValue)
				continue
			}
			migResources := map[v1.ResourceName]int64{
				v1.ResourceName(annotationName): value,
			}
			pi.GpuRequirement = *resource_info.NewGpuResourceRequirementWithMig(migResources)
			pi.IsLegacyMIGtask = true
		}
	}
}

// rebuildResReqVector rebuilds ResReqVector from the current vector base resources and GpuRequirement.
func (pi *PodInfo) rebuildResReqVector() {
	pi.ResReqVector.Set(resource_info.GPUIndex, pi.GpuRequirement.GPUs()+float64(pi.GpuRequirement.GetDraGpusCount()))
	for migName, migCount := range pi.GpuRequirement.MigResources() {
		if idx := pi.VectorMap.GetIndex(migName); idx >= 0 {
			pi.ResReqVector.Set(idx, float64(migCount))
		}
	}
}

func (pi *PodInfo) ShouldAllocate(isRealAllocation bool) bool {
	return pi.Status == pod_status.Pending ||
		(!isRealAllocation && pi.Status == pod_status.Releasing && pi.IsVirtualStatus)
}
