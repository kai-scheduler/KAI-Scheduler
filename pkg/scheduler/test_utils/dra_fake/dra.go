// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dra_fake

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
)

type TestDRAObjects struct {
	DeviceClasses  []string
	ResourceSlices []*TestResourceSlice
	ResourceClaims []*TestResourceClaim
}

type TestResourceSlice struct {
	Name            string
	DeviceClassName string

	// NodeName identifies the node which provides the resources in this pool.
	// A field selector can be used to list only ResourceSlice
	// objects belonging to a certain node.
	//
	// This field can be used to limit access from nodes to ResourceSlices with
	// the same node name. It also indicates to autoscalers that adding
	// new nodes of the same type as some old node might also make new
	// resources available.
	//
	// Exactly one of NodeName, NodeSelector and AllNodes must be set.
	// This field is immutable.
	//
	// +optional
	// +oneOf=NodeSelection
	NodeName string `json:"nodeName,omitempty" protobuf:"bytes,3,opt,name=nodeName"`

	// NodeSelector defines which nodes have access to the resources in the pool,
	// when that pool is not limited to a single node.
	//
	// Must use exactly one term.
	//
	// Exactly one of NodeName, NodeSelector and AllNodes must be set.
	//
	// +optional
	// +oneOf=NodeSelection
	NodeSelector *v1.NodeSelector `json:"nodeSelector,omitempty" protobuf:"bytes,4,opt,name=nodeSelector"`

	// AllNodes indicates that all nodes have access to the resources in the pool.
	//
	// Exactly one of NodeName, NodeSelector and AllNodes must be set.
	//
	// +optional
	// +oneOf=NodeSelection
	AllNodes bool `json:"allNodes,omitempty" protobuf:"bytes,5,opt,name=allNodes"`

	Count int
}

type TestResourceClaim struct {
	// Name can be used to reference this request in a pod.spec.containers[].resources.claims
	// entry and in a constraint of the claim.
	//
	// Must be a DNS label.
	//
	// +required
	Name string `json:"name" protobuf:"bytes,1,name=name"`

	Namespace string

	// DeviceClassName references a specific DeviceClass, which can define
	// additional configuration and selectors to be inherited by this
	// request.
	//
	// A class is required. Which classes are available depends on the cluster.
	//
	// Administrators may use this to restrict which devices may get
	// requested by only installing classes with selectors for permitted
	// devices. If users are free to request anything without restrictions,
	// then administrators can create an empty DeviceClass for users
	// to reference.
	//
	// +required
	DeviceClassName string `json:"deviceClassName" protobuf:"bytes,2,name=deviceClassName"`

	// Count is used only when the count mode is "ExactCount". Must be greater than zero.
	// If AllocationMode is ExactCount and this field is not specified, the default is one.
	//
	// +optional
	// +oneOf=AllocationMode
	Count int64 `json:"count,omitempty" protobuf:"bytes,5,opt,name=count"`

	// ReservedFor indicates which entities are currently allowed to use
	// the claim. A Pod which references a ResourceClaim which is not
	// reserved for that Pod will not be started. A claim that is in
	// use or might be in use because it has been reserved must not get
	// deallocated.
	//
	// In a cluster with multiple scheduler instances, two pods might get
	// scheduled concurrently by different schedulers. When they reference
	// the same ResourceClaim which already has reached its maximum number
	// of consumers, only one pod can be scheduled.
	//
	// Both schedulers try to add their pod to the claim.status.reservedFor
	// field, but only the update that reaches the API server first gets
	// stored. The other one fails with an error and the scheduler
	// which issued it knows that it must put the pod back into the queue,
	// waiting for the ResourceClaim to become usable again.
	//
	// There can be at most 32 such reservations. This may get increased in
	// the future, but not reduced.
	//
	// +optional
	// +listType=map
	// +listMapKey=uid
	// +patchStrategy=merge
	// +patchMergeKey=uid
	ReservedFor []resourceapi.ResourceClaimConsumerReference `json:"reservedFor,omitempty" protobuf:"bytes,6,opt,name=reservedFor"`
}

func RandomReservedForReferences(numReferences int) []resourceapi.ResourceClaimConsumerReference {
	var references []resourceapi.ResourceClaimConsumerReference
	for index := range numReferences {
		references = append(references,
			resourceapi.ResourceClaimConsumerReference{
				Resource: "pods",
				Name:     fmt.Sprintf("pod-%d", index),
				UID:      types.UID(rand.String(8)),
			})
	}
	return references
}
