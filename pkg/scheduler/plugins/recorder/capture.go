// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	record "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

// cycleRecord is one scheduling cycle. The embedded snapshot is built from the
// objects cache.Snapshot already listed for the session, not from a second list.
// OpenedAt is the cycle clock. Decisions are the commits, in order.
// StateID names the cluster-state copy this cycle used. The copy itself is omitted when
// an earlier cycle in this process already stored the same state within the save interval.
// StateMissing is set when that copy was not stored or is not in the records being read.
type cycleRecord struct {
	snapshot.Snapshot
	SessionID    string                   `json:"sessionID"`
	OpenedAt     time.Time                `json:"openedAt"`
	ClusterName  string                   `json:"clusterName,omitempty"`
	ShardName    string                   `json:"shardName,omitempty"`
	Decisions    []framework.CommitEvent  `json:"decisions,omitempty"`
	Usage        *queue_info.ClusterUsage `json:"usage,omitempty"`
	Pending      []pendingGroup           `json:"pending,omitempty"`
	StateID      string                   `json:"stateId,omitempty"`
	StateMissing bool                     `json:"stateMissing,omitempty"`
}

// pendingGroup is a pod group that still had a pending pod when the cycle closed.
// Attempted is true when an action entered its placement function for the group.
// ReasonCodes are the codes KAI recorded during the cycle. Pod group status is not copied.
type pendingGroup struct {
	UID         string   `json:"uid,omitempty"`
	Namespace   string   `json:"namespace,omitempty"`
	Name        string   `json:"name,omitempty"`
	Attempted   bool     `json:"attempted"`
	ReasonCodes []string `json:"reasonCodes,omitempty"`
}

func buildCycle(ssn *framework.Session, decisions []framework.CommitEvent, openedAt time.Time) *cycleRecord {
	record := &cycleRecord{
		SessionID: ssn.ID,
		OpenedAt:  openedAt,
		Decisions: append([]framework.CommitEvent(nil), decisions...),
	}
	record.Config = ssn.Config
	params := ssn.SchedulerParams
	record.SchedulerParams = &params
	record.Pending = pendingFromSession(ssn)
	if ssn.ClusterInfo == nil {
		return record
	}
	record.RawObjects = objectsFromCluster(ssn.ClusterInfo, &params)
	record.Usage = copyUsage(&ssn.ClusterInfo.QueueResourceUsage)
	return record
}

