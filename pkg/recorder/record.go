// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package recorder is the on-disk record format shared by the scheduler plugin and the sidecar.
package recorder

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	StreamCycle = "cycle"
	KindCycle   = "cycle"
	// SchemaVersion is the record shape this binary writes. Bump it whenever the record shape changes.
	SchemaVersion = 1
)

// Envelope is one line on the localhost stream. Sequence numbers are assigned when
// the record is accepted into the buffer. DroppedSinceLast greater than zero means
// earlier records in this stream were dropped.
type Envelope struct {
	SchemaVersion    int             `json:"schemaVersion"`
	KaiVersion       string          `json:"kaiVersion,omitempty"`
	Stream           string          `json:"stream"`
	Seq              uint64          `json:"seq"`
	Epoch            string          `json:"epoch"`
	EpochStart       bool            `json:"epochStart,omitempty"`
	DroppedSinceLast uint64          `json:"droppedSinceLast,omitempty"`
	Kind             string          `json:"kind"`
	EnqueuedAt       time.Time       `json:"enqueuedAt"`
	Record           json.RawMessage `json:"record"`
}

// CheckSchema rejects a record written with a newer shape than this binary understands.
// A missing version is an older record and is still readable.
func CheckSchema(version int) error {
	if version > SchemaVersion {
		return fmt.Errorf("recorder schemaVersion %d is newer than this build understands (%d)", version, SchemaVersion)
	}
	return nil
}
