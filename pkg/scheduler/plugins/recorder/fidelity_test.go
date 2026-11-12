// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/stalegangeviction"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/minruntime"
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

func TestReplayKeepsPodFieldsAndQueueAge(t *testing.T) {
	registerReplayPlugins(t)
	opened := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	params := actionParams(false)
	params.StuckInReleasingThreshold = time.Hour
	objects := fieldObjects(opened)
	config := actionConfig(string(framework.Allocate))

	first, state := runActionsAt(t, config, params, objects, nil, opened, []framework.Action{allocate.New()})
	require.Equal(t, []string{"allocate allocate team/ready-0@node-a"}, first)
	require.NotContains(t, first, "gated-0")
	require.NotContains(t, first, "ported-0")
	require.NotNil(t, state)

	body := string(mustJSON(t, state))
	require.Contains(t, body, `"hostPort":80`)
	require.Contains(t, body, "schedulingGates")
	require.Contains(t, body, "configMapKeyRef")
	require.Contains(t, body, "resourceClaimName")
	require.Contains(t, body, "deletionTimestamp")
	var oldQueue, newQueue bool
	for _, queue := range state.RawObjects.Queues {
		switch queue.Name {
		case "q-old":
			require.True(t, queue.CreationTimestamp.Time.Equal(opened.Add(-2*time.Hour)))
			oldQueue = true
		case "q-new":
			require.True(t, queue.CreationTimestamp.Time.Equal(opened.Add(-time.Minute)))
			newQueue = true
		}
	}
	require.True(t, oldQueue && newQueue)

	replayed := replayRedacted(t, config, state, []framework.Action{allocate.New()})
	require.Equal(t, hashDecisions(first, recorder.NewHasher([]byte("fidelity-key"))), replayed)

	session := openAt(t, params, objects, nil, opened)
	require.Equal(t, pod_status.StuckInReleasing, taskStatus(session, "stuck-0"))
	require.Equal(t, pod_status.Releasing, taskStatus(session, "releasing-0"))
	framework.CloseSession(session)

	// The same deletion timestamps are years before the wall clock, so a live clock
	// would mark the recent one stuck as well.
	live := actionParams(false)
	live.StuckInReleasingThreshold = time.Hour
	session = openAt(t, live, objects, nil, time.Time{})
	require.Equal(t, pod_status.StuckInReleasing, taskStatus(session, "releasing-0"))
	framework.CloseSession(session)
}

func TestReplayFeedsUsageAndRecordedTime(t *testing.T) {
	registerReplayPlugins(t)
	framework.RegisterPluginBuilder("minruntime", minruntime.New)
	opened := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	config := actionConfig(string(framework.Preempt))
	config.Tiers[0].Plugins = append(config.Tiers[0].Plugins, conf.PluginOption{Name: "minruntime"})

	usageObjects := usageObjects(opened)
	usage := queue_info.NewClusterUsage()
	usage.Queues[common_info.QueueID("q-heavy")] = queue_info.QueueUsage{v1.ResourceCPU: 1}
	usage.Queues[common_info.QueueID("q-light")] = queue_info.QueueUsage{v1.ResourceCPU: 0}
	params := actionParams(false)

	withUsage, state := runActionsAt(t, actionConfig(string(framework.Allocate)), params, usageObjects, usage, opened, []framework.Action{allocate.New()})
	require.Equal(t, []string{"allocate allocate team/light-0@node-a"}, withUsage)
	require.NotNil(t, state)
	require.NotNil(t, state.Usage)
	replayed := replayRedacted(t, actionConfig(string(framework.Allocate)), state, []framework.Action{allocate.New()})
	require.Equal(t, hashDecisions(withUsage, recorder.NewHasher([]byte("fidelity-key"))), replayed)

	without, _ := runActionsAt(t, actionConfig(string(framework.Allocate)), params, usageObjects, nil, opened, []framework.Action{allocate.New()})
	require.Equal(t, []string{"allocate allocate team/heavy-0@node-a"}, without)

	delayObjects := preemptObjects()
	delayObjects.PodGroups[1].CreationTimestamp = metav1.NewTime(opened.Add(-time.Minute))
	delayObjects.PodGroups[1].Spec.PreemptionDelay = &metav1.Duration{Duration: 30 * time.Minute}
	held, _ := runActionsAt(t, config, params, delayObjects, nil, opened, []framework.Action{preempt.New()})
	require.Empty(t, held)
	released, delayState := runActionsAt(t, config, params, delayObjects, nil, opened.Add(time.Hour), []framework.Action{preempt.New()})
	require.NotEmpty(t, released)
	require.Equal(t, hashDecisions(released, recorder.NewHasher([]byte("fidelity-key"))), replayRedacted(t, config, delayState, []framework.Action{preempt.New()}))

	runtimeObjects := preemptObjects()
	runtimeObjects.Queues[0].Spec.PreemptMinRuntime = &metav1.Duration{Duration: time.Hour}
	runtimeObjects.PodGroups[0].Annotations = map[string]string{
		commonconstants.LastStartTimeStamp: opened.Add(-10 * time.Minute).Format(time.RFC3339),
	}
	protected, _ := runActionsAt(t, config, params, runtimeObjects, nil, opened, []framework.Action{preempt.New()})
	require.Empty(t, protected)
	expired, runtimeState := runActionsAt(t, config, params, runtimeObjects, nil, opened.Add(2*time.Hour), []framework.Action{preempt.New()})
	require.NotEmpty(t, expired)
	require.Contains(t, string(mustJSON(t, runtimeState)), commonconstants.LastStartTimeStamp)
	require.Equal(t, hashDecisions(expired, recorder.NewHasher([]byte("fidelity-key"))), replayRedacted(t, config, runtimeState, []framework.Action{preempt.New()}))

	staleObjects := staleGangObjects(opened)
	waiting, _ := runActionsAt(t, actionConfig(string(framework.StaleGangEviction)), params, staleObjects, nil, opened.Add(-2*time.Hour), []framework.Action{stalegangeviction.New()})
	require.Empty(t, waiting)
	evicted, staleState := runActionsAt(t, actionConfig(string(framework.StaleGangEviction)), params, staleObjects, nil, opened, []framework.Action{stalegangeviction.New()})
	require.NotEmpty(t, evicted)
	require.Contains(t, string(mustJSON(t, staleState)), commonconstants.StalePodgroupTimeStamp)
	require.Equal(t, hashDecisions(evicted, recorder.NewHasher([]byte("fidelity-key"))), replayRedacted(t, actionConfig(string(framework.StaleGangEviction)), staleState, []framework.Action{stalegangeviction.New()}))
}

