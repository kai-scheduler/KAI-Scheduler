// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	scheduler_cache "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
)

// A resize reservation is a synthetic pending pod with no real workload behind it, so BindPod must
// short-circuit before reaching the cache: no BindRequest is emitted. The mock cache has no Bind
// expectation, so any call to it fails the test; the early return also avoids the nil ClusterInfo
// dereference the normal bind path would otherwise hit here, so a clean pass proves the guard.
func TestBindPodSkipsResizeReservation(t *testing.T) {
	cacheMock := scheduler_cache.NewMockCache(gomock.NewController(t))
	ssn := &Session{Cache: cacheMock}

	reservation := &pod_info.PodInfo{
		Namespace:           "ns",
		Name:                "resize-reservation",
		NodeName:            "node0",
		IsResizeReservation: true,
	}

	if err := ssn.BindPod(reservation); err != nil {
		t.Fatalf("BindPod(resize reservation) = %v, want nil", err)
	}
}
