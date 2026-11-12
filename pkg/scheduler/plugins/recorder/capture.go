// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"time"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
)

// cycleRecord is one scheduling cycle. The embedded snapshot is built from the
// objects cache.Snapshot already listed for the session, not from a second list.
// OpenedAt is the cycle clock. Decisions are the commits, in order.
type cycleRecord struct {
	snapshot.Snapshot
	SessionID   string                   `json:"sessionID"`
	OpenedAt    time.Time                `json:"openedAt"`
	ClusterName string                   `json:"clusterName,omitempty"`
	ShardName   string                   `json:"shardName,omitempty"`
	Decisions   []framework.CommitEvent  `json:"decisions,omitempty"`
	Usage       *queue_info.ClusterUsage `json:"usage,omitempty"`
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
	if ssn.ClusterInfo == nil {
		return record
	}
	record.RawObjects = objectsFromCluster(ssn.ClusterInfo, &params)
	record.Usage = copyUsage(&ssn.ClusterInfo.QueueResourceUsage)
	return record
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
	for _, job := range info.PodGroupInfos {
		if job == nil || job.PodGroup == nil {
			continue
		}
		raw.PodGroups = append(raw.PodGroups, job.PodGroup)
		if job.PodGroup.Spec.PriorityClassName != "" {
			classes[job.PodGroup.Spec.PriorityClassName] = job.Priority
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
	return raw
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