func TestReplayMultiActionCycle(t *testing.T) {
	registerReplayPlugins(t)
	opened := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	config := actionConfig("allocate, preempt")
	params := actionParams(false)
	objects := multiActionObjects()
	actionsToRun := []framework.Action{allocate.New(), preempt.New()}

	first, state := runActionsAt(t, config, params, objects, nil, opened, actionsToRun)
	require.NotEmpty(t, first)
	var sawAllocate, sawPreempt bool
	for _, line := range first {
		if strings.HasPrefix(line, "allocate ") {
			require.False(t, sawPreempt, "allocate commits must stay ahead of preempt: %v", first)
			sawAllocate = true
		}
		if strings.HasPrefix(line, "preempt ") {
			sawPreempt = true
		}
	}
	require.True(t, sawAllocate && sawPreempt, "decisions: %v", first)
	require.Equal(t, first, replayPlain(t, config, state, actionsToRun))
	require.Equal(t, hashDecisions(first, recorder.NewHasher([]byte("fidelity-key"))), replayRedacted(t, config, state, actionsToRun))
}

func registerReplayPlugins(t *testing.T) {
	t.Helper()
	actions.InitDefaultActions()
	framework.RegisterPluginBuilder("predicates", predicates.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)
	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder(nodelocalgreedy.Name, nodelocalgreedy.New)
	framework.RegisterPluginBuilder(multinodegang.Name, multinodegang.New)
}

func runActionsAt(
	t *testing.T, config *conf.SchedulerConfiguration, params *conf.SchedulerParams,
	objects *snapshot.RawKubernetesObjects, usage *queue_info.ClusterUsage, at time.Time,
	toRun []framework.Action,
) ([]string, *cycleRecord) {
	t.Helper()
	var now func() time.Time
	if !at.IsZero() {
		opened := at
		now = func() time.Time { return opened }
	}
	var recorded *cycleRecord
	var decisions []string
	plugin := newPlugin(nil, func(payload *cycleRecord) bool {
		recorded = payload
		return true
	})
	schedCache := startReplayCache(t, params, objects, usage, now)
	session, err := framework.OpenSession(schedCache, config, params, "fidelity", http.NewServeMux())
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
	for _, action := range toRun {
		session.SetCurrentAction(string(action.Name()))
		action.Execute(session)
	}
	plugin.OnSessionClose(session)
	framework.CloseSession(session)
	return decisions, recorded
}

