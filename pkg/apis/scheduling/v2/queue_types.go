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

package v2

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// QueueSpec defines the desired state of Queue
type QueueSpec struct {
	DisplayName string          `json:"displayName,omitempty"`
	ParentQueue string          `json:"parentQueue,omitempty"`
	Resources   *QueueResources `json:"resources,omitempty"`
	// Priority of the queue. Over-quota resources will be divided first among queues with higher priority. Queues with
	// higher priority will be considerd first for allocation, and last for reclaim. When not set, default is 100.
	// +optional
	Priority *int `json:"priority,omitempty"`

	// Minimum runtime of a job in queue before it can be preempted.
	// +optional
	PreemptMinRuntime *metav1.Duration `json:"preemptMinRuntime,omitempty"`

	// Minimum runtime of a job in queue before it can be reclaimed.
	// +optional
	ReclaimMinRuntime *metav1.Duration `json:"reclaimMinRuntime,omitempty"`
}

// QueueStatus defines the observed state of Queue
type QueueStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Current conditions of the queue
	// +optional
	Conditions []QueueCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`

	// List of queues in cluster which specify this queue as parent
	// +optional
	ChildQueues []string `json:"childQueues,omitempty"`

	// Current allocated GPU (in fractions), CPU (in millicpus) and Memory in megabytes
	// for all running jobs in queue and child queues
	Allocated v1.ResourceList `json:"allocated,omitempty"`

	// Current allocated GPU (in fractions), CPU (in millicpus) and Memory in megabytes
	// for all non-preemptible running jobs in queue and child queues
	AllocatedNonPreemptible v1.ResourceList `json:"allocatedNonPreemptible,omitempty"`

	// Current requested GPU (in fractions), CPU (in millicpus) and Memory in megabytes
	// by all running and pending jobs in queue and child queues
	Requested v1.ResourceList `json:"requested,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Priority",type=string,JSONPath=`.spec.priority`
// +kubebuilder:printcolumn:name="Parent",type=string,JSONPath=`.spec.parentQueue`
// +kubebuilder:printcolumn:name="Children",type=string,JSONPath=`.status.childQueues`
// +kubebuilder:printcolumn:name="DisplayName",type=string,JSONPath=`.spec.displayName`

// Queue is the Schema for the queues API
type Queue struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   QueueSpec   `json:"spec,omitempty"`
	Status QueueStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// QueueList contains a list of Queue
type QueueList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Queue `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Queue{}, &QueueList{})
}

// Hub marks this type as a conversion hub.
func (*Queue) Hub() {}
