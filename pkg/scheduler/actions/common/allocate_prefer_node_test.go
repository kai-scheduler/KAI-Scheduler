// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
)

func TestPreferNodeFirst(t *testing.T) {
	a := &node_info.NodeInfo{Name: "a"}
	b := &node_info.NodeInfo{Name: "b"}
	c := &node_info.NodeInfo{Name: "c"}

	t.Run("moves named node to front", func(t *testing.T) {
		got := preferNodeFirst([]*node_info.NodeInfo{a, b, c}, "b")
		require.Equal(t, []*node_info.NodeInfo{b, a, c}, got)
	})

	t.Run("keeps relative order of other nodes", func(t *testing.T) {
		got := preferNodeFirst([]*node_info.NodeInfo{c, a, b}, "a")
		require.Equal(t, []*node_info.NodeInfo{a, c, b}, got)
	})

	t.Run("no-op when name missing", func(t *testing.T) {
		in := []*node_info.NodeInfo{a, b, c}
		got := preferNodeFirst(in, "missing")
		require.Equal(t, in, got)
	})

	t.Run("no-op when name empty", func(t *testing.T) {
		in := []*node_info.NodeInfo{a, b}
		got := preferNodeFirst(in, "")
		require.Equal(t, in, got)
	})
}
