/*
Copyright The Kubernetes Authors.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Disclaimer: This Topology CRD was derived from Kueue's Topology CRD structure.

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster

// Topology is the Schema for the topology API
type Topology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec TopologySpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// TopologyList contains a list of Topology
type TopologyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Topology `json:"items"`
}

// TopologySpec defines the desired state of Topology
type TopologySpec struct {
	// levels define the levels of topology.
	//
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="field is immutable"
	// +kubebuilder:validation:XValidation:rule="size(self.filter(i, size(self.filter(j, j == i)) > 1)) == 0",message="must be unique"
	// +kubebuilder:validation:XValidation:rule="size(self.filter(i, i.nodeLabel == 'kubernetes.io/hostname')) == 0 || self[size(self) - 1].nodeLabel == 'kubernetes.io/hostname'",message="the kubernetes.io/hostname label can only be used at the lowest level of topology"
	Levels []TopologyLevel `json:"levels,omitempty"`
}

// TopologyLevel defines the desired state of TopologyLevel
type TopologyLevel struct {
	// nodeLabel indicates the name of the node label for a specific topology
	// level.
	//
	// Examples:
	// - cloud.provider.com/topology-block
	// - cloud.provider.com/topology-rack
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=316
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	NodeLabel string `json:"nodeLabel"`
}

func init() {
	SchemeBuilder.Register(&Topology{}, &TopologyList{})
}
