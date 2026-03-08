/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package conf

import (
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
)

type SchedulerParams struct {
	SchedulerName                     string                    `json:"schedulerName,omitempty"`
	RestrictSchedulingNodes           bool                      `json:"restrictSchedulingNodes,omitempty"`
	PartitionParams                   *SchedulingNodePoolParams `json:"partitionParams,omitempty"`
	MaxNumberConsolidationPreemptees  int                       `json:"maxNumberConsolidationPreemptees,omitempty"`
	ScheduleCSIStorage                bool                      `json:"scheduleCSIStorage,omitempty"`
	UseSchedulingSignatures           bool                      `json:"useSchedulingSignatures,omitempty"`
	FullHierarchyFairness             bool                      `json:"fullHierarchyFairness,omitempty"`
	AllowConsolidatingReclaim         bool                      `json:"allowConsolidatingReclaim,omitempty"`
	NumOfStatusRecordingWorkers       int                       `json:"numOfStatusRecordingWorkers,omitempty"`
	GlobalDefaultStalenessGracePeriod time.Duration             `json:"globalDefaultStalenessGracePeriod,omitempty"`
	SchedulePeriod                    time.Duration             `json:"schedulePeriod,omitempty"`
	DetailedFitErrors                 bool                      `json:"detailedFitErrors,omitempty"`
	UpdatePodEvictionCondition        bool                      `json:"updatePodEvictionCondition,omitempty"`
	QueueLabelKey                     string                    `json:"queueLabelKey,omitempty"`
}

// SchedulerConfiguration defines the configuration of scheduler.
type SchedulerConfiguration struct {
	// Actions defines the actions list of scheduler in order
	Actions string `yaml:"actions" json:"actions"`

	// Tiers defines plugins in different tiers
	Tiers []Tier `yaml:"tiers,omitempty" json:"tiers,omitempty"`

	// QueueDepthPerAction max number of jobs to try for action per queue
	QueueDepthPerAction map[string]int `yaml:"queueDepthPerAction,omitempty" json:"queueDepthPerAction,omitempty"`

	// UsageDBConfig defines configuration for the usage db client
	UsageDBConfig *usagedbapi.UsageDBConfig `yaml:"usageDBConfig,omitempty" json:"usageDBConfig,omitempty"`
}

// Tier defines plugin tier
type Tier struct {
	Plugins []PluginOption `yaml:"plugins" json:"plugins"`
}

// PluginOption defines the options of plugin
type PluginOption struct {
	// The name of Plugin
	Name string `yaml:"name" json:"name"`
	// JobOrderDisabled defines whether jobOrderFn is disabled
	JobOrderDisabled bool `yaml:"disableJobOrder,omitempty" json:"disableJobOrder,omitempty"`
	// TaskOrderDisabled defines whether taskOrderFn is disabled
	TaskOrderDisabled bool `yaml:"disableTaskOrder,omitempty" json:"disableTaskOrder,omitempty"`
	// PreemptableDisabled defines whether preemptableFn is disabled
	PreemptableDisabled bool `yaml:"disablePreemptable,omitempty" json:"disablePreemptable,omitempty"`
	// ReclaimableDisabled defines whether reclaimableFn is disabled
	ReclaimableDisabled bool `yaml:"disableReclaimable,omitempty" json:"disableReclaimable,omitempty"`
	// QueueOrderDisabled defines whether queueOrderFn is disabled
	QueueOrderDisabled bool `yaml:"disableQueueOrder,omitempty" json:"disableQueueOrder,omitempty"`
	// PredicateDisabled defines whether predicateFn is disabled
	PredicateDisabled bool `yaml:"disablePredicate,omitempty" json:"disablePredicate,omitempty"`
	// NodeOrderDisabled defines whether NodeOrderFn is disabled
	NodeOrderDisabled bool `yaml:"disableNodeOrder,omitempty" json:"disableNodeOrder,omitempty"`
	// Arguments defines the different arguments that can be given to different plugins
	Arguments map[string]string `yaml:"arguments,omitempty" json:"arguments,omitempty"`
}

type SchedulingNodePoolParams struct {
	NodePoolLabelKey   string
	NodePoolLabelValue string
}

func (s *SchedulingNodePoolParams) GetLabelSelector() (labels.Selector, error) {
	if s.NodePoolLabelKey == "" {
		return labels.Everything(), nil
	}
	operator := selection.DoesNotExist
	var vals []string
	if len(s.NodePoolLabelValue) > 0 {
		operator = selection.Equals
		vals = []string{s.NodePoolLabelValue}
	}

	requirement, err := labels.NewRequirement(s.NodePoolLabelKey, operator, vals)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)
	return selector, nil
}

func (s *SchedulingNodePoolParams) GetLabels() map[string]string {
	if s.NodePoolLabelKey == "" || s.NodePoolLabelValue == "" {
		return map[string]string{}
	}
	return map[string]string{s.NodePoolLabelKey: s.NodePoolLabelValue}
}
