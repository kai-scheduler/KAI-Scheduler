// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gangcascadeorder

import (
	"sort"
	"strconv"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// buildVictimSet creates n same-priority victim gangs, alternating rigid (at min, spans 4
// nodes) and elastic (above min, spans 4 nodes) to exercise both branches of
// GetTasksToEvict. Node names come from a bounded pool so gangs realistically share nodes.
func buildVictimSet(n int) []*podgroup_info.PodGroupInfo {
	const pool = 64
	gangs := make([]*podgroup_info.PodGroupInfo, 0, n)
	for i := 0; i < n; i++ {
		nodes := []string{
			"node-" + strconv.Itoa((i*4+0)%pool),
			"node-" + strconv.Itoa((i*4+1)%pool),
			"node-" + strconv.Itoa((i*4+2)%pool),
			"node-" + strconv.Itoa((i*4+3)%pool),
		}
		minAvail := int32(4) // rigid: at minimum
		if i%2 == 1 {
			minAvail = 1 // elastic: 3 surplus pods
		}
		gangs = append(gangs, makeGangWithMin("g"+strconv.Itoa(i), 10, nodes, minAvail))
	}
	return gangs
}

// baselineLess models today's victim ordering cost when the plugin is absent: an O(1)
// creation/UID style tiebreak.
func baselineLess(a, c *podgroup_info.PodGroupInfo) bool {
	return a.UID < c.UID
}

func BenchmarkVictimOrdering(b *testing.B) {
	ssn := &framework.Session{}
	p := &gangCascadeOrderPlugin{ssn: ssn}
	pluginLess := func(a, c *podgroup_info.PodGroupInfo) bool {
		if v := p.VictimOrderFn(a, c); v != 0 {
			return v < 0
		}
		return a.UID < c.UID
	}

	sortPass := func(b *testing.B, base []*podgroup_info.PodGroupInfo, resetCache bool, less func(a, c *podgroup_info.PodGroupInfo) bool) {
		n := len(base)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if resetCache {
				// Model one reclaim solve: order from a cold cache that warms as the pass
				// runs (each gang costed once, reused across comparisons).
				p.cache = map[footprintKey]footprint{}
			}
			work := make([]*podgroup_info.PodGroupInfo, n)
			copy(work, base)
			sort.SliceStable(work, func(x, y int) bool { return less(work[x], work[y]) })
		}
	}

	for _, n := range []int{2, 4, 8, 16, 32, 64, 128, 256, 512, 1024} {
		base := buildVictimSet(n)
		b.Run("baseline/N="+strconv.Itoa(n), func(b *testing.B) {
			p.cache = nil
			sortPass(b, base, false, baselineLess)
		})
		b.Run("plugin_nocache/N="+strconv.Itoa(n), func(b *testing.B) {
			p.cache = nil
			sortPass(b, base, false, pluginLess)
		})
		b.Run("plugin_cached/N="+strconv.Itoa(n), func(b *testing.B) {
			sortPass(b, base, true, pluginLess)
		})
	}
}
