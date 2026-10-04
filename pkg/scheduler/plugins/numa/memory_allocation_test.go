// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

func TestMemoryHintErrorDiagnostics(t *testing.T) {
	topology := numaTopology(node_info.TopologyPolicyBestEffort, node_info.TopologyScopeContainer,
		numaZone("node-0", map[string]string{"memory": "100Gi", "hugepages-2Mi": "2Mi"}),
		numaZone("node-1", map[string]string{"memory": "100Gi", "hugepages-2Mi": "2Mi"}),
		numaZone("node-2", map[string]string{"memory": "100Gi", "hugepages-2Mi": "2Mi"}))
	groups := &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown}
	singleton, err := pod_info.NewNUMAMask([]int{0})
	require.NoError(t, err)
	wide, err := pod_info.NewNUMAMask([]int{1, 2})
	require.NoError(t, err)
	require.NoError(t, applyTestMemoryOwner(groups, topology, "pinned", []pod_info.MemoryGroupPlacement{
		{Mask: wide, Amount: req("memory", "1Gi")},
		{Mask: singleton, Amount: req("memory", "1Gi")},
	}))
	affinity, err := bitmask.NewBitMask(0, 2)
	require.NoError(t, err)

	tests := []struct {
		name     string
		memory   string
		affinity bitmask.BitMask
		wantErr  error
		message  string
	}{
		{
			name:    "conflict without affinity",
			memory:  "250Gi",
			wantErr: errMemoryGroupConflict,
			message: "NUMA memory placement conflict: requested {hugepages-2Mi: 2Mi, memory: 250Gi} cannot fit without conflicting with existing memory groups on NUMA nodes [[0] [1 2]]",
		},
		{
			name:     "conflict with affinity",
			memory:   "250Gi",
			affinity: affinity,
			wantErr:  errMemoryGroupConflict,
			message:  "NUMA memory placement conflict: requested {hugepages-2Mi: 2Mi, memory: 250Gi} cannot fit without conflicting with existing memory groups on NUMA nodes [[0] [1 2]]. Memory placement must include NUMA nodes [0 2]",
		},
		{
			name:    "capacity without affinity",
			memory:  "301Gi",
			wantErr: errMemoryGroupCapacity,
			message: "Insufficient NUMA memory capacity for requested {hugepages-2Mi: 2Mi, memory: 301Gi}",
		},
		{
			name:     "capacity with affinity",
			memory:   "301Gi",
			affinity: affinity,
			wantErr:  errMemoryGroupCapacity,
			message:  "Insufficient NUMA memory capacity for requested {hugepages-2Mi: 2Mi, memory: 301Gi}. Memory placement must include NUMA nodes [0 2]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := req("memory", test.memory, "hugepages-2Mi", "2Mi")
			originalRequest := request.DeepCopy()
			for iteration := 0; iteration < 20; iteration++ {
				solver := &memorySolver{topology: topology, groups: groups.Clone()}
				err := solver.noMemoryHintError(request, test.affinity)
				require.ErrorIs(t, err, test.wantErr)
				require.EqualError(t, err, test.message)
				require.False(t, strings.HasSuffix(err.Error(), "."))
				fitErrors := &common_info.TasksFitErrors{}
				fitErrors.AddNodeError(err)
				require.Equal(t, common_info.ResourcesWereNotFoundMsg+": 1 "+test.message+".", fitErrors.Error())
			}
			require.Equal(t, originalRequest, request)
		})
	}
}

func TestMemorySolverReusesHintsUntilAllocationChanges(t *testing.T) {
	topology := twoZoneNode(node_info.TopologyPolicyBestEffort, req("memory", "100Gi"))
	solver := &memorySolver{topology: topology, groups: &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown}}
	first := solver.hintsFor(req("memory", "30Gi"))
	require.Same(t, first, solver.hintsFor(req("memory", "32212254720")))
	require.Len(t, first.hints, 3)
	require.NoError(t, solver.allocateMemory("application", 1, req("memory", "80Gi")))
	withApplication := solver.hintsFor(req("memory", "30Gi"))
	require.NotSame(t, first, withApplication)
	require.Len(t, withApplication.hints, 1)
	require.Equal(t, []int{1}, withApplication.hints[0].NUMANodeAffinity.GetBits())
}

func TestMemoryFitsAmountsUsesExactIntegerBytes(t *testing.T) {
	request := v1.ResourceList{v1.ResourceMemory: resource.MustParse("9007199254740993")}
	require.False(t, fitsMemoryAmounts(request, func(v1.ResourceName) int64 { return 9007199254740992 }))
	require.True(t, fitsMemoryAmounts(request, func(v1.ResourceName) int64 { return 9007199254740993 }))
}
