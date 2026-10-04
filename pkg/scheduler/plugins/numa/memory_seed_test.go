// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func memoryRecord(amount string, zones ...string) []schedulingv1alpha2.NUMAMemoryGroupPlacement {
	return []schedulingv1alpha2.NUMAMemoryGroupPlacement{{MemoryNodes: zones, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse(amount)}}}
}

func memoryAnnotation(t *testing.T, records []schedulingv1alpha2.NUMAMemoryGroupPlacement) string {
	t.Helper()
	encoded, err := json.Marshal(records)
	require.NoError(t, err)
	return string(encoded)
}

func memoryTestTask(name, amount string) *pod_info.PodInfo {
	task := makeGuaranteedTask(name, map[string]string{"memory": amount})
	task.Pod.Name = name
	task.Pod.Namespace = "ns"
	task.Pod.UID = types.UID(name)
	task.Status = pod_status.Running
	return task
}

func memoryTestPlugin() (*numaPlugin, *framework.Session, *node_info.NodeInfo) {
	return wiredPlugin(numaTopology(node_info.TopologyPolicyRestricted, node_info.TopologyScopeContainer,
		numaZone("node-0", map[string]string{"memory": "100Gi"}),
		numaZone("node-1", map[string]string{"memory": "100Gi"}),
	))
}

func TestMemoryGroupObservationPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		observed *string
		known    bool
		used     int64
	}{
		{name: "absent uses BindRequest", known: true, used: 120 << 30},
		{name: "observed replaces prediction", observed: stringPointer(memoryAnnotation(t, memoryRecord("140Gi", "node-0", "node-1"))), known: true, used: 140 << 30},
		{name: "complete empty replaces prediction", observed: stringPointer("[]"), known: true},
		{name: "explicit null overrides prediction", observed: stringPointer("null")},
		{name: "malformed overrides prediction", observed: stringPointer("not-json")},
		{name: "unknown zone overrides prediction", observed: stringPointer(memoryAnnotation(t, memoryRecord("120Gi", "node-9")))},
		{name: "over capacity overrides prediction", observed: stringPointer(memoryAnnotation(t, memoryRecord("201Gi", "node-0", "node-1")))},
		{name: "overflow overrides prediction", observed: stringPointer(memoryAnnotation(t, memoryRecord("18446744073709551616", "node-0", "node-1")))},
		{name: "fractional amount invalid", observed: stringPointer(memoryAnnotation(t, memoryRecord("0.5", "node-0")))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pp, session, node := memoryTestPlugin()
			owner := memoryTestTask("owner", "120Gi")
			owner.Pod.Annotations = map[string]string{commonconstants.NumaMemoryGroupsPredicted: memoryAnnotation(t, memoryRecord("130Gi", "node-0", "node-1"))}
			if test.observed != nil {
				owner.Pod.Annotations[commonconstants.NumaMemoryGroupsObserved] = *test.observed
			}
			node.PodInfos = pod_info.PodsMap{owner.UID: owner}
			session.ClusterInfo.BindRequests = bindrequest_info.BindRequestMap{
				bindrequest_info.NewKeyFromPod(owner.Pod): {BindRequest: &schedulingv1alpha2.BindRequest{Spec: schedulingv1alpha2.BindRequestSpec{PredictedNUMAMemoryGroups: memoryRecord("120Gi", "node-0", "node-1")}}},
			}
			pp.seedMemoryGroups(session)
			state := node.NumaTopology.MemoryGroups
			assert.Equal(t, test.known, state.Status == node_info.MemoryGroupsKnown)
			if test.used > 0 {
				assert.Equal(t, int64(200<<30)-test.used, state.Available(node.NumaTopology, 3, v1.ResourceMemory))
			} else {
				assert.Empty(t, state.Groups)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func TestDelayedBindMemoryGroupRestrictsNextPod(t *testing.T) {
	pp, session, node := memoryTestPlugin()
	pp.seedMemoryGroups(session)
	first := memoryTestTask("first", "120Gi")
	placement, err := pp.placement(first, node)
	require.NoError(t, err)
	require.Len(t, placement.MemoryGroups, 1)
	assert.Equal(t, pod_info.NUMAMask(3), placement.MemoryGroups[0].Mask)
	first.NUMAPlacement = placement
	pp.allocate(&framework.Event{Task: first})
	second := memoryTestTask("second", "40Gi")
	assert.Error(t, pp.predicate(second, nil, node))
	pp.deallocate(&framework.Event{Task: first})
	assert.NoError(t, pp.predicate(second, nil, node))

	first.Status = pod_status.Binding
	first.Pod.Spec.NodeName = ""
	node.PodInfos = pod_info.PodsMap{first.UID: first}
	session.ClusterInfo.BindRequests = bindrequest_info.BindRequestMap{
		bindrequest_info.NewKeyFromPod(first.Pod): {BindRequest: &schedulingv1alpha2.BindRequest{Spec: schedulingv1alpha2.BindRequestSpec{PredictedNUMAMemoryGroups: memoryRecord("120Gi", "node-0", "node-1")}}},
	}
	pp.seedMemoryGroups(session)
	assert.Error(t, pp.predicate(second, nil, node), "a delayed bind remains an owner in the next cycle")
}

func TestMemoryOwnersRetainedAndLastOwnerRemoved(t *testing.T) {
	for _, status := range []pod_status.PodStatus{pod_status.Running, pod_status.Releasing, pod_status.StuckInReleasing} {
		t.Run(status.String(), func(t *testing.T) {
			pp, session, node := memoryTestPlugin()
			first := memoryTestTask("first", "120Gi")
			second := memoryTestTask("second", "30Gi")
			first.Status = status
			for _, task := range []*pod_info.PodInfo{first, second} {
				amount := task.Pod.Spec.Containers[0].Resources.Requests.Memory().String()
				task.Pod.Annotations = map[string]string{commonconstants.NumaMemoryGroupsObserved: memoryAnnotation(t, memoryRecord(amount, "node-0", "node-1"))}
			}
			node.PodInfos = pod_info.PodsMap{first.UID: first, second.UID: second}
			pp.seedMemoryGroups(session)
			state := node.NumaTopology.MemoryGroups
			assert.Equal(t, int64(50<<30), state.Available(node.NumaTopology, 3, v1.ResourceMemory))
			pp.deallocate(&framework.Event{Task: first})
			assert.Equal(t, int64(170<<30), state.Available(node.NumaTopology, 3, v1.ResourceMemory))
			pp.allocate(&framework.Event{Task: first})
			assert.Equal(t, int64(50<<30), state.Available(node.NumaTopology, 3, v1.ResourceMemory))
			pp.deallocate(&framework.Event{Task: first})
			pp.deallocate(&framework.Event{Task: second})
			assert.Empty(t, state.Groups)
		})
	}
}

func TestSeedMemoryPlacementRestoresCanonicalGroups(t *testing.T) {
	plugin, session, node := memoryTestPlugin()
	owner := memoryTestTask("owner", "20Gi")
	owner.NodeName = node.Name
	owner.Pod.Spec.InitContainers = []v1.Container{{Name: "init"}}
	owner.Pod.Annotations = map[string]string{commonconstants.NumaMemoryGroupsPredicted: memoryAnnotation(t, memoryRecord("20Gi", "node-0"))}
	clone := owner.Clone()
	node.PodInfos = pod_info.PodsMap{clone.UID: clone}
	session.ClusterInfo.PodGroupInfos = map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
		"job": podgroup_info.NewPodGroupInfo("job", owner),
	}
	plugin.seedMemoryGroups(session)
	require.Len(t, owner.NUMAPlacement.MemoryGroups, 1)
	require.True(t, clone.NUMAPlacement.Equal(owner.NUMAPlacement))
	require.NoError(t, plugin.predicate(memoryTestTask("next", "1Gi"), nil, node))
	before := node.NumaTopology.MemoryGroups.Clone()
	plugin.deallocate(&framework.Event{Task: owner})
	require.Empty(t, node.NumaTopology.MemoryGroups.Groups)
	plugin.allocate(&framework.Event{Task: owner})
	require.Equal(t, before, node.NumaTopology.MemoryGroups)
}

func TestSeedMemoryPlacementIgnoresIneligibleCanonicalPod(t *testing.T) {
	plugin, session, node := memoryTestPlugin()
	owner := memoryTestTask("owner", "20Gi")
	owner.NodeName = node.Name
	owner.Pod.Status.QOSClass = v1.PodQOSBurstable
	owner.Pod.Annotations = map[string]string{commonconstants.NumaMemoryGroupsObserved: memoryAnnotation(t, memoryRecord("20Gi", "node-0"))}
	owner.NUMAPlacement.MemoryGroups = []pod_info.MemoryGroupPlacement{{Mask: 1, Amount: req("memory", "20Gi")}}
	session.ClusterInfo.PodGroupInfos = map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
		"job": podgroup_info.NewPodGroupInfo("job", owner),
	}
	plugin.seedMemoryGroups(session)
	require.Empty(t, owner.NUMAPlacement.MemoryGroups)
}