func replayPlain(t *testing.T, config *conf.SchedulerConfiguration, state *cycleRecord, toRun []framework.Action) []string {
	t.Helper()
	replayed, _ := runActionsAt(t, config, state.SchedulerParams, state.RawObjects, state.Usage, state.OpenedAt, toRun)
	return replayed
}

func replayRedacted(t *testing.T, config *conf.SchedulerConfiguration, state *cycleRecord, toRun []framework.Action) []string {
	t.Helper()
	env := &recorder.Envelope{Kind: recorder.KindCycle, Record: mustJSON(t, state)}
	require.NoError(t, recorder.RedactEnvelope(env, recorder.NewHasher([]byte("fidelity-key"))))
	var redacted cycleRecord
	require.NoError(t, json.Unmarshal(env.Record, &redacted))
	replayed, _ := runActionsAt(t, config, redacted.SchedulerParams, redacted.RawObjects, redacted.Usage, redacted.OpenedAt, toRun)
	return replayed
}

func hashDecisions(lines []string, h recorder.Hasher) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		parts := strings.SplitN(line, " ", 3)
		place := strings.SplitN(parts[2], "@", 2)
		name := strings.SplitN(place[0], "/", 2)
		out[i] = parts[0] + " " + parts[1] + " " + h.Hash(name[0]) + "/" + h.Hash(name[1]) + "@" + h.Hash(place[1])
	}
	return out
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func openAt(t *testing.T, params *conf.SchedulerParams, objects *snapshot.RawKubernetesObjects, usage *queue_info.ClusterUsage, at time.Time) *framework.Session {
	t.Helper()
	var now func() time.Time
	if !at.IsZero() {
		opened := at
		now = func() time.Time { return opened }
	}
	schedCache := startReplayCache(t, params, objects, usage, now)
	session, err := framework.OpenSession(schedCache, actionConfig(string(framework.Allocate)), params, "fidelity-open", http.NewServeMux())
	require.NoError(t, err)
	return session
}

func taskStatus(session *framework.Session, name string) pod_status.PodStatus {
	for _, node := range session.ClusterInfo.Nodes {
		if node == nil {
			continue
		}
		for _, task := range node.PodInfos {
			if task != nil && task.Name == name {
				return task.Status
			}
		}
	}
	for _, job := range session.ClusterInfo.PodGroupInfos {
		if job == nil {
			continue
		}
		for _, task := range job.GetAllPodsMap() {
			if task != nil && task.Name == name {
				return task.Status
			}
		}
	}
	return 0
}

func usageObjects(opened time.Time) *snapshot.RawKubernetesObjects {
	heavy := quotaQueue("q-heavy", 0, 1)
	light := quotaQueue("q-light", 0, 1)
	heavy.CreationTimestamp = metav1.NewTime(opened.Add(-2 * time.Hour))
	light.CreationTimestamp = metav1.NewTime(opened.Add(-time.Minute))
	return &snapshot.RawKubernetesObjects{
		Nodes:  []*v1.Node{cpuNode("node-a", "1")},
		Queues: []*enginev2.Queue{heavy, light},
		PodGroups: []*enginev2alpha2.PodGroup{
			podGroup("team", "heavy", "q-heavy", ""),
			podGroup("team", "light", "q-light", ""),
		},
		Pods: []*v1.Pod{
			workloadPod("team", "heavy-0", "heavy", "", "1", v1.PodPending),
			workloadPod("team", "light-0", "light", "", "1", v1.PodPending),
		},
	}
}

