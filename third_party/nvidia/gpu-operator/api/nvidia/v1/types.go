// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

/*
Package v1 is a minimal, hand-maintained mirror of
github.com/NVIDIA/gpu-operator/api/nvidia/v1.

It contains only the subset of the upstream ClusterPolicy CRD types that
KAI-scheduler reads. We keep a local copy instead of importing the upstream
module because github.com/NVIDIA/gpu-operator publishes CalVer release tags
(v24.x / v25.x / v26.x) without the matching "/vN" module-path suffix that Go's
semantic import versioning requires. As a result the module can only ever be
resolved at v1.x pseudo-versions, which CVE scanners always compare as lower
than the advisory's fixed version (e.g. 25.3.2) — so the dependency is reported
as vulnerable and cannot be bumped to a fixed version.
*/
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// SchemeGroupVersion is the group version used to register these objects.
	SchemeGroupVersion = schema.GroupVersion{Group: "nvidia.com", Version: "v1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// Runtime describes the container runtime used on the nodes.
type Runtime string

const (
	// Docker runtime.
	Docker Runtime = "docker"
	// CRIO runtime.
	CRIO Runtime = "crio"
	// Containerd runtime.
	Containerd Runtime = "containerd"
)

// OperatorSpec mirrors the subset of the upstream operator spec we use.
type OperatorSpec struct {
	DefaultRuntime Runtime `json:"defaultRuntime"`
}

// CDIConfigSpec configures the Container Device Interface (CDI) mechanism.
type CDIConfigSpec struct {
	// Enabled indicates whether CDI can be used to make GPUs accessible to containers.
	Enabled *bool `json:"enabled,omitempty"`
	// Default indicates whether to use CDI as the default mechanism for GPU access.
	Default *bool `json:"default,omitempty"`
}

// ClusterPolicySpec mirrors the subset of the upstream ClusterPolicy spec we use.
type ClusterPolicySpec struct {
	Operator OperatorSpec  `json:"operator"`
	CDI      CDIConfigSpec `json:"cdi,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterPolicy is a trimmed mirror of the nvidia.com/v1 ClusterPolicy resource.
type ClusterPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ClusterPolicySpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterPolicyList contains a list of ClusterPolicy.
type ClusterPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterPolicy{}, &ClusterPolicyList{})
}
