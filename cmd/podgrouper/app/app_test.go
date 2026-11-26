// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	controllers "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper"
)

func TestNewSchemeDoesNotRegisterKartaByDefault(t *testing.T) {
	scheme := newScheme()

	_, _, err := scheme.ObjectKinds(&kartav1alpha1.Karta{})

	require.Error(t, err)
}

func TestPodCacheSkipsFailedPods(t *testing.T) {
	podSelector := podCacheSelector(t, getCacheOptions(controllers.Configs{SchedulerName: "kai-scheduler"}))

	tests := []struct {
		name          string
		schedulerName string
		phase         corev1.PodPhase
		expectCached  bool
	}{
		{name: "pending pod", schedulerName: "kai-scheduler", phase: corev1.PodPending, expectCached: true},
		{name: "running pod", schedulerName: "kai-scheduler", phase: corev1.PodRunning, expectCached: true},
		{name: "succeeded pod", schedulerName: "kai-scheduler", phase: corev1.PodSucceeded, expectCached: true},
		{name: "failed pod", schedulerName: "kai-scheduler", phase: corev1.PodFailed, expectCached: false},
		{name: "pod of another scheduler", schedulerName: "default-scheduler", phase: corev1.PodRunning,
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
		if _, isPod := object.(*corev1.Pod); isPod {
			return byObject.Field
		}
	}
	t.Fatal("no pod entry in the cache options")
	return nil
}
