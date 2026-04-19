// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

type Event struct {
	Task *pod_info.PodInfo
}

type EventHandler struct {
	AllocateFunc   func(event *Event)
	DeallocateFunc func(event *Event)
}