func pendingFromSession(ssn *framework.Session) []pendingGroup {
	if ssn == nil || ssn.ClusterInfo == nil {
		return nil
	}
	var out []pendingGroup
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if job == nil || job.GetNumPendingTasks() == 0 {
			continue
		}
		out = append(out, pendingGroup{
			UID:         string(job.UID),
			Namespace:   job.Namespace,
			Name:        job.Name,
			Attempted:   ssn.JobWasAttempted(job.UID),
			ReasonCodes: jobReasonCodes(job),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func jobReasonCodes(job *podgroup_info.PodGroupInfo) []string {
	seen := map[string]struct{}{}
	var codes []string
	add := func(code string) {
		if !record.KeepReasonCode(code) {
			return
		}
		if _, ok := seen[code]; ok {
			return
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	for _, fitErr := range job.JobFitErrors {
		if fitErr != nil {
			add(string(fitErr.Reason()))
		}
	}
	for _, taskErr := range job.TasksFitErrors {
		if taskErr == nil {
			continue
		}
		for _, code := range taskErr.ReasonCodes() {
			add(code)
		}
	}
	sort.Strings(codes)
	return codes
}

// stateHash is a content id for the cluster state a cycle used.
// The synthetic parent queue is stamped with the current time on every snapshot, so that
// timestamp is left out of the id. A real change to pods, nodes, queues, or settings still changes it.
func stateHash(cycle *cycleRecord) string {
	if cycle == nil {
		return ""
	}
	payload := struct {
		Config *conf.SchedulerConfiguration   `json:"config"`
		Params *conf.SchedulerParams          `json:"schedulerParams"`
		Raw    *snapshot.RawKubernetesObjects `json:"rawObjects"`
		Usage  *queue_info.ClusterUsage       `json:"usage"`
	}{cycle.Config, cycle.SchedulerParams, cycle.RawObjects, cycle.Usage}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return ""
	}
	scrubSyntheticQueueTime(generic)
	raw, err = json.Marshal(generic)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

func scrubSyntheticQueueTime(state map[string]any) {
	raw, _ := state["rawObjects"].(map[string]any)
	queues, _ := raw["queues"].([]any)
	for _, item := range queues {
		queue, _ := item.(map[string]any)
		meta, _ := queue["metadata"].(map[string]any)
		if meta["name"] == "default" {
			delete(meta, "creationTimestamp")
		}
	}
}

// ResolveStates copies a stored state copy onto cycles that only point at it.
// A cycle with StateMissing set, or whose StateID is not present, returns an error.
func ResolveStates(cycles []*cycleRecord) error {
	byID := map[string]*cycleRecord{}
	for _, cycle := range cycles {
		if cycle == nil || cycle.StateID == "" || cycle.RawObjects == nil {
			continue
		}
		if _, ok := byID[cycle.StateID]; !ok {
			byID[cycle.StateID] = cycle
		}
	}
	for _, cycle := range cycles {
		if cycle == nil {
			continue
		}
		if cycle.StateMissing {
			return fmt.Errorf("recorder state %s is missing", cycle.StateID)
		}
		if cycle.RawObjects != nil || cycle.StateID == "" {
			continue
		}
		holder := byID[cycle.StateID]
		if holder == nil {
			cycle.StateMissing = true
			return fmt.Errorf("recorder state %s is missing", cycle.StateID)
		}
		cycle.Config = holder.Config
		cycle.SchedulerParams = holder.SchedulerParams
		cycle.RawObjects = holder.RawObjects
		cycle.Usage = holder.Usage
	}
	return nil
}

// markMissingState sets StateMissing when this cycle points at a state copy that was not stored.
func markMissingState(cycle *cycleRecord, written map[string]struct{}) {
	if cycle == nil || cycle.RawObjects != nil || cycle.StateID == "" {
		return
	}
	if _, ok := written[cycle.StateID]; ok {
		return
	}
	cycle.StateMissing = true
}

func objectsFromCluster(info *api.ClusterInfo, params *conf.SchedulerParams) *snapshot.RawKubernetesObjects {
	raw := &snapshot.RawKubernetesObjects{}
	if info == nil {
		return raw
	}
	raw.Pods = shardPods(info)
	for _, node := range info.Nodes {
		if node == nil {
			continue
		}
		if node.Node != nil {
			raw.Nodes = append(raw.Nodes, node.Node)
		}
		if node.NodeResourceTopology != nil {
			raw.NodeResourceTopologies = append(raw.NodeResourceTopologies, node.NodeResourceTopology)
		}
	}
	for _, queue := range info.Queues {
		if queue == nil {
			continue
		}
		raw.Queues = append(raw.Queues, queueFromInfo(queue, params))
	}
	classes := map[string]int32{}
	var jobs []*podgroup_info.PodGroupInfo
	for _, job := range info.PodGroupInfos {
		if job == nil || job.PodGroup == nil {
			continue
		}
		jobs = append(jobs, job)
		if name := job.PodGroup.Spec.PriorityClassName; name != "" {
			classes[name] = job.Priority
		}
	}
	for _, job := range jobs {
		pg := recordedPodGroup(job, classes)
		raw.PodGroups = append(raw.PodGroups, pg)
		if pg.Spec.PriorityClassName != "" {
			classes[pg.Spec.PriorityClassName] = job.Priority
		}
	}
	for _, request := range info.BindRequests {
		if request != nil && request.BindRequest != nil {
			raw.BindRequests = append(raw.BindRequests, request.BindRequest)
		}
	}
	for _, request := range info.FailedBindRequestsForShard {
		if request != nil && request.BindRequest != nil {
			raw.BindRequests = append(raw.BindRequests, request.BindRequest)
		}
	}
	raw.ResourceClaims = info.ResourceClaims
	raw.ResourceSlices = info.ResourceSlices
	raw.DeviceClasses = info.DeviceClasses
	raw.Topologies = info.Topologies
	for _, cm := range info.ConfigMaps {
		if cm == nil {
			continue
		}
		raw.ConfigMaps = append(raw.ConfigMaps, &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cm.Name, Namespace: cm.Namespace}})
	}
	for name, value := range classes {
		raw.PriorityClasses = append(raw.PriorityClasses, &schedulingv1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Value:      value,
		})
	}
	sortRawObjects(raw)
	return raw
}

// recordedPodGroup keeps the priority the session resolved. A group that names no class
// inherited the cluster default; the copy names a class with that value so replay does too.
// The live object is left unchanged. The generated name stays kai-effective-<n> unless that
// name is already used with a different value, in which case a numeric suffix is added.
func recordedPodGroup(job *podgroup_info.PodGroupInfo, classes map[string]int32) *enginev2alpha2.PodGroup {
	pg := job.PodGroup
	if pg.Spec.PriorityClassName != "" {
		return pg
	}
	copied := pg.DeepCopy()
	copied.Spec.PriorityClassName = uniqueEffectivePriorityClass(job.Priority, classes)
	return copied
}

func uniqueEffectivePriorityClass(priority int32, classes map[string]int32) string {
	base := fmt.Sprintf("kai-effective-%d", priority)
	if value, ok := classes[base]; !ok || value == priority {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s-%d", base, i)
		if value, ok := classes[name]; !ok || value == priority {
			return name
		}
	}
}

func sortRawObjects(raw *snapshot.RawKubernetesObjects) {
	if raw == nil {
		return
	}
	sort.Slice(raw.Pods, func(i, j int) bool {
		return lessMeta(raw.Pods[i].Namespace, raw.Pods[i].Name, raw.Pods[j].Namespace, raw.Pods[j].Name)
	})
	sort.Slice(raw.Nodes, func(i, j int) bool { return raw.Nodes[i].Name < raw.Nodes[j].Name })
	sort.Slice(raw.Queues, func(i, j int) bool { return raw.Queues[i].Name < raw.Queues[j].Name })
	sort.Slice(raw.PodGroups, func(i, j int) bool {
		return lessMeta(raw.PodGroups[i].Namespace, raw.PodGroups[i].Name, raw.PodGroups[j].Namespace, raw.PodGroups[j].Name)
	})
	sort.Slice(raw.BindRequests, func(i, j int) bool {
		return lessMeta(raw.BindRequests[i].Namespace, raw.BindRequests[i].Name, raw.BindRequests[j].Namespace, raw.BindRequests[j].Name)
	})
	sort.Slice(raw.PriorityClasses, func(i, j int) bool { return raw.PriorityClasses[i].Name < raw.PriorityClasses[j].Name })
	sort.Slice(raw.ConfigMaps, func(i, j int) bool {
		return lessMeta(raw.ConfigMaps[i].Namespace, raw.ConfigMaps[i].Name, raw.ConfigMaps[j].Namespace, raw.ConfigMaps[j].Name)
	})
	sort.Slice(raw.Topologies, func(i, j int) bool { return raw.Topologies[i].Name < raw.Topologies[j].Name })
	sort.Slice(raw.NodeResourceTopologies, func(i, j int) bool {
		return raw.NodeResourceTopologies[i].Name < raw.NodeResourceTopologies[j].Name
	})
	sort.Slice(raw.ResourceClaims, func(i, j int) bool {
		return lessMeta(raw.ResourceClaims[i].Namespace, raw.ResourceClaims[i].Name, raw.ResourceClaims[j].Namespace, raw.ResourceClaims[j].Name)
	})
	sort.Slice(raw.ResourceSlices, func(i, j int) bool {
		return lessMeta(raw.ResourceSlices[i].Namespace, raw.ResourceSlices[i].Name, raw.ResourceSlices[j].Namespace, raw.ResourceSlices[j].Name)
	})
	sort.Slice(raw.DeviceClasses, func(i, j int) bool {
		return raw.DeviceClasses[i].Name < raw.DeviceClasses[j].Name
	})
}

func lessMeta(nsA, nameA, nsB, nameB string) bool {
	if nsA != nsB {
		return nsA < nsB
	}
	return nameA < nameB
}

func shardPods(info *api.ClusterInfo) []*v1.Pod {
	seen := map[types.UID]struct{}{}
	var pods []*v1.Pod
	add := func(pod *v1.Pod) {
		if pod == nil {
			return
		}
		if pod.Spec.NodeName != "" {
			if _, ok := info.Nodes[pod.Spec.NodeName]; !ok {
				return
			}
		}
		if pod.UID != "" {
			if _, ok := seen[pod.UID]; ok {
				return
			}
			seen[pod.UID] = struct{}{}
		}
		pods = append(pods, pod)
	}
	for _, node := range info.Nodes {
		if node == nil {
			continue
		}
		for _, task := range node.PodInfos {
			if task != nil {
				add(task.Pod)
			}
		}
	}
	for _, job := range info.PodGroupInfos {
		if job == nil {
			continue
		}
		for _, task := range job.GetAllPodsMap() {
			if task != nil {
				add(task.Pod)
			}
		}
	}
	return pods
}

func queueFromInfo(queue *queue_info.QueueInfo, params *conf.SchedulerParams) *enginev2.Queue {
	priority := queue.Priority
	out := &enginev2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:              string(queue.UID),
			CreationTimestamp: queue.CreationTimestamp,
		},
		Spec: enginev2.QueueSpec{
			ParentQueue:       string(queue.ParentQueue),
			DisplayName:       queue.DisplayName,
			Priority:          &priority,
			PreemptMinRuntime: queue.PreemptMinRuntime,
			ReclaimMinRuntime: queue.ReclaimMinRuntime,
			Resources: &enginev2.QueueResources{
				GPU:    enginev2.QueueResource{Quota: queue.Resources.GPU.Quota, Limit: queue.Resources.GPU.Limit, OverQuotaWeight: queue.Resources.GPU.OverQuotaWeight},
				CPU:    enginev2.QueueResource{Quota: queue.Resources.CPU.Quota, Limit: queue.Resources.CPU.Limit, OverQuotaWeight: queue.Resources.CPU.OverQuotaWeight},
				Memory: enginev2.QueueResource{Quota: queue.Resources.Memory.Quota, Limit: queue.Resources.Memory.Limit, OverQuotaWeight: queue.Resources.Memory.OverQuotaWeight},
			},
		},
	}
	if params != nil && params.PartitionParams != nil {
		for key, value := range params.PartitionParams.GetLabels() {
			if out.Labels == nil {
				out.Labels = map[string]string{}
			}
			out.Labels[key] = value
		}
	}
	return out
}

func copyUsage(in *queue_info.ClusterUsage) *queue_info.ClusterUsage {
	if in == nil || len(in.Queues) == 0 {
		return nil
	}
	out := queue_info.NewClusterUsage()
	for id, usage := range in.Queues {
		copied := make(queue_info.QueueUsage, len(usage))
		for name, value := range usage {
			copied[name] = value
		}
		out.Queues[id] = copied
	}
	return out
}
