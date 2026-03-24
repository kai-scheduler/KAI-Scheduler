// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1alpha2

import (
	v1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BindRequestSpec defines the desired state of BindRequest
type BindRequestSpec struct {
	// PodName is the name of the pod to bind
	PodName string `json:"podName,omitempty"`

	// SelectedNode is the name of the selected node the pod should be bound to
	SelectedNode string `json:"selectedNode,omitempty"`

	// ReceivedResourceType is the type of the resource that was received [Regular/Fraction]
	ReceivedResourceType string `json:"receivedResourceType,omitempty"`

	// ReceivedGPU is the amount of GPUs that were received
	ReceivedGPU *ReceivedGPU `json:"receivedGPU,omitempty"`

	// SelectedGPUGroups is the name of the selected GPU groups for fractional GPU resources.
	// Only if the RecievedResourceType is "Fraction"
	SelectedGPUGroups []string `json:"selectedGPUGroups,omitempty"`

	// ResourceClaims is the list of resource claims that need to be bound for this pod
	ResourceClaimAllocations []ResourceClaimAllocation `json:"resourceClaimAllocations,omitempty"`

	// BackoffLimit is the number of retries before giving up
	BackoffLimit *int32 `json:"backoffLimit,omitempty"`
}

type ReceivedGPU struct {
	// Count is the amount of GPUs devices that were received
	Count int `json:"count,omitempty"`

	// This is the portion size that the pod will receive from each connected GPU device
	// This is a serialized float that should be written as a decimal point number.
	Portion string `json:"portion,omitempty"`
}

type ResourceClaimAllocation struct {
	// Name corresponds to the podResourceClaim.Name from the pod spec
	Name string `json:"name,omitempty"`

	// Allocation is the desired allocation of the resource claim
	Allocation *v1.AllocationResult `json:"allocation,omitempty"`
}

const (
	BindRequestPhasePending   = "Pending"
	BindRequestPhaseSucceeded = "Succeeded"
	BindRequestPhaseFailed    = "Failed"
)

// BindRequestStatus defines the observed state of BindRequest
type BindRequestStatus struct {
	// Phase is the current phase of the bindrequest. [Pending/Succeeded/Failed]
	Phase string `json:"phase,omitempty"`

	// Reason is the reason for the current phase
	Reason string `json:"reason,omitempty"`

	// FailedAttempts is the number of failed attempts
	FailedAttempts int32 `json:"failedAttempts,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// BindRequest is the Schema for the bindrequests API
type BindRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BindRequestSpec   `json:"spec,omitempty"`
	Status BindRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BindRequestList contains a list of BindRequest.
type BindRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BindRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BindRequest{}, &BindRequestList{})
}
