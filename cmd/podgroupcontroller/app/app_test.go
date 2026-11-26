// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

func TestPodCacheSkipsFailedPods(t *testing.T) {
	podSelector := podCacheSelector(t, getCacheOptions("kai-scheduler", ""))

	tests := []struct {
		name          string
		schedulerName string
		phase         v1.PodPhase
		expectCached  bool
	}{
		{name: "pending pod", schedulerName: "kai-scheduler", phase: v1.PodPending, expectCached: true},
		{name: "running pod", schedulerName: "kai-scheduler", phase: v1.PodRunning, expectCached: true},
		{name: "succeeded pod", schedulerName: "kai-scheduler", phase: v1.PodSucceeded, expectCached: true},
		{name: "failed pod", schedulerName: "kai-scheduler", phase: v1.PodFailed, expectCached: false},
		{name: "pod of another scheduler", schedulerName: "default-scheduler", phase: v1.PodRunning,
			expectCached: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podFields := fields.Set{"spec.schedulerName": tt.schedulerName, "status.phase": string(tt.phase)}
			require.Equal(t, tt.expectCached, podSelector.Matches(podFields))
		})
	}
}

func podCacheSelector(t *testing.T, options cache.Options) fields.Selector {
	t.Helper()
	for object, byObject := range options.ByObject {
		if _, isPod := object.(*v1.Pod); isPod {
			return byObject.Field
		}
	}
	t.Fatal("no pod entry in the cache options")
	return nil
}
