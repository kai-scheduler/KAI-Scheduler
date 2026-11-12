// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package recorder is the on-disk record format shared by the scheduler plugin and the sidecar.
package recorder

import (
	"encoding/json"
	"time"
)

const (
	StreamCycle = "cycle"
	KindCycle   = "cycle"
)

// Envelope is one line on the localhost stream. Sequence numbers are assigned when
// the record is accepted into the buffer. DroppedSinceLast greater than zero means
// earlier records in this stream were dropped.
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
