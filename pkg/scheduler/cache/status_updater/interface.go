// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	v1 "k8s.io/api/core/v1"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

type PodGroupsSync interface {
	SyncPodGroupsWithPendingUpdates(podGroups []*enginev2alpha2.PodGroup)
}

type Interface interface {
	PodGroupsSync
	Evicted(evictedPodGroup *enginev2alpha2.PodGroup, evictionMetadata eviction_info.EvictionMetadata, message string)
	PreBind(pod *v1.Pod)
	Bound(pod *v1.Pod, hostname string, bindError error, nodePoolName string) error
	Pipelined(pod *v1.Pod, message string)
	PatchPodLabels(pod *v1.Pod, labels map[string]interface{})
	RecordJobStatusEvent(
		job *podgroup_info.PodGroupInfo,
		resolveDetailedFitErrors func(
			*podgroup_info.PodGroupInfo,
			*pod_info.PodInfo,
		) ([]*common_info.TasksFitError, error),
	) error

	Run(stopCh <-chan struct{})
}
