// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"sync"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/clock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	// defaultStateInterval is how long a saved cluster-state copy is reused for later cycles
	// whose state has not changed. It matches the sidecar roll interval so a new file is likely
	// to contain a copy of its own.
	defaultStateInterval = 5 * time.Minute
)

const Name = "recorder"

type enqueueFunc func(payload *cycleRecord) bool

type recorderPlugin struct {
	enqueue       enqueueFunc
	clusterName   string
	shardName     string
	stateInterval time.Duration

	mu              sync.Mutex
	openedAt        time.Time
	decisions       []framework.CommitEvent
	lastStateHash   string
	lastStateAt     time.Time
	lastStateQueued bool
}

var (
	processSenderOnce sync.Once
	processSender     *Sender
)

func New(arguments framework.PluginArguments) framework.Plugin {
	address := ""
	if arguments != nil {
		address = arguments.GetString("address", "")
	}
	processSenderOnce.Do(func() {
		processSender = newSender(senderConfig{Address: address})
	})
	return newPlugin(arguments, processSender.tryEnqueue)
}

func newPlugin(arguments framework.PluginArguments, enqueue enqueueFunc) *recorderPlugin {
	plugin := &recorderPlugin{enqueue: enqueue, stateInterval: defaultStateInterval}
	if arguments != nil {
		plugin.clusterName = arguments.GetString("clusterName", "")
		plugin.shardName = arguments.GetString("shardName", "")
		if interval, err := arguments.GetDuration("stateInterval", defaultStateInterval); err == nil {
			plugin.stateInterval = interval
		}
	}
	return plugin
}

func (p *recorderPlugin) Name() string {
	return Name
}

func (p *recorderPlugin) OnSessionOpen(ssn *framework.Session) {
	p.mu.Lock()
	p.openedAt = clock.Now().UTC()
	p.decisions = nil
	p.mu.Unlock()
	ssn.AddEventHandler(&framework.EventHandler{OnCommit: p.onCommit})
}

func (p *recorderPlugin) OnSessionClose(ssn *framework.Session) {
	p.mu.Lock()
	decisions := p.decisions
	openedAt := p.openedAt
	p.decisions = nil
	p.mu.Unlock()
	if p.enqueue == nil {
		return
	}
	cycle := buildCycle(ssn, decisions, openedAt)
	cycle.ClusterName = p.clusterName
	cycle.ShardName = p.shardName
	p.mu.Lock()
	embedded := p.retainState(cycle)
	p.mu.Unlock()
	queued := p.enqueue(cycle)
	if embedded {
		p.mu.Lock()
		p.lastStateQueued = queued
		if queued {
			p.lastStateHash = cycle.StateID
			p.lastStateAt = clock.Now()
		}
		p.mu.Unlock()
	}
	if !queued {
		log.InfraLogger.V(2).Infof("Recorder dropped cycle %s", ssn.ID)
	}
}

// retainState keeps the cluster-state body when it changed, when the previous copy was not queued,
// or when the save interval has elapsed. Otherwise the cycle only keeps StateID.
// The caller holds p.mu.
func (p *recorderPlugin) retainState(cycle *cycleRecord) bool {
	if cycle == nil || cycle.RawObjects == nil {
		return false
	}
	hash := stateHash(cycle)
	if hash == "" {
		return true
	}
	cycle.StateID = hash
	reuse := hash == p.lastStateHash && p.lastStateQueued && !p.lastStateAt.IsZero() &&
		clock.Since(p.lastStateAt) < p.stateInterval
	if !reuse {
		return true
	}
	cycle.Config = nil
	cycle.SchedulerParams = nil
	cycle.RawObjects = nil
	cycle.Usage = nil
	return false
}

func (p *recorderPlugin) onCommit(event *framework.CommitEvent) {
	if event == nil || len(event.Ops) == 0 {
		return
	}
	copied := *event
	copied.Ops = append([]framework.CommitOp(nil), event.Ops...)
	p.mu.Lock()
	p.decisions = append(p.decisions, copied)
	p.mu.Unlock()
}
