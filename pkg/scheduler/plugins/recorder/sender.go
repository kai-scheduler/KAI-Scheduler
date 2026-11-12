// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

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

	record "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

const (
	defaultAddress = "127.0.0.1:9095"
	dialTimeout    = 500 * time.Millisecond
	writeTimeout   = 2 * time.Second
	// maxQueueBytes bounds how much unsent cycle JSON the process will hold.
	defaultQueueBytes = 32 << 20
)

// Sender marshals cycle records on its own goroutine and drops instead of blocking.
type Sender struct {
	address  string
	epoch    string
	maxBytes int64
	ch       chan *queuedCycle
	seq      atomic.Uint64
	gaps     gapCount
	held     atomic.Int64
	dropped  *prometheus.CounterVec
	queue    prometheus.Gauge
	sent     prometheus.Counter
}

type queuedCycle struct {
	seq        uint64
	enqueuedAt time.Time
	payload    *cycleRecord
	weight     int64
}

type senderConfig struct {
	Address    string
	MaxBytes   int64
	Registerer prometheus.Registerer
}

func newSender(cfg senderConfig) *Sender {
	if cfg.Address == "" {
		cfg.Address = defaultAddress
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultQueueBytes
	}
	registerer := cfg.Registerer
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	namespace := metrics.Namespace()
	dropped := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "recorder_dropped_records_total",
		Help:      "Scheduling records dropped before they reached the recorder sidecar.",
	}, []string{"stream"})
	depth := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "recorder_queue_bytes",
		Help:      "Unsent recorder bytes waiting for the sidecar.",
	})
	sent := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "recorder_sent_records_total",
		Help:      "Scheduling records written to the recorder sidecar.",
	})
	_ = registerer.Register(dropped)
	_ = registerer.Register(depth)
	_ = registerer.Register(sent)
	sender := &Sender{
		address:  cfg.Address,
		epoch:    fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63()),
		maxBytes: cfg.MaxBytes,
		ch:       make(chan *queuedCycle, 8),
		dropped:  dropped,
		queue:    depth,
		sent:     sent,
	}
	go sender.loop()
	return sender
}

// tryEnqueue queues one cycle. Marshaling happens on the sender goroutine.
// It returns false when the record was dropped.
func (s *Sender) tryEnqueue(payload *cycleRecord) bool {
	if s == nil || payload == nil {
		return false
	}
	weight := estimateBytes(payload)
	for {
		held := s.held.Load()
		if held+weight > s.maxBytes && held > 0 {
			s.drop(1)
			return false
		}
		if s.held.CompareAndSwap(held, held+weight) {
			break
		}
	}
	seq := s.seq.Add(1)
	item := &queuedCycle{seq: seq, enqueuedAt: time.Now().UTC(), payload: payload, weight: weight}
	select {
	case s.ch <- item:
		s.queue.Set(float64(s.held.Load()))
		return true
	default:
		s.held.Add(-weight)
		s.drop(1)
		return false
	}
}

func (s *Sender) drop(n uint64) {
	if n == 0 {
		return
	}
	s.gaps.add(n)
	s.dropped.WithLabelValues(record.StreamCycle).Add(float64(n))
	log.InfraLogger.V(4).Infof("Recorder dropped %d cycle record(s)", n)
}

func (s *Sender) loop() {
	var conn net.Conn
	started := false
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()
	for item := range s.ch {
		s.held.Add(-item.weight)
		s.queue.Set(float64(s.held.Load()))
		raw, err := json.Marshal(item.payload)
		if err != nil {
			s.drop(1)
			continue
		}
		reported := s.gaps.take()
		env := record.Envelope{
			Stream:           record.StreamCycle,
			Seq:              item.seq,
			Epoch:            s.epoch,
			Kind:             record.KindCycle,
			EnqueuedAt:       item.enqueuedAt,
			Record:           raw,
			DroppedSinceLast: reported,
		}
		if !started {
			env.EpochStart = true
		}
		conn, err = writeEnvelope(conn, s.address, &env)
		if err != nil {
			s.gaps.add(reported)
			s.drop(1)
			if conn != nil {
				_ = conn.Close()
				conn = nil
			}
			continue
		}
		started = true
		s.sent.Inc()
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

type gapCount struct {
	mu sync.Mutex
	n  uint64
}

func (g *gapCount) add(n uint64) {
	g.mu.Lock()
	g.n += n
	g.mu.Unlock()
}

func (g *gapCount) take() uint64 {
	g.mu.Lock()
	n := g.n
	g.n = 0
	g.mu.Unlock()
	return n
}

func estimateBytes(record *cycleRecord) int64 {
	if record == nil || record.RawObjects == nil {
		return 1024
	}
	objects := record.RawObjects
	return int64(1024 + len(objects.Pods)*800 + len(objects.Nodes)*1500 + len(objects.PodGroups)*400)
}
