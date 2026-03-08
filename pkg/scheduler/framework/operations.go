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
// ???
// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
)

const (
	// Evict op
	evict = "evict"
	// Pipeline op
	pipeline = "pipeline"
	// Allocate op
	allocate = "allocate"
	// Undo op
	undo = "undo"
)

type Operation interface {
	Name() string
	TaskInfo() *pod_info.PodInfo
	Reverse() error
}

type ReverseOperation func() error

type evictOperation struct {
	taskInfo                  *pod_info.PodInfo
	previousStatus            pod_status.PodStatus
	previousNode              *node_info.NodeInfo
	previousGpuGroups         []string
	previousResourceClaimInfo bindrequest_info.ResourceClaimInfo
	message                   string
	evictionMetadata          eviction_info.EvictionMetadata
	reverseOperation          ReverseOperation
}

func (op evictOperation) Name() string {
	return evict
}

func (op evictOperation) TaskInfo() *pod_info.PodInfo {
	return op.taskInfo
}

func (op evictOperation) Reverse() error {
	return op.reverseOperation()
}

type allocateOperation struct {
	taskInfo         *pod_info.PodInfo
	nextNode         string
	reverseOperation ReverseOperation
}

func (op allocateOperation) Name() string {
	return allocate
}

func (op allocateOperation) TaskInfo() *pod_info.PodInfo {
	return op.taskInfo
}

func (op allocateOperation) Reverse() error {
	return op.reverseOperation()
}

type pipelineOperation struct {
	taskInfo                  *pod_info.PodInfo
	previousStatus            pod_status.PodStatus
	previousNode              string
	previousGpuGroups         []string
	previousResourceClaimInfo bindrequest_info.ResourceClaimInfo
	nextNode                  string
	message                   string
	reverseOperation          ReverseOperation
}

func (op pipelineOperation) Name() string {
	return pipeline
}

func (op pipelineOperation) TaskInfo() *pod_info.PodInfo {
	return op.taskInfo
}

func (op pipelineOperation) Reverse() error {
	return op.reverseOperation()
}

type undoOperation struct {
	operationIndex   int
	reverseOperation ReverseOperation
}

func (op undoOperation) Name() string {
	return undo
}

func (op undoOperation) TaskInfo() *pod_info.PodInfo {
	return &pod_info.PodInfo{
		UID: "",
		Job: "",
	}
}

func (op undoOperation) Reverse() error {
	return op.reverseOperation()
}
