// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

type noop struct{}

func NewNoop() Interface {
	return noop{}
}

func (noop) SyncPodGroupsWithPendingUpdates([]*enginev2alpha2.PodGroup) {}

func (noop) Evicted(*v1.Pod, *enginev2alpha2.PodGroup, eviction_info.EvictionMetadata, string) {
}

func (noop) PreBind(*v1.Pod) {}

func (noop) Bound(*v1.Pod, string, error, string) error { return nil }

func (noop) Pipelined(*v1.Pod, string) {}

func (noop) PatchPodLabels(*v1.Pod, map[string]interface{}) {}

func (noop) RecordJobStatusEvent(
	*podgroup_info.PodGroupInfo,
	func(*podgroup_info.PodGroupInfo, *pod_info.PodInfo) ([]*common_info.TasksFitError, error),
) error {
	return nil
}

func (noop) Run(<-chan struct{}) {}
