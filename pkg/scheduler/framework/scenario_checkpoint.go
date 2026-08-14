// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"sync"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

const defaultScenarioCheckpointCapacity = 4096

// ScenarioCheckpointKey identifies one resumable scenario search per job and action.
type ScenarioCheckpointKey struct {
	Action ActionType
	JobUID common_info.PodGroupID
}

// ScenarioCheckpoint contains only fixed-size fingerprints and scalar resume state.
// It deliberately never retains a session snapshot, scenario, queue, or PodInfo.
type ScenarioCheckpoint struct {
	InputFingerprint [32]byte
	Cursor           [32]byte
	GeneratorName    string
	ProbeK           int
	StopReason       string
}

// ScenarioCheckpointStore keeps a bounded set of cross-session checkpoints.
type ScenarioCheckpointStore struct {
	mu       sync.Mutex
	capacity int
	entries  map[ScenarioCheckpointKey]ScenarioCheckpoint
}

func NewScenarioCheckpointStore() *ScenarioCheckpointStore {
	return newScenarioCheckpointStore(defaultScenarioCheckpointCapacity)
}

func newScenarioCheckpointStore(capacity int) *ScenarioCheckpointStore {
	return &ScenarioCheckpointStore{
		capacity: capacity,
		entries:  make(map[ScenarioCheckpointKey]ScenarioCheckpoint),
	}
}

func (s *ScenarioCheckpointStore) Load(key ScenarioCheckpointKey) (ScenarioCheckpoint, bool) {
	if s == nil {
		return ScenarioCheckpoint{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, found := s.entries[key]
	return checkpoint, found
}

func (s *ScenarioCheckpointStore) Save(key ScenarioCheckpointKey, checkpoint ScenarioCheckpoint) {
	if s == nil || s.capacity <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.entries[key]; !found && len(s.entries) >= s.capacity {
		return
	}
	s.entries[key] = checkpoint
}

func (s *ScenarioCheckpointStore) Delete(key ScenarioCheckpointKey) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
}
