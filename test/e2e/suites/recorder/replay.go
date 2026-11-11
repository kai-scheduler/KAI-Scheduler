// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/fake"

	schedrec "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	kaifake "github.com/kai-scheduler/api/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()
}

// replayCycle runs one action against the saved state copy and returns the
// committed placements and evictions, sorted, plus a summary of the session.
func replayCycle(state schedrec.StateRecord, actionName string) ([]string, string) {
	Expect(state.RawObjects).NotTo(BeNil())
	Expect(state.Config).NotTo(BeNil())
	params := state.SchedulerParams
	if params == nil {
		params = &conf.SchedulerParams{SchedulerName: "kai-scheduler"}
	}
	if params.PartitionParams == nil {
		params.PartitionParams = &conf.SchedulingNodePoolParams{}
	}
	if params.NumOfStatusRecordingWorkers == 0 {
		params.NumOfStatusRecordingWorkers = 1
	}

	config := *state.Config
	config.Actions = actionName
	config.Tiers = dropRecorderPlugin(config.Tiers)

	schedCache := replayCache(params, state.RawObjects)
	session, err := framework.OpenSession(schedCache, &config, params, "e2e-replay", http.NewServeMux())
	Expect(err).NotTo(HaveOccurred())
	defer framework.CloseSession(session)

	var decisions []string
	session.AddEventHandler(&framework.EventHandler{OnCommit: func(event *framework.CommitEvent) {
		if event.DiscardedOpCount > 0 && len(event.Ops) == 0 {
			return
		}
		for _, op := range event.Ops {
			if op.NotAttempted {
				continue
			}
			decisions = append(decisions, event.Action+" "+op.Kind+" "+op.Namespace+"/"+op.Name+"@"+op.Node)
		}
	}})
	session.SetCurrentAction(actionName)
	action := actionByName(actionName)
	Expect(action).NotTo(BeNil())
	action.Execute(session)
	sort.Strings(decisions)
	return decisions, replaySummary(state, actionName, session, decisions)
}

func replaySummary(state schedrec.StateRecord, actionName string, session *framework.Session, decisions []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "session %s action %s decisions %v\n", state.SessionID, actionName, decisions)
	if state.Config != nil {
		fmt.Fprintf(&b, "config actions %q\n", state.Config.Actions)
	}
	if state.SchedulerParams != nil && state.SchedulerParams.PartitionParams != nil {
		fmt.Fprintf(&b, "partition key=%q value=%q\n",
			state.SchedulerParams.PartitionParams.NodePoolLabelKey,
			state.SchedulerParams.PartitionParams.NodePoolLabelValue)
	}
	if state.RawObjects != nil {
		fmt.Fprintf(&b, "snapshot nodes=%d pods=%d queues=%d podgroups=%d priorityClasses=%d\n",
			len(state.RawObjects.Nodes), len(state.RawObjects.Pods), len(state.RawObjects.Queues),
			len(state.RawObjects.PodGroups), len(state.RawObjects.PriorityClasses))
	}
	if session == nil || session.ClusterInfo == nil {
		b.WriteString("replay session has no cluster info\n")
		return b.String()
	}
	fmt.Fprintf(&b, "replay session nodes=%d queues=%d jobs=%d\n",
		len(session.ClusterInfo.Nodes), len(session.ClusterInfo.Queues), len(session.ClusterInfo.PodGroupInfos))
	lines := make([]string, 0, len(session.ClusterInfo.PodGroupInfos))
	for _, job := range session.ClusterInfo.PodGroupInfos {
		if job == nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("job %s queue=%s priority=%d pending=%d allocated=%d",
			job.Name, job.Queue, job.Priority, len(job.GetPendingTasks()), job.GetNumAllocatedTasks()))
	}
	sort.Strings(lines)
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func actionByName(name string) framework.Action {
	switch framework.ActionType(name) {
	case framework.Allocate:
		return allocate.New()
	case framework.Reclaim:
		return reclaim.New()
	case framework.Preempt:
		return preempt.New()
	default:
		return nil
	}
}

func dropRecorderPlugin(tiers []conf.Tier) []conf.Tier {
	out := make([]conf.Tier, len(tiers))
	for i, tier := range tiers {
		out[i] = tier
		out[i].Plugins = nil
		for _, plugin := range tier.Plugins {
			if plugin.Name == "recorder" {
				continue
			}
			out[i].Plugins = append(out[i].Plugins, plugin)
		}
	}
	return out
}

func replayCache(params *conf.SchedulerParams, objects *snapshot.RawKubernetesObjects) cache.Cache {
	kubeClient := fake.NewSimpleClientset()
	kaiClient := kaifake.NewSimpleClientset()
	for _, node := range objects.Nodes {
		if node == nil || node.Name == "" {
			continue
		}
		_, err := kubeClient.CoreV1().Nodes().Create(context.Background(), node.DeepCopy(), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
	for _, pod := range objects.Pods {
		if pod == nil || pod.Name == "" || pod.Namespace == "" {
			continue
		}
		_, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod.DeepCopy(), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
	for _, queue := range objects.Queues {
		if queue == nil || queue.Name == "" {
			continue
		}
		_, err := kaiClient.SchedulingV2().Queues(queue.Namespace).Create(context.Background(), queue.DeepCopy(), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
	for _, group := range objects.PodGroups {
		if group == nil || group.Name == "" || group.Namespace == "" {
			continue
		}
		_, err := kaiClient.SchedulingV2alpha2().PodGroups(group.Namespace).Create(context.Background(), group.DeepCopy(), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
	for _, priorityClass := range objects.PriorityClasses {
		if priorityClass == nil || priorityClass.Name == "" {
			continue
		}
		_, err := kubeClient.SchedulingV1().PriorityClasses().Create(context.Background(), priorityClass.DeepCopy(), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
	schedCache, err := cache.New(&cache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kaiClient,
		SchedulerName:               params.SchedulerName,
		NodePoolParams:              params.PartitionParams,
		RestrictNodeScheduling:      params.RestrictSchedulingNodes,
		FullHierarchyFairness:       params.FullHierarchyFairness,
		NumOfStatusRecordingWorkers: params.NumOfStatusRecordingWorkers,
		DiscoveryClient:             kubeClient.Discovery(),
	})
	Expect(err).NotTo(HaveOccurred())
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	schedCache.Run(ctx.Done())
	schedCache.WaitForCacheSync(ctx.Done())
	return schedCache
}
