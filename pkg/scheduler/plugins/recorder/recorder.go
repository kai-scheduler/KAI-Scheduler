// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"sync"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const Name = "recorder"

type enqueueFunc func(payload *cycleRecord) bool

type recorderPlugin struct {
	enqueue     enqueueFunc
	clusterName string
	shardName   string

	mu        sync.Mutex
	openedAt  time.Time
	decisions []framework.CommitEvent
}

func New(arguments framework.PluginArguments) framework.Plugin {
	address := ""
	if arguments != nil {
		address = arguments.GetString("address", "")
	}
	sender := newSender(senderConfig{Address: address})
	return newPlugin(arguments, sender.tryEnqueue)
}

func newPlugin(arguments framework.PluginArguments, enqueue enqueueFunc) *recorderPlugin {
	plugin := &recorderPlugin{enqueue: enqueue}
	if arguments != nil {
		plugin.clusterName = arguments.GetString("clusterName", "")
		plugin.shardName = arguments.GetString("shardName", "")
	}
	return plugin
}

func (p *recorderPlugin) Name() string {
	return Name
}

func (p *recorderPlugin) OnSessionOpen(ssn *framework.Session) {
	p.mu.Lock()
	p.openedAt = ssn.Now().UTC()
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
	if len(decisions) == 0 || p.enqueue == nil {
		return
	}
	cycle := buildCycle(ssn, decisions, openedAt)
	cycle.ClusterName = p.clusterName
	cycle.ShardName = p.shardName
	if !p.enqueue(cycle) {
		log.InfraLogger.V(2).Infof("Recorder dropped cycle %s", ssn.ID)
	}
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
