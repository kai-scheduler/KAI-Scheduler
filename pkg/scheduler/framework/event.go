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
}

// CommitOp is one operation inside a committed or discarded statement.
// Values are copied at publish time so a handler can retain the event.
type CommitOp struct {
	Kind         string                         `json:"kind"`
	UID          string                         `json:"uid,omitempty"`
	Namespace    string                         `json:"namespace,omitempty"`
	Name         string                         `json:"name,omitempty"`
	Node         string                         `json:"node,omitempty"`
	Message      string                         `json:"message,omitempty"`
	Error        string                         `json:"error,omitempty"`
	NotAttempted bool                           `json:"notAttempted,omitempty"`
	Eviction     eviction_info.EvictionMetadata `json:"eviction,omitempty"`
}

// CommitEvent is emitted once per Statement.Commit, Statement.Discard, and Session.Evict.
// Discarded events carry only DiscardedOpCount. Handlers run on the scheduling goroutine
// and must not block.
type CommitEvent struct {
	SessionID        string     `json:"sessionID"`
	Action           string     `json:"action,omitempty"`
	StatementID      string     `json:"statementID,omitempty"`
	Ops              []CommitOp `json:"ops,omitempty"`
	Discarded        bool       `json:"discarded,omitempty"`
	DiscardedOpCount int        `json:"discardedOpCount,omitempty"`
}

type CommitHandler struct {
	OnCommit func(event *CommitEvent)
}
