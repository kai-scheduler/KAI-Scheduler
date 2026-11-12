/*
Copyright 2018 The Kubernetes Authors.

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

package framework

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

// SessionCloseAction tags commits that run while plugins close, after the configured actions.
const SessionCloseAction = "session-close"

type Event struct {
	Task *pod_info.PodInfo
}

type EventHandler struct {
	AllocateFunc   func(event *Event)
	DeallocateFunc func(event *Event)
	// OnCommit runs inline on the scheduling goroutine for Statement.Commit
	// and Session.Evict. Discard does not publish. It must not block.
	OnCommit func(event *CommitEvent)
}

// CommitOp is one operation inside a commit. Fields are the values read off the operation.
type CommitOp struct {
	Kind         string                         `json:"kind"`
	UID          string                         `json:"uid,omitempty"`
	Namespace    string                         `json:"namespace,omitempty"`
	Name         string                         `json:"name,omitempty"`
	Node         string                         `json:"node,omitempty"`
	Message      string                         `json:"message,omitempty"`
	Error        string                         `json:"error,omitempty"`
	NotAttempted bool                           `json:"notAttempted,omitempty"`
	Eviction     eviction_info.EvictionMetadata `json:"eviction"`
}

// CommitEvent is one committed statement. Undo operations are not included.
type CommitEvent struct {
	SessionID string     `json:"sessionID"`
	Action    string     `json:"action,omitempty"`
	Ops       []CommitOp `json:"ops,omitempty"`
}
