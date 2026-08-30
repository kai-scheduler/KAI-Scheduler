// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type orderRecordingPlugin struct {
	name   string
	closed *[]string
}

func (p *orderRecordingPlugin) Name() string            { return p.name }
func (p *orderRecordingPlugin) OnSessionOpen(*Session)  {}
func (p *orderRecordingPlugin) OnSessionClose(*Session) { *p.closed = append(*p.closed, p.name) }

// TestCloseSessionReversesOpenOrder pins the teardown order: a plugin that opened after another is
// closed before it. Plugins use session state owned by plugins that opened earlier - the event
// handlers a statement operation triggers, for one - so closing in open order would tear that state
// down while it is still in use.
func TestCloseSessionReversesOpenOrder(t *testing.T) {
	var closed []string

	ssn := &Session{
		plugins:        map[string]Plugin{},
		pluginsInOrder: []Plugin{},
	}
	for _, name := range []string{"first", "second", "third"} {
		plugin := &orderRecordingPlugin{name: name, closed: &closed}
		ssn.plugins[name] = plugin
		ssn.pluginsInOrder = append(ssn.pluginsInOrder, plugin)
	}

	ssn.closePlugins()

	require.Equal(t, []string{"third", "second", "first"}, closed)
}
