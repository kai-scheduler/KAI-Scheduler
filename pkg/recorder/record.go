// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package recorder is the on-disk record format shared by the scheduler plugin and the sidecar.
package recorder

import (
	"encoding/json"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
)

const (
	StreamCycle     = "cycle"
	StreamLifecycle = "lifecycle"

	KindState      = "state"
	KindDecision   = "decision"
	KindSessionEnd = "session-end"
	KindLifecycle  = "lifecycle"
)

// Envelope is one line on the localhost stream. Sequence numbers are per stream
// and are assigned when the record is accepted into the buffer. A gap in Seq, or
// DroppedSinceLast greater than zero, means records were dropped.
type Envelope struct {
	Stream           string          `json:"stream"`
	Seq              uint64          `json:"seq"`
	Epoch            string          `json:"epoch"`
	EpochStart       bool            `json:"epochStart,omitempty"`
	DroppedSinceLast uint64          `json:"droppedSinceLast,omitempty"`
	Kind             string          `json:"kind"`
	EnqueuedAt       time.Time       `json:"enqueuedAt"`
	Record           json.RawMessage `json:"record"`
}

// StateRecord is the cluster state copied at session open for one scheduling cycle.
type StateRecord struct {
	SessionID       string                         `json:"sessionID"`
	Partial         bool                           `json:"partial,omitempty"`
	CommitSHA       string                         `json:"commitSHA,omitempty"`
	ClusterName     string                         `json:"clusterName,omitempty"`
	ShardName       string                         `json:"shardName,omitempty"`
	OpenedAt        time.Time                      `json:"openedAt"`
	Config          *conf.SchedulerConfiguration   `json:"config,omitempty"`
	SchedulerParams *conf.SchedulerParams          `json:"schedulerParams,omitempty"`
	RawObjects      *snapshot.RawKubernetesObjects `json:"rawObjects,omitempty"`
	Discovery       *snapshot.DiscoverySnapshot    `json:"discovery,omitempty"`
}

// DecisionOp is one committed operation. It is the recorder's copy of a framework commit op.
type DecisionOp struct {
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

// DecisionRecord is one committed statement.
type DecisionRecord struct {
	SessionID string       `json:"sessionID"`
	Action    string       `json:"action,omitempty"`
	Ops       []DecisionOp `json:"ops,omitempty"`
}

// ActionStat is the per-action summary for one cycle.
type ActionStat struct {
	WallTimeMs        int64 `json:"wallTimeMs"`
	DiscardedAttempts int   `json:"discardedAttempts,omitempty"`
	DiscardedOps      int   `json:"discardedOps,omitempty"`
	Commits           int   `json:"commits,omitempty"`
}

// FailReason is one unschedulable reason code for a pod group. Reason is the code only.
type FailReason struct {
	PodUID    string `json:"podUID,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// SessionEndRecord closes a cycle. Partial is set when the state copy was missing or incomplete.
type SessionEndRecord struct {
	SessionID   string                `json:"sessionID"`
	Pending     bool                  `json:"pending,omitempty"`
	Committed   bool                  `json:"committed,omitempty"`
	Partial     bool                  `json:"partial,omitempty"`
	ActionStats map[string]ActionStat `json:"actionStats,omitempty"`
	FailReasons []FailReason          `json:"failReasons,omitempty"`
}

// LifecycleRecord is a change the scheduler informers observed. SeenAt is when the
// scheduler saw it, not an object timestamp.
type LifecycleRecord struct {
	ObjectKind    string     `json:"objectKind"`
	Op            string     `json:"op"`
	Namespace     string     `json:"namespace,omitempty"`
	Name          string     `json:"name,omitempty"`
	UID           string     `json:"uid,omitempty"`
	Phase         string     `json:"phase,omitempty"`
	NodeName      string     `json:"nodeName,omitempty"`
	StartTime     *time.Time `json:"startTime,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	Unschedulable *bool      `json:"unschedulable,omitempty"`
	Ready         string     `json:"ready,omitempty"`
	GPUGroups     []string   `json:"gpuGroups,omitempty"`
	ResourceType  string     `json:"resourceType,omitempty"`
	BackoffLimit  *int32     `json:"backoffLimit,omitempty"`
	SeenAt        time.Time  `json:"seenAt"`
}
