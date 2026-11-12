// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/multinodegang"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/nodelocalgreedy"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/predicates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/priority"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	commonconstants "github.com/kai-scheduler/api/constants"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestReplayReclaimPreemptAndConsolidation(t *testing.T) {
	actions.InitDefaultActions()
	framework.RegisterPluginBuilder("predicates", predicates.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)
	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder(nodelocalgreedy.Name, nodelocalgreedy.New)
	framework.RegisterPluginBuilder(multinodegang.Name, multinodegang.New)

	cases := []struct {
		name     string
		action   framework.Action
		objects  *snapshot.RawKubernetesObjects
		params   *conf.SchedulerParams
		expected []string
	}{
		{
			name: "reclaim", action: reclaim.New(), objects: reclaimObjects(), params: actionParams(false),
			expected: []string{
				"reclaim evict team/victim-0@node-a",
				"reclaim pipeline team/pending-0@node-a",
			},
		},
		{
			name: "preempt", action: preempt.New(), objects: preemptObjects(), params: actionParams(false),
			expected: []string{
				"preempt evict team/victim-0@node-a",
				"preempt pipeline team/pending-0@node-a",
			},
		},
		{
			name: "consolidation", action: consolidation.New(), objects: consolidationObjects(), params: actionParams(true),
			expected: []string{
				"consolidation evict team/spread-b-0@node-b",
				"consolidation pipeline team/pending-0@node-b",
				"consolidation pipeline team/spread-b-0@node-a",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := actionConfig(string(tc.action.Name()))
			recorded, state := runRecordedAction(t, config, tc.params, tc.objects, tc.action)
			require.Equal(t, tc.expected, recorded)
			require.NotNil(t, state)
			require.NotNil(t, state.RawObjects)
			replayed, _ := runRecordedAction(t, config, tc.params, state.RawObjects, tc.action)
			require.Equal(t, recorded, replayed)
		})
	}
}

func TestReplayRedactedPartitionedPreempt(t *testing.T) {
	actions.InitDefaultActions()
	framework.RegisterPluginBuilder("predicates", predicates.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)
	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder(nodelocalgreedy.Name, nodelocalgreedy.New)
	framework.RegisterPluginBuilder(multinodegang.Name, multinodegang.New)

	const poolKey = "kai.scheduler/node-pool"
	const poolValue = "pool-a"
	objects := preemptObjects()
	for _, node := range objects.Nodes {
		node.Labels = map[string]string{poolKey: poolValue}
	}
	for _, queue := range objects.Queues {
		queue.Labels = map[string]string{poolKey: poolValue}
	}
	for _, group := range objects.PodGroups {
		group.Labels = map[string]string{poolKey: poolValue}
	}
	params := actionParams(false)
	params.PartitionParams = &conf.SchedulingNodePoolParams{
		NodePoolLabelKey:   poolKey,
		NodePoolLabelValue: poolValue,
	}
	config := actionConfig(string(framework.Preempt))
	recorded, state := runRecordedAction(t, config, params, objects, preempt.New())
	require.Equal(t, []string{
		"preempt evict team/victim-0@node-a",
		"preempt pipeline team/pending-0@node-a",
	}, recorded)

	raw, err := json.Marshal(state)
	require.NoError(t, err)
	env := &recorder.Envelope{Kind: recorder.KindCycle, Record: raw}
	hasher := recorder.NewHasher([]byte("replay-key"))
	require.NoError(t, recorder.RedactEnvelope(env, hasher))
	var redacted cycleRecord
	require.NoError(t, json.Unmarshal(env.Record, &redacted))
	require.Equal(t, hasher.Hash(poolValue), redacted.SchedulerParams.PartitionParams.NodePoolLabelValue)

	replayed, _ := runRecordedAction(t, config, redacted.SchedulerParams, redacted.RawObjects, preempt.New())
	require.Equal(t, []string{
		"preempt evict " + hasher.Hash("team") + "/" + hasher.Hash("victim-0") + "@" + hasher.Hash("node-a"),
		"preempt pipeline " + hasher.Hash("team") + "/" + hasher.Hash("pending-0") + "@" + hasher.Hash("node-a"),
	}, replayed)
}

func runRecordedAction(
	t *testing.T, config *conf.SchedulerConfiguration, params *conf.SchedulerParams,
	objects *snapshot.RawKubernetesObjects, action framework.Action,
) ([]string, *cycleRecord) {
	t.Helper()
	var recorded *cycleRecord
	var decisions []string
	plugin := newPlugin(nil, func(payload *cycleRecord) bool {
		recorded = payload
		return true
	})
	schedCache := startReplayCache(t, params, objects)
	session, err := framework.OpenSession(schedCache, config, params, "replay-"+string(action.Name()), http.NewServeMux())
	require.NoError(t, err)
	plugin.OnSessionOpen(session)
	session.AddEventHandler(&framework.EventHandler{OnCommit: func(event *framework.CommitEvent) {
		for _, op := range event.Ops {
			if op.NotAttempted {
				continue
			}
			decisions = append(decisions, event.Action+" "+op.Kind+" "+op.Namespace+"/"+op.Name+"@"+op.Node)
		}
	}})
	session.SetCurrentAction(string(action.Name()))
	action.Execute(session)
	plugin.OnSessionClose(session)
	framework.CloseSession(session)
	sort.Strings(decisions)
	return decisions, recorded
}

