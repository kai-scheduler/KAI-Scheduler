// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/predicates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	kaifake "github.com/kai-scheduler/api/client/clientset/versioned/fake"
	commonconstants "github.com/kai-scheduler/api/constants"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestReplayAllocateMatchesRecordedDecision(t *testing.T) {
	actions.InitDefaultActions()
	framework.RegisterPluginBuilder("predicates", predicates.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)

	config, params := replayConfig()
	first, events := runAllocate(t, config, params, replayObjects())
	require.NotEmpty(t, events, "allocate should commit a decision")

	var state recorder.StateRecord
	var found bool
	for _, event := range events {
		if event.Kind != recorder.KindState {
			continue
		}
		require.NoError(t, json.Unmarshal(event.Record, &state))
		found = true
	}
	require.True(t, found, "a pending cycle should record its state copy")
	require.NotNil(t, state.RawObjects)

	second, _ := runAllocate(t, config, params, state.RawObjects)
	require.Equal(t, decisionsByAction(first), decisionsByAction(second))
}

func TestIdleCycleIsNotRecorded(t *testing.T) {
	var events []recorder.Envelope
	plugin := newPlugin(nil, func(_, _ string, _ any) bool {
		events = append(events, recorder.Envelope{})
		return true
	})
	session := &framework.Session{ID: "idle", ClusterInfo: &api.ClusterInfo{}}
	plugin.OnSessionOpen(session)
	plugin.OnSessionClose(session)
	require.Empty(t, events)
}

func runAllocate(t *testing.T, config *conf.SchedulerConfiguration, params *conf.SchedulerParams, objects *snapshot.RawKubernetesObjects) ([]framework.CommitOp, []recorder.Envelope) {
	t.Helper()
	var mu sync.Mutex
	var events []recorder.Envelope
	var ops []framework.CommitOp
	plugin := newPlugin(nil, func(stream, kind string, payload any) bool {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		mu.Lock()
		events = append(events, recorder.Envelope{Stream: stream, Kind: kind, Record: raw})
		mu.Unlock()
		return true
	})

	schedCache := startReplayCache(t, params, objects)
	session, err := framework.OpenSession(schedCache, config, params, "replay", http.NewServeMux())
	require.NoError(t, err)
	plugin.OnSessionOpen(session)
	session.AddEventHandler(&framework.EventHandler{OnCommit: func(event *framework.CommitEvent) {
		if event.DiscardedOpCount > 0 && len(event.Ops) == 0 {
			return
		}
		for _, op := range event.Ops {
			if op.NotAttempted {
				continue
			}
			ops = append(ops, op)
		}
	}})
	session.SetCurrentAction(string(framework.Allocate))
	allocate.New().Execute(session)
	plugin.OnSessionClose(session)
	framework.CloseSession(session)
	return ops, events
}

func decisionsByAction(ops []framework.CommitOp) map[string][]string {
	out := map[string][]string{}
	for _, op := range ops {
		out[op.Kind] = append(out[op.Kind], op.Namespace+"/"+op.Name+"@"+op.Node)
	}
	return out
}

func replayConfig() (*conf.SchedulerConfiguration, *conf.SchedulerParams) {
	return &conf.SchedulerConfiguration{
			Actions: "allocate",
			Tiers: []conf.Tier{{Plugins: []conf.PluginOption{
				{Name: "predicates"},
				{Name: "proportion"},
			}}},
		}, &conf.SchedulerParams{
			SchedulerName:   "kai-scheduler",
			PartitionParams: &conf.SchedulingNodePoolParams{},
		}
}

func replayObjects() *snapshot.RawKubernetesObjects {
	return &snapshot.RawKubernetesObjects{
		Nodes: []*v1.Node{{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("8Gi"),
					v1.ResourcePods:   resource.MustParse("10"),
				},
				Conditions: []v1.NodeCondition{{Type: v1.NodeReady, Status: v1.ConditionTrue}},
			},
		}},
		Queues: []*enginev2.Queue{{
			ObjectMeta: metav1.ObjectMeta{Name: "team-a"},
			Spec: enginev2.QueueSpec{Resources: &enginev2.QueueResources{
				CPU:    enginev2.QueueResource{Quota: 4, Limit: 4, OverQuotaWeight: 1},
				Memory: enginev2.QueueResource{Quota: 8, Limit: 8, OverQuotaWeight: 1},
			}},
		}},
		PodGroups: []*enginev2alpha2.PodGroup{{
			ObjectMeta: metav1.ObjectMeta{Name: "job-a", Namespace: "team-a"},
			Spec: enginev2alpha2.PodGroupSpec{
				Queue:     "team-a",
				MinMember: ptr.To(int32(1)),
			},
		}},
		Pods: []*v1.Pod{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job-a-0",
				Namespace: "team-a",
				UID:       "job-a-0",
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod: "job-a",
				},
			},
			Spec: v1.PodSpec{
				SchedulerName: "kai-scheduler",
				Containers: []v1.Container{{
					Name: "worker",
					Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1"),
						v1.ResourceMemory: resource.MustParse("1Gi"),
					}},
				}},
			},
			Status: v1.PodStatus{Phase: v1.PodPending},
		}},
	}
}

func startReplayCache(t *testing.T, params *conf.SchedulerParams, objects *snapshot.RawKubernetesObjects) cache.Cache {
	t.Helper()
	kubeClient := fake.NewSimpleClientset()
	kaiClient := kaifake.NewSimpleClientset()
	for _, node := range objects.Nodes {
		_, err := kubeClient.CoreV1().Nodes().Create(context.Background(), node.DeepCopy(), metav1.CreateOptions{})
		require.NoError(t, err)
	}
	for _, pod := range objects.Pods {
		_, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod.DeepCopy(), metav1.CreateOptions{})
		require.NoError(t, err)
	}
	for _, queue := range objects.Queues {
		_, err := kaiClient.SchedulingV2().Queues(queue.Namespace).Create(context.Background(), queue.DeepCopy(), metav1.CreateOptions{})
		require.NoError(t, err)
	}
	for _, group := range objects.PodGroups {
		_, err := kaiClient.SchedulingV2alpha2().PodGroups(group.Namespace).Create(context.Background(), group.DeepCopy(), metav1.CreateOptions{})
		require.NoError(t, err)
	}
	schedCache, err := cache.New(&cache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kaiClient,
		SchedulerName:               params.SchedulerName,
		NodePoolParams:              params.PartitionParams,
		NumOfStatusRecordingWorkers: 1,
		DiscoveryClient:             kubeClient.Discovery(),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	schedCache.Run(ctx.Done())
	schedCache.WaitForCacheSync(ctx.Done())
	return schedCache
}
