// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
)

func schedulingConstraintsSignature(pod *v1.Pod, storageClaims map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo) common_info.SchedulingConstraintsSignature {
	hash := sha256.New()

	// Storage
	storageRequirements := getStorageRequirements(pod, storageClaims)
	storageRequirementsBytes, _ := json.Marshal(storageRequirements)
	hash.Write(storageRequirementsBytes)

	// Node selector
	if pod.Spec.NodeSelector != nil {
		hash.Write([]byte(fmt.Sprintf("%v", pod.Spec.NodeSelector)))
	}

	// Affinity
	if pod.Spec.Affinity != nil {
		hash.Write([]byte(pod.Spec.Affinity.String()))
	}

	// Tolerations
	if pod.Spec.Tolerations != nil {
		tolerations := make([]tolerationRepresentation, 0, len(pod.Spec.Tolerations))
		for _, toleration := range pod.Spec.Tolerations {
			tolerations = append(tolerations, tolerationRepresentation{
				Key:      toleration.Key,
				Operator: toleration.Operator,
				Value:    toleration.Value,
				Effect:   toleration.Effect,
			})
		}
		tolerationString := fmt.Sprintf("%v", tolerations)
		hash.Write([]byte(tolerationString))
	}

	// Priority
	hash.Write([]byte(pod.Spec.PriorityClassName))
	if pod.Spec.Priority != nil {
		_ = binary.Write(hash, binary.LittleEndian, *pod.Spec.Priority)
	}

	//TopologySpreadConstraints
	if pod.Spec.TopologySpreadConstraints != nil {
		var dereferencedTPCs []dereferencedTopologySpreadConstraints
		for _, tpc := range pod.Spec.TopologySpreadConstraints {
			var minDomains int32
			if tpc.MinDomains != nil {
				minDomains = *tpc.MinDomains
			}

			var nodeAffinityPolicy v1.NodeInclusionPolicy
			if tpc.NodeAffinityPolicy != nil {
				nodeAffinityPolicy = *tpc.NodeAffinityPolicy
			}

			var nodeTaintsPolicy v1.NodeInclusionPolicy
			if tpc.NodeTaintsPolicy != nil {
				nodeTaintsPolicy = *tpc.NodeTaintsPolicy
			}

			dereferencedTPCs = append(dereferencedTPCs, dereferencedTopologySpreadConstraints{
				MaxSkew:            tpc.MaxSkew,
				TopologyKey:        tpc.TopologyKey,
				WhenUnsatisfiable:  tpc.WhenUnsatisfiable,
				LabelSelector:      tpc.LabelSelector,
				MinDomains:         minDomains,
				NodeAffinityPolicy: nodeAffinityPolicy,
				NodeTaintsPolicy:   nodeTaintsPolicy,
				MatchLabelKeys:     tpc.MatchLabelKeys,
			})
		}
		constraints := fmt.Sprintf("%v", dereferencedTPCs)
		hash.Write([]byte(constraints))
	}

	// Ports
	for _, container := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
		for _, port := range container.Ports {
			_ = binary.Write(hash, binary.LittleEndian, port.HostPort)
		}
	}

	return common_info.SchedulingConstraintsSignature(fmt.Sprintf("%x", hash.Sum(nil)))
}

type dereferencedTopologySpreadConstraints struct {
	MaxSkew            int32                            `json:"maxSkew" protobuf:"varint,1,opt,name=maxSkew"`
	TopologyKey        string                           `json:"topologyKey" protobuf:"bytes,2,opt,name=topologyKey"`
	WhenUnsatisfiable  v1.UnsatisfiableConstraintAction `json:"whenUnsatisfiable" protobuf:"bytes,3,opt,name=whenUnsatisfiable,casttype=UnsatisfiableConstraintAction"`
	LabelSelector      *metav1.LabelSelector            `json:"labelSelector,omitempty" protobuf:"bytes,4,opt,name=labelSelector"`
	MinDomains         int32                            `json:"minDomains,omitempty" protobuf:"varint,5,opt,name=minDomains"`
	NodeAffinityPolicy v1.NodeInclusionPolicy           `json:"nodeAffinityPolicy,omitempty" protobuf:"bytes,6,opt,name=nodeAffinityPolicy"`
	NodeTaintsPolicy   v1.NodeInclusionPolicy           `json:"nodeTaintsPolicy,omitempty" protobuf:"bytes,7,opt,name=nodeTaintsPolicy"`
	MatchLabelKeys     []string                         `json:"matchLabelKeys,omitempty" protobuf:"bytes,8,opt,name=matchLabelKeys"`
}

type tolerationRepresentation struct {
	Key               string                `json:"key,omitempty" protobuf:"bytes,1,opt,name=key"`
	Operator          v1.TolerationOperator `json:"operator,omitempty" protobuf:"bytes,2,opt,name=operator,casttype=TolerationOperator"`
	Value             string                `json:"value,omitempty" protobuf:"bytes,3,opt,name=value"`
	Effect            v1.TaintEffect        `json:"effect,omitempty" protobuf:"bytes,4,opt,name=effect,casttype=TaintEffect"`
	TolerationSeconds *int64                `json:"tolerationSeconds,omitempty" protobuf:"varint,5,opt,name=tolerationSeconds"`
}