func fieldObjects(opened time.Time) *snapshot.RawKubernetesObjects {
	oldQ := quotaQueue("q-old", -1, 1)
	newQ := quotaQueue("q-new", -1, 1)
	oldQ.CreationTimestamp = metav1.NewTime(opened.Add(-2 * time.Hour))
	newQ.CreationTimestamp = metav1.NewTime(opened.Add(-time.Minute))
	ready := workloadPod("team", "ready-0", "ready", "", "100m", v1.PodPending)
	ready.Spec.Containers[0].Env = []v1.EnvVar{{
		Name: "CFG",
		ValueFrom: &v1.EnvVarSource{ConfigMapKeyRef: &v1.ConfigMapKeySelector{
			LocalObjectReference: v1.LocalObjectReference{Name: "app-config"},
			Key:                  "data",
		}},
	}}
	ready.Spec.ResourceClaims = []v1.PodResourceClaim{{
		Name:              "gpu",
		ResourceClaimName: ptr.To("claim-a"),
	}}
	gated := workloadPod("team", "gated-0", "gated", "", "100m", v1.PodPending)
	gated.Spec.SchedulingGates = []v1.PodSchedulingGate{{Name: "gpu-init"}}
	ported := workloadPod("team", "ported-0", "ported", "", "100m", v1.PodPending)
	ported.Spec.Containers[0].Ports = []v1.ContainerPort{{ContainerPort: 80, HostPort: 80, Protocol: v1.ProtocolTCP}}
	holder := workloadPod("team", "holder-0", "holder", "node-a", "100m", v1.PodRunning)
	holder.Spec.Containers[0].Ports = []v1.ContainerPort{{ContainerPort: 80, HostPort: 80, Protocol: v1.ProtocolTCP}}
	stuck := workloadPod("team", "stuck-0", "stuck", "node-a", "100m", v1.PodRunning)
	stuck.DeletionTimestamp = &metav1.Time{Time: opened.Add(-2 * time.Hour)}
	releasing := workloadPod("team", "releasing-0", "releasing", "node-a", "100m", v1.PodRunning)
	releasing.DeletionTimestamp = &metav1.Time{Time: opened.Add(-time.Minute)}
	node := cpuNode("node-a", "4")
	return &snapshot.RawKubernetesObjects{
		Nodes:  []*v1.Node{node},
		Queues: []*enginev2.Queue{oldQ, newQ, quotaQueue("q-shared", -1, 1)},
		PodGroups: []*enginev2alpha2.PodGroup{
			podGroup("team", "ready", "q-old", ""),
			podGroup("team", "gated", "q-old", ""),
			podGroup("team", "ported", "q-old", ""),
			podGroup("team", "holder", "q-shared", ""),
			podGroup("team", "stuck", "q-shared", ""),
			podGroup("team", "releasing", "q-shared", ""),
			podGroup("team", "young", "q-new", ""),
		},
		Pods: []*v1.Pod{
			ready, gated, ported, holder, stuck, releasing,
			workloadPod("team", "young-0", "young", "", "4", v1.PodPending),
		},
		ConfigMaps: []*v1.ConfigMap{{
			ObjectMeta: metav1.ObjectMeta{Name: "app-config", Namespace: "team"},
		}},
		ResourceClaims: []*resourceapi.ResourceClaim{{
			ObjectMeta: metav1.ObjectMeta{Name: "claim-a", Namespace: "team"},
		}},
	}
}

func staleGangObjects(opened time.Time) *snapshot.RawKubernetesObjects {
	group := podGroup("team", "gang", "q-shared", "")
	group.Spec.MinMember = ptr.To(int32(2))
	group.Spec.StalenessGracePeriod = &metav1.Duration{Duration: 30 * time.Minute}
	group.Annotations = map[string]string{
		commonconstants.StalePodgroupTimeStamp: opened.Add(-time.Hour).Format(time.RFC3339),
	}
	return &snapshot.RawKubernetesObjects{
		Nodes:     []*v1.Node{cpuNode("node-a", "4")},
		Queues:    []*enginev2.Queue{quotaQueue("q-shared", -1, 1)},
		PodGroups: []*enginev2alpha2.PodGroup{group},
		Pods: []*v1.Pod{
			workloadPod("team", "gang-0", "gang", "node-a", "100m", v1.PodRunning),
		},
	}
}

func multiActionObjects() *snapshot.RawKubernetesObjects {
	nodeA := cpuNode("node-a", "1")
	nodeB := cpuNode("node-b", "1")
	nodeA.Labels = map[string]string{"kubernetes.io/hostname": "node-a"}
	nodeB.Labels = map[string]string{"kubernetes.io/hostname": "node-b"}
	pinned := workloadPod("team", "pinned-0", "pinned", "", "800m", v1.PodPending)
	pinned.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": "node-b"}
	pinned.Spec.PriorityClassName = "high"
	victim := workloadPod("team", "victim-0", "victim", "node-b", "800m", v1.PodRunning)
	victim.Spec.PriorityClassName = "low"
	return &snapshot.RawKubernetesObjects{
		Nodes:  []*v1.Node{nodeA, nodeB},
		Queues: []*enginev2.Queue{quotaQueue("q-shared", 4000, 1)},
		PodGroups: []*enginev2alpha2.PodGroup{
			podGroup("team", "small", "q-shared", ""),
			podGroup("team", "victim", "q-shared", "low"),
			podGroup("team", "pinned", "q-shared", "high"),
		},
		Pods: []*v1.Pod{
			workloadPod("team", "small-0", "small", "", "500m", v1.PodPending),
			victim,
			pinned,
		},
		PriorityClasses: []*schedulingv1.PriorityClass{
			{ObjectMeta: metav1.ObjectMeta{Name: "low"}, Value: 50},
			{ObjectMeta: metav1.ObjectMeta{Name: "high"}, Value: 80},
		},
	}
}
