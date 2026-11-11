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
	// OnCommit runs inline on the scheduling goroutine for Statement.Commit,
	// Statement.Discard, and Session.Evict. It must not block.
	OnCommit func(event *CommitEvent)
}

// CommitOp is one operation inside a commit. Fields are the values read off the operation.
type CommitOp struct {
	Kind         string
	UID          string
	Namespace    string
	Name         string
	Node         string
	Message      string
	Error        string
	NotAttempted bool
	Eviction     eviction_info.EvictionMetadata
}

// CommitEvent is one commit, or a discard. DiscardedOpCount is the number of
// operations undone by Statement.Discard, and Ops is empty in that case.
type CommitEvent struct {
	SessionID        string
	Action           string
	Ops              []CommitOp
	DiscardedOpCount int
}
