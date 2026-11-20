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
	reuse         *reuseState

	mu        sync.Mutex
	openedAt  time.Time
	decisions []framework.CommitEvent
}

// reuseState is the last cluster-state copy saved for one recording stream.
// New sessions in the same process share it, keyed by cluster and shard.
type reuseState struct {
	mu              sync.Mutex
	lastStateHash   string
	lastStateAt     time.Time
	lastStateQueued bool
}

var (
	processSenderOnce sync.Once
	processSender     *Sender
	processReuse      sync.Map
)

func New(arguments framework.PluginArguments) framework.Plugin {
	address := ""
	if arguments != nil {
		address = arguments.GetString("address", "")
	}
	processSenderOnce.Do(func() {
		processSender = newSender(senderConfig{Address: address})
	})
	plugin := newPlugin(arguments, processSender.tryEnqueue)
	plugin.reuse = streamReuse(arguments)
	return plugin
}

func streamKey(arguments framework.PluginArguments) string {
	if arguments == nil {
		return ""
	}
	return arguments.GetString("clusterName", "") + "\x00" + arguments.GetString("shardName", "")
}

func streamReuse(arguments framework.PluginArguments) *reuseState {
	key := streamKey(arguments)
	if existing, ok := processReuse.Load(key); ok {
		return existing.(*reuseState)
	}
	created := &reuseState{}
	actual, _ := processReuse.LoadOrStore(key, created)
	return actual.(*reuseState)
}

func newPlugin(arguments framework.PluginArguments, enqueue enqueueFunc) *recorderPlugin {
	plugin := &recorderPlugin{
		enqueue:       enqueue,
		stateInterval: defaultStateInterval,
		reuse:         &reuseState{},
	}
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
	embedded := p.retainState(cycle)
	queued := p.enqueue(cycle)
	if embedded {
		p.noteSavedState(cycle, queued)
	}
	if !queued {
		log.InfraLogger.V(2).Infof("Recorder dropped cycle %s", ssn.ID)
	}
}

// retainState keeps the cluster-state body when it changed, when the previous copy was not queued,
// or when the save interval has elapsed. Otherwise the cycle only keeps StateID.
func (p *recorderPlugin) retainState(cycle *cycleRecord) bool {
	if cycle == nil || cycle.RawObjects == nil || p.reuse == nil {
		return false
	}
	hash := stateHash(cycle)
	if hash == "" {
		return true
	}
	cycle.StateID = hash
	p.reuse.mu.Lock()
	defer p.reuse.mu.Unlock()
	reuse := hash == p.reuse.lastStateHash && p.reuse.lastStateQueued && !p.reuse.lastStateAt.IsZero() &&
		clock.Since(p.reuse.lastStateAt) < p.stateInterval
	if !reuse {
		return true
	}
	cycle.Config = nil
	cycle.SchedulerParams = nil
	cycle.RawObjects = nil
	cycle.Usage = nil
	return false
}

func (p *recorderPlugin) noteSavedState(cycle *cycleRecord, queued bool) {
	if p.reuse == nil || cycle == nil || cycle.StateID == "" {
		return
	}
	p.reuse.mu.Lock()
	p.reuse.lastStateQueued = queued
	if queued {
		p.reuse.lastStateHash = cycle.StateID
		p.reuse.lastStateAt = clock.Now()
	}
	p.reuse.mu.Unlock()
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
