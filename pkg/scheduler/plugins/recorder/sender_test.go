// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestTryEnqueueDoesNotBlockWhenSidecarIsDown(t *testing.T) {
	sender := newSender(senderConfig{Address: "127.0.0.1:1", MaxBytes: 1, Registerer: prometheus.NewRegistry()})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			sender.tryEnqueue(&cycleRecord{SessionID: "s"})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue blocked while the sidecar was down")
	}
}

func TestDropMetricCountsEachLostRecordOnce(t *testing.T) {
	registry := prometheus.NewRegistry()
	sender := newSender(senderConfig{Address: "127.0.0.1:1", MaxBytes: 1 << 30, Registerer: registry})
	for i := 0; i < 20; i++ {
		sender.drop(1)
	}
	metric := sender.dropped.WithLabelValues("cycle")
	require.Equal(t, float64(20), testutil.ToFloat64(metric))
}
