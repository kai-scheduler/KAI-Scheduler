// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	record "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
)

func TestTryEnqueueDoesNotBlockWhenSidecarIsDown(t *testing.T) {
	sender := NewSender(Config{Address: "127.0.0.1:1", CycleBuffer: 1, LifecycleBuffer: 1})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			sender.TryEnqueue(record.StreamCycle, record.KindDecision, map[string]int{"i": i})
			sender.TryEnqueue(record.StreamLifecycle, record.KindLifecycle, map[string]int{"i": i})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue blocked on a full buffer")
	}
	require.Greater(t, sender.cycle.pending.Load()+sender.life.pending.Load(), uint64(0))
}
