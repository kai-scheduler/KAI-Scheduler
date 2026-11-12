// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	schedreplay "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/replay"
)

func init() {
	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()
}

// recordedCycle is one line the sidecar stored for a scheduling cycle.
type recordedCycle struct {
	snapshot.Snapshot
	SessionID string                   `json:"sessionID"`
	OpenedAt  time.Time                `json:"openedAt"`
	Decisions []framework.CommitEvent  `json:"decisions,omitempty"`
	Usage     *queue_info.ClusterUsage `json:"usage,omitempty"`
}

// replayCycle runs the recorded action sequence against the saved state copy.
func replayCycle(state recordedCycle) ([]string, string) {
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
	config.Tiers = dropRecorderPlugin(config.Tiers)

	var now func() time.Time
	if !state.OpenedAt.IsZero() {
		opened := state.OpenedAt
		now = func() time.Time { return opened }
	}
	schedCache, stop, err := schedreplay.NewReplayCache(params, state.RawObjects, state.Usage, now)
	Expect(err).NotTo(HaveOccurred())
	defer stop()
	session, err := framework.OpenSession(schedCache, &config, params, "e2e-replay", http.NewServeMux())
	Expect(err).NotTo(HaveOccurred())
	defer framework.CloseSession(session)

	var decisions []string
	session.AddEventHandler(&framework.EventHandler{OnCommit: func(event *framework.CommitEvent) {
		for _, op := range event.Ops {
			if op.NotAttempted {
				continue
			}
			decisions = append(decisions, event.Action+" "+op.Kind+" "+op.Namespace+"/"+op.Name+"@"+op.Node)
		}
	}})
	actionList, err := conf_util.GetActionsFromConfig(&config)
	Expect(err).NotTo(HaveOccurred())
	for _, action := range actionList {
		session.SetCurrentAction(string(action.Name()))
		action.Execute(session)
	}
	return decisions, replaySummary(state, session, decisions)
}

func replaySummary(state recordedCycle, session *framework.Session, decisions []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "session %s decisions %v\n", state.SessionID, decisions)
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
