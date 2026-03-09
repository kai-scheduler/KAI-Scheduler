// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package bindrequest_info

import (
	v1 "k8s.io/api/core/v1"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

type Key common_info.ObjectKey
type BindRequestMap map[Key]*BindRequestInfo

func (brm BindRequestMap) GetBindRequestForPod(pod *v1.Pod) *BindRequestInfo {
	key := NewKeyFromPod(pod)
	request, found := brm[key]
	if !found {
		return nil
	}
	if request.IsFailed() {
		return nil
	}

	return request
}

func NewKey(namespace, name string) Key {
	return Key(common_info.NewObjectKey(namespace, name))
}

func NewKeyFromRequest(request *schedulingv1alpha2.BindRequest) Key {
	return NewKey(request.Namespace, request.Spec.PodName)
}

func NewKeyFromPod(pod *v1.Pod) Key {
	return NewKey(pod.Namespace, pod.Name)
}

type ResourceClaimInfo map[string]*schedulingv1alpha2.ResourceClaimAllocation

func (rci ResourceClaimInfo) Clone() ResourceClaimInfo {
	if rci == nil {
		return nil
	}
	newrci := make(ResourceClaimInfo, len(rci))
	for i, info := range rci {
		newrci[i] = &schedulingv1alpha2.ResourceClaimAllocation{
			Name:       info.Name,
			Allocation: info.Allocation.DeepCopy(),
		}
	}
	return newrci
}

func (rci ResourceClaimInfo) ToSlice() []schedulingv1alpha2.ResourceClaimAllocation {
	if rci == nil {
		return nil
	}
	slice := make([]schedulingv1alpha2.ResourceClaimAllocation, 0, len(rci))
	for _, info := range rci {
		slice = append(slice, *info)
	}
	return slice
}

type BindRequestInfo struct {
	Name        string
	Namespace   string
	BindRequest *schedulingv1alpha2.BindRequest
}

func NewBindRequestInfo(
	bindRequest *schedulingv1alpha2.BindRequest,
) *BindRequestInfo {
	return &BindRequestInfo{
		Name:        bindRequest.Name,
		Namespace:   bindRequest.Namespace,
		BindRequest: bindRequest,
	}
}

func (bri *BindRequestInfo) IsFailed() bool {
	if bri.BindRequest.Status.Phase != schedulingv1alpha2.BindRequestPhaseFailed {
		return false
	}
	if bri.BindRequest.Spec.BackoffLimit == nil {
		return true
	}
	return bri.BindRequest.Status.FailedAttempts >= *bri.BindRequest.Spec.BackoffLimit
}
