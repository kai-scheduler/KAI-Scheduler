/*
Copyright 2017 The Kubernetes Authors.

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

package v2alpha2

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PodGroupSpec defines the desired state of PodGroup
type PodGroupSpec struct {
	// MinMember defines the minimal number of members to run the PodGroup;
	// if there are not enough resources to start all required members, the scheduler will not start anyone.
	// +kubebuilder:validation:Minimum=1
	MinMember int32 `json:"minMember,omitempty" protobuf:"bytes,1,opt,name=minMember"`

	// Queue defines the queue to allocate resource for PodGroup; if queue does not exist,
	// the PodGroup will not be scheduled.
	Queue string `json:"queue,omitempty" protobuf:"bytes,2,opt,name=queue"`

	// If specified, indicates the PodGroup's priority. "system-node-critical" and
	// "system-cluster-critical" are two special keywords which indicate the
	// highest priorities with the former being the highest priority. Any other
	// name must be defined by creating a PriorityClass object with that name.
	// If not specified, the PodGroup priority will be default or zero if there is no
	// default.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty" protobuf:"bytes,3,opt,name=priorityClassName"`

	// Preemptibility determines if this PodGroup can be preempted by higher priority workloads.
	// When unspecified, preemptibility is determined by the PriorityClass value - values below 100 are considered preemptible.
	Preemptibility Preemptibility `json:"preemptibility,omitempty" protobuf:"bytes,4,opt,name=preemptibility"`

	// The number of pods which will try to run at any instant.
	Parallelism int32 `json:"parallelism,omitempty" protobuf:"bytes,4,opt,name=parallelism"`

	// The number of successful pods required for this podgroup to move to 'Successful' phase.
	Completions int32 `json:"completions,omitempty" protobuf:"bytes,5,opt,name=completions"`

	// The number of failed pods required for this podgroup to move to 'Failed' phase.
	BackoffLimit int32 `json:"backoffLimit,omitempty" protobuf:"bytes,6,opt,name=backoffLimit"`

	// Should add "Unschedulable" event to the pods or not.
	MarkUnschedulable *bool `json:"markUnschedulable,omitempty" protobuf:"bytes,7,opt,name=markUnschedulable"`

	// The number of scheduling cycles to try before marking the pod group as UnschedulableOnNodePool. Currently only supporting -1 and 1
	SchedulingBackoff *int32 `json:"schedulingBackoff,omitempty" protobuf:"bytes,8,opt,name=schedulingBackoff"`

	// TopologyConstraint defines the topology constraints for this PodGroup
	TopologyConstraint TopologyConstraint `json:"topologyConstraint,omitempty"`

	// SubGroups defines finer-grained subsets of pods within the PodGroup with individual scheduling constraints
	SubGroups []SubGroup `json:"subGroups,omitempty"`
}

// Preemptibility defines whether this PodGroup can be preempted
//
// Supported values are:
// - `preemptible` - PodGroup can be preempted by higher-priority workloads
// - `non-preemptible` - PodGroup runs to completion once scheduled
//
// Defaults to priority-based preemptibility determination (preemptible if priority < 100)
//
// +kubebuilder:validation:Enum=preemptible;non-preemptible
// +optional
type Preemptibility string

const (
	Preemptible    Preemptibility = "preemptible"
	NonPreemptible Preemptibility = "non-preemptible"
)

func ParsePreemptibility(value string) (Preemptibility, error) {
	switch value {
	case string(Preemptible):
		return Preemptible, nil
	case string(NonPreemptible):
		return NonPreemptible, nil
	case "":
		// Empty value is valid and represents the default priority-based preemptibility
		return "", nil
	default:
		return "", fmt.Errorf("invalid preemptibility value: %s", value)
	}
}

type SubGroup struct {
	// Name uniquely identifies the SubGroup within the PodGroup.
	// Must be a valid DNS label (RFC 1123).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// MinMember defines the minimal number of members to run this SubGroup;
	// if there are not enough resources to start all required members, the scheduler will not start anyone.
	// +kubebuilder:validation:Minimum=1
	MinMember int32 `json:"minMember,omitempty"`

	// Parent is an optional attribute that specifies the name of the parent SubGroup.
	// Must be a valid DNS label (RFC 1123).
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Parent *string `json:"parent,omitempty"`

	// TopologyConstraint defines the topology constraints for this SubGroup
	TopologyConstraint *TopologyConstraint `json:"topologyConstraint,omitempty"`
}

// PodGroupStatus defines the observed state of PodGroup
type PodGroupStatus struct {
	// Current phase of PodGroup.
	Phase PodGroupPhase `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`

	// The conditions of PodGroup.
	// +optional
	Conditions []PodGroupCondition `json:"conditions,omitempty" protobuf:"bytes,2,opt,name=conditions"`

	// The number of actively running pods.
	// +optional
	Running int32 `json:"running,omitempty" protobuf:"bytes,3,opt,name=running"`

	// The number of pods which reached phase Succeeded.
	// +optional
	Succeeded int32 `json:"succeeded,omitempty" protobuf:"bytes,4,opt,name=succeeded"`

	// The number of pods which reached phase Failed.
	// +optional
	Failed int32 `json:"failed,omitempty" protobuf:"bytes,5,opt,name=failed"`

	// The number of pods which reached phase Pending.
	// +optional
	Pending int32 `json:"pending,omitempty" protobuf:"bytes,6,opt,name=pending"`

	// Status of resources related to pods connected to this pod group.
	// +optional
	ResourcesStatus PodGroupResourcesStatus `json:"resourcesStatus,omitempty"`

	// The scheduling conditions of PodGroup.
	// +optional
	SchedulingConditions []SchedulingCondition `json:"schedulingConditions,omitempty" protobuf:"bytes,7,opt,name=schedulingConditions"`
}

// PodGroupPhase is the phase of a pod group at the current time.
type PodGroupPhase string

type PodGroupConditionType string

// PodGroupResourcesStatus contains the status of resources related to pods connected to this pod group.
type PodGroupResourcesStatus struct {
	// Current allocated GPU (in fracions), CPU (in millicpus), Memory in megabytes and any extra resources in ints
	// for all preemptible resources used by pods of this pod group
	// +optional
	Allocated v1.ResourceList `json:"allocated,omitempty"`

	// Current allocated GPU (in fracions), CPU (in millicpus) and Memory in megabytes and any extra resources in ints
	// for all non-preemptible resources used by pods of this pod group
	// +optional
	AllocatedNonPreemptible v1.ResourceList `json:"allocatedNonPreemptible,omitempty"`

	// Current requested GPU (in fracions), CPU (in millicpus) and Memory in megabytes
	// by all running and pending jobs in queue and child queues
	// +optional
	Requested v1.ResourceList `json:"requested,omitempty"`
}

// PodGroupCondition contains details for the current state of this pod group.
type PodGroupCondition struct {
	// Type is the type of the condition
	Type PodGroupConditionType `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`

	// Status is the status of the condition.
	Status v1.ConditionStatus `json:"status,omitempty" protobuf:"bytes,2,opt,name=status"`

	// The ID of condition transition.
	TransitionID string `json:"transitionID,omitempty" protobuf:"bytes,3,opt,name=transitionID"`

	// Last time the phase transitioned from another to current phase.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`

	// Unique, one-word, CamelCase reason for the phase's last transition.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`

	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
}

// SchedulingCondition contains details for the current scheduling state of this pod group.
type SchedulingCondition struct {
	// Type is the type of the scheduling condition
	Type SchedulingConditionType `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`

	// The Node Pool name on witch the scheduling condition happened
	NodePool string `json:"nodePool,omitempty" protobuf:"bytes,2,opt,name=nodePool"`

	// Unique, one-word, CamelCase reason for the phase's last transition.
	// Deprecated: use Reasons instead
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,3,opt,name=reason"`

	// Human-readable message indicating details about the condition.
	// Deprecated: use Reasons instead
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,4,opt,name=message"`

	// Reasons is a map of UnschedulableReason to a human-readable message indicating details about the condition.
	// Clients can handle specific reasons, but more types of reasons could be added in the future.
	// +optional
	Reasons UnschedulableExplanations `json:"reasons,omitempty" protobuf:"bytes,5,opt,rep,casttype=UnschedulableExplanations,castkey=UnschedulableReason,name=reasons"`

	// The ID of condition transition.
	TransitionID string `json:"transitionID,omitempty" protobuf:"bytes,5,opt,name=transitionID"`

	// Last time the phase transitioned from another to current phase.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,6,opt,name=lastTransitionTime"`

	// Status is the status of the condition.
	// +optional
	Status v1.ConditionStatus `json:"status,omitempty" protobuf:"bytes,7,opt,name=status"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:shortName=pg

// PodGroup is the Schema for the podgroups API
type PodGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodGroupSpec   `json:"spec,omitempty"`
	Status PodGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PodGroupList contains a list of PodGroup
type PodGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodGroup `json:"items"`
}

type SchedulingConditionType string

const (
	// UnschedulableOnNodePool means the pod group is Unschedulable on the current node pool
	UnschedulableOnNodePool SchedulingConditionType = "UnschedulableOnNodePool"
)

// These are reasons for a pod group's transition to a condition.
const (
	// PodGroupReasonUnschedulable reason in SchedulingCondition means that the scheduler
	// can't schedule the pod group right now, for example due to insufficient resources in the cluster.
	PodGroupReasonUnschedulable = "Unschedulable"
)

type UnschedulableReason string

type UnschedulableExplanations []UnschedulableExplanation

type UnschedulableExplanation struct {
	// Reason is a brief, one-word explanation of why the pod group is unschedulable.
	Reason UnschedulableReason `json:"reason,omitempty" protobuf:"bytes,1,opt,name=reason"`

	// Message is a human-readable explanation of why the pod group is unschedulable. Can be used by clients when not programmed to handle specific error.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,1,opt,name=message"`

	// Details contains structured information about why the pod group is unschedulable. Can be used by clients to handle specific errors.
	// Different fields will be set depending on the reason for unschedulability. Use helper functions, such as AsQueueDetails(), to interpret the details.
	// +optional
	Details *UnschedulableExplanationDetails `json:"details,omitempty" protobuf:"bytes,2,opt,name=details"`
}

type UnschedulableExplanationDetails struct {
	// QueueDetails contains information about the queue that the pod group is trying to schedule in. Used in NonPreemptibleOverQuota and OverLimit reasons.
	// +optional
	QueueDetails *QuotaDetails `json:"queueDetails,omitempty" protobuf:"bytes,1,opt,name=queueDetails"`
}

type QuotaDetails struct {
	// Name is the name of the queue.
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// QueueRequestedResources is the requested resources of the queue at the time of the error.
	// +optional
	QueueRequestedResources v1.ResourceList `json:"queueRequestedResources,omitempty" protobuf:"bytes,2,opt,name=queueRequestedResources"`

	// QueueDeservedResources is the deserved resources of the queue at the time of the error.
	// +optional
	QueueDeservedResources v1.ResourceList `json:"queueDeservedResources,omitempty" protobuf:"bytes,3,opt,name=queueDeservedResources"`

	// QueueAllocatedResources is the allocated resources of the queue at the time of the error, including preemptible and non-preemptible resources.
	// +optional
	QueueAllocatedResources v1.ResourceList `json:"queueAllocatedResources,omitempty" protobuf:"bytes,3,opt,name=queueAllocatedResources"`

	// QueueAllocatedNonPreemptibleResources is the allocated non-preemptible resources of the queue at the time of the error.
	// +optional
	QueueAllocatedNonPreemptibleResources v1.ResourceList `json:"queueAllocatedNonPreemptibleResources,omitempty" protobuf:"bytes,4,opt,name=queueAllocatedNonPreemptibleResources"`

	// QueueResourceLimits is the resource limits of the queue at the time of the error.
	// +optional
	QueueResourceLimits v1.ResourceList `json:"queueResourceLimits,omitempty" protobuf:"bytes,4,opt,name=queueResourceLimits"`

	// PodGroupRequestedResources is the requested resources needed to satisfy the minimum number of pods for the pod group at the time of the error, including preemptible and non-preemptible resources.
	// +optional
	PodGroupRequestedResources v1.ResourceList `json:"podGroupRequestedResources,omitempty" protobuf:"bytes,5,opt,name=podGroupRequestedResources"`

	// PodGroupRequestedNonPreemptibleResources is the requested non-preemptible resources of the pod group at the time of the error.
	// +optional
	PodGroupRequestedNonPreemptibleResources v1.ResourceList `json:"podGroupRequestedNonPreemptibleResources,omitempty" protobuf:"bytes,6,opt,name=podGroupRequestedNonPreemptibleResources"`
}

const (
	// NonPreemptibleOverQuota means that the pod group is not schedulable because scheduling it would make the queue's
	// non-preemptible resource allocation larger than the queue's quota.
	NonPreemptibleOverQuota UnschedulableReason = "NonPreemptibleOverQuota"

	// OverLimit means that the pod group is not schedulable because scheduling it would exceed the queue's limits.
	OverLimit UnschedulableReason = "OverLimit"

	// QueueDoesNotExist means the pod group references a queue that doesn't exist or has no parent queue.
	QueueDoesNotExist UnschedulableReason = "QueueDoesNotExist"
)

func (e UnschedulableExplanations) String() string {
	var sb strings.Builder
	for _, v := range e {
		sb.WriteString(string(v.Reason))
		sb.WriteString(": ")
		sb.WriteString(v.Message)
		sb.WriteString("\n")
	}
	return sb.String()
}

func init() {
	SchemeBuilder.Register(&PodGroup{}, &PodGroupList{})
}

type TopologyConstraint struct {
	// PreferredTopologyLevel defines the preferred level in the topology hierarchy
	// that this constraint applies to (e.g., "rack", "zone", "datacenter").
	// Jobs will be scheduled to maintain locality at this level when possible.
	PreferredTopologyLevel string `json:"preferredTopologyLevel,omitempty"`

	// RequiredTopologyLevel defines the maximal level in the topology hierarchy
	// that all pods must be scheduled within.
	// If set, all pods of the job must be scheduled within a single domain at this level.
	RequiredTopologyLevel string `json:"requiredTopologyLevel,omitempty"`

	// Topology specifies the name of the topology CRD that defines the
	// physical layout to use for this constraint. This allows for supporting
	// multiple different topology configurations in the same cluster.
	Topology string `json:"topology,omitempty"`
}
