// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package recorder buffers scheduling records and sends them to a sidecar without
// blocking the scheduling goroutine.
package recorder

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	record "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	defaultAddress         = "127.0.0.1:9095"
	defaultCycleBuffer     = 4
	defaultLifecycleBuffer = 1024
	dialTimeout            = 500 * time.Millisecond
	writeTimeout           = 2 * time.Second
)

var (
	droppedRecords = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kai_scheduler_recorder_dropped_records_total",
		Help: "Scheduling records dropped before they reached the recorder sidecar.",
	}, []string{"stream"})
	queueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kai_scheduler_recorder_queue_depth",
		Help: "Records waiting to be sent to the recorder sidecar.",
	}, []string{"stream"})
	sentRecords = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kai_scheduler_recorder_sent_records_total",
		Help: "Scheduling records written to the recorder sidecar.",
	}, []string{"stream"})
)

// Config is the process-wide sender configuration. The first Start wins.
type Config struct {
	Address         string
	CycleBuffer     int
	LifecycleBuffer int
}

type queuedRecord struct {
	seq        uint64
	kind       string
	enqueuedAt time.Time
	payload    any
}

type streamState struct {
	name    string
	ch      chan queuedRecord
	seq     atomic.Uint64
	pending atomic.Uint64
}

// Sender is safe for concurrent TryEnqueue. A full buffer or a failed send drops the record.
type Sender struct {
	address string
	epoch   string
	cycle   *streamState
	life    *streamState
}

var (
	startOnce sync.Once
	global    *Sender
)

// EnsureStarted returns the process-wide sender, creating it on the first call.
func EnsureStarted(cfg Config) *Sender {
	startOnce.Do(func() {
		global = NewSender(cfg)
	})
	return global
}

// NewSender starts two drain goroutines, one per stream, so a lifecycle burst cannot stall cycle records.
func NewSender(cfg Config) *Sender {
	if cfg.Address == "" {
		cfg.Address = defaultAddress
	}
	if cfg.CycleBuffer <= 0 {
		cfg.CycleBuffer = defaultCycleBuffer
	}
	if cfg.LifecycleBuffer <= 0 {
		cfg.LifecycleBuffer = defaultLifecycleBuffer
	}
	sender := &Sender{
		address: cfg.Address,
		epoch:   fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63()),
		cycle:   newStream(record.StreamCycle, cfg.CycleBuffer),
		life:    newStream(record.StreamLifecycle, cfg.LifecycleBuffer),
	}
	go sender.loop(sender.cycle)
	go sender.loop(sender.life)
	return sender
}

func newStream(name string, size int) *streamState {
	return &streamState{name: name, ch: make(chan queuedRecord, size)}
}

// TryEnqueue queues payload without serializing it. Marshaling happens on the sender
// goroutine so a large state copy does not stall scheduling. It returns false when the
// record was dropped.
func (s *Sender) TryEnqueue(stream, kind string, payload any) bool {
	if s == nil {
		return false
	}
	st := s.stream(stream)
	seq := st.seq.Add(1)
	item := queuedRecord{
		seq:        seq,
		kind:       kind,
		enqueuedAt: time.Now().UTC(),
		payload:    payload,
	}
	select {
	case st.ch <- item:
		queueDepth.WithLabelValues(st.name).Set(float64(len(st.ch)))
		return true
	default:
		s.drop(st, 1)
		return false
	}
}

func (s *Sender) stream(name string) *streamState {
	if name == record.StreamLifecycle {
		return s.life
	}
	return s.cycle
}

func (s *Sender) drop(st *streamState, n uint64) {
	if n == 0 || st == nil {
		return
	}
	st.pending.Add(n)
	droppedRecords.WithLabelValues(st.name).Add(float64(n))
	log.InfraLogger.V(4).Infof("Recorder dropped %d %s record(s)", n, st.name)
}

func (s *Sender) loop(st *streamState) {
	var conn net.Conn
	started := false
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()
	for item := range st.ch {
		queueDepth.WithLabelValues(st.name).Set(float64(len(st.ch)))
		raw, err := json.Marshal(item.payload)
		if err != nil {
			s.drop(st, 1)
			continue
		}
		env := record.Envelope{
			Stream:           st.name,
			Seq:              item.seq,
			Epoch:            s.epoch,
			Kind:             item.kind,
			EnqueuedAt:       item.enqueuedAt,
			Record:           raw,
			DroppedSinceLast: st.pending.Swap(0),
		}
		// The first record that actually leaves the process marks the new epoch.
		// Earlier sequence numbers may have been dropped while the sidecar was down.
		if !started {
			env.EpochStart = true
		}
		conn, err = writeEnvelope(conn, s.address, &env)
		if err != nil {
			s.drop(st, env.DroppedSinceLast+1)
			if conn != nil {
				_ = conn.Close()
				conn = nil
			}
			continue
		}
		started = true
		sentRecords.WithLabelValues(st.name).Inc()
	}
}

func writeEnvelope(conn net.Conn, address string, env *record.Envelope) (net.Conn, error) {
	payload, err := json.Marshal(env)
	if err != nil {
		return conn, err
	}
	payload = append(payload, '\n')
	if conn == nil {
		dialed, dialErr := net.DialTimeout("tcp", address, dialTimeout)
		if dialErr != nil {
			return nil, dialErr
		}
		conn = dialed
	}
	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return conn, err
	}
	if _, err := conn.Write(payload); err != nil {
		return conn, err
	}
	return conn, nil
}