func actionConfig(action string) *conf.SchedulerConfiguration {
	return &conf.SchedulerConfiguration{
		Actions: action,
		Tiers: []conf.Tier{{Plugins: []conf.PluginOption{
			{Name: "predicates"},
			{Name: "proportion"},
			{Name: "priority"},
			{Name: nodelocalgreedy.Name},
			{Name: multinodegang.Name},
		}}},
	}
}

func actionParams(consolidation bool) *conf.SchedulerParams {
	params := &conf.SchedulerParams{
		SchedulerName:   "kai-scheduler",
		PartitionParams: &conf.SchedulingNodePoolParams{},
	}
	if consolidation {
		params.MaxNumberConsolidationPreemptees = -1
	}
	return params
}

func reclaimObjects() *snapshot.RawKubernetesObjects {
	return &snapshot.RawKubernetesObjects{
		Nodes: []*v1.Node{cpuNode("node-a", "1")},
		Queues: []*enginev2.Queue{
			quotaQueue("q-victim", 0, 0),
			quotaQueue("q-pending", 2000, 1),
		},
		PodGroups: []*enginev2alpha2.PodGroup{
			podGroup("team", "victim", "q-victim", ""),
			podGroup("team", "pending", "q-pending", ""),
		},
		Pods: []*v1.Pod{
			workloadPod("team", "victim-0", "victim", "node-a", "800m", v1.PodRunning),
			workloadPod("team", "pending-0", "pending", "", "500m", v1.PodPending),
		},
	}
}

func preemptObjects() *snapshot.RawKubernetesObjects {
	return &snapshot.RawKubernetesObjects{
		Nodes:  []*v1.Node{cpuNode("node-a", "1")},
		Queues: []*enginev2.Queue{quotaQueue("q-shared", 4000, 1)},
		PodGroups: []*enginev2alpha2.PodGroup{
			podGroup("team", "victim", "q-shared", "low"),
			podGroup("team", "pending", "q-shared", "high"),
		},
		Pods: []*v1.Pod{
			workloadPod("team", "victim-0", "victim", "node-a", "800m", v1.PodRunning),
			workloadPod("team", "pending-0", "pending", "", "500m", v1.PodPending),
		},
		PriorityClasses: []*schedulingv1.PriorityClass{
			{ObjectMeta: metav1.ObjectMeta{Name: "low"}, Value: 50},
			{ObjectMeta: metav1.ObjectMeta{Name: "high"}, Value: 80},
		},
	}
}

func consolidationObjects() *snapshot.RawKubernetesObjects {
	return &snapshot.RawKubernetesObjects{
		Nodes:  []*v1.Node{cpuNode("node-a", "2"), cpuNode("node-b", "2")},
		Queues: []*enginev2.Queue{quotaQueue("q-shared", 8000, 1)},
		PodGroups: []*enginev2alpha2.PodGroup{
			podGroup("team", "spread-a", "q-shared", ""),
			podGroup("team", "spread-b", "q-shared", ""),
			podGroup("team", "pending", "q-shared", ""),
		},
		Pods: []*v1.Pod{
			workloadPod("team", "spread-a-0", "spread-a", "node-a", "1500m", v1.PodRunning),
			workloadPod("team", "spread-b-0", "spread-b", "node-b", "500m", v1.PodRunning),
			workloadPod("team", "pending-0", "pending", "", "2", v1.PodPending),
		},
	}
}

func cpuNode(name, cpu string) *v1.Node {
	cpuQty := resource.MustParse(cpu)
	mem := resource.MustParse("8Gi")
	pods := resource.MustParse("10")
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1.NodeStatus{
			Allocatable: v1.ResourceList{v1.ResourceCPU: cpuQty, v1.ResourceMemory: mem, v1.ResourcePods: pods},
			Capacity:    v1.ResourceList{v1.ResourceCPU: cpuQty, v1.ResourceMemory: mem, v1.ResourcePods: pods},
			Conditions:  []v1.NodeCondition{{Type: v1.NodeReady, Status: v1.ConditionTrue}},
		},
	}
}

func quotaQueue(name string, cpuQuota, weight float64) *enginev2.Queue {
	return &enginev2.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)},
		Spec: enginev2.QueueSpec{
			ParentQueue: "department",
			Resources: &enginev2.QueueResources{
				CPU:    enginev2.QueueResource{Quota: cpuQuota, Limit: -1, OverQuotaWeight: weight},
				Memory: enginev2.QueueResource{Quota: -1, Limit: -1, OverQuotaWeight: weight},
				GPU:    enginev2.QueueResource{Quota: -1, Limit: -1, OverQuotaWeight: weight},
			},
		},
	}
}

func podGroup(namespace, name, queue, priorityClass string) *enginev2alpha2.PodGroup {
	return &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID(namespace + "/" + name)},
		Spec: enginev2alpha2.PodGroupSpec{
			Queue:             queue,
			MinMember:         ptr.To(int32(1)),
			PriorityClassName: priorityClass,
		},
	}
}

func workloadPod(namespace, name, group, node, cpu string, phase v1.PodPhase) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(namespace + "/" + name),
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: group,
			},
		},
		Spec: v1.PodSpec{
			SchedulerName: "kai-scheduler",
			NodeName:      node,
			Containers: []v1.Container{{
				Name: "main",
				Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse(cpu),
					v1.ResourceMemory: resource.MustParse("100Mi"),
				}},
			}},
		},
		Status: v1.PodStatus{Phase: phase},
	}
}
