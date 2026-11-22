// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"testing"

	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
)

func TestUnchangedDRAListStateID(t *testing.T) {
	forward := draCluster(false)
	reverse := draCluster(true)
	first := objectsFromCluster(forward, &conf.SchedulerParams{})
	second := objectsFromCluster(reverse, &conf.SchedulerParams{})

	require.Equal(t, []string{"ns/a", "ns/b", "other/a"}, claimNames(first))
	require.Equal(t, []string{"ns/a", "ns/c", "other/b"}, sliceNames(first))
	require.Equal(t, []string{"class-a", "class-z"}, classNames(first))
	require.Equal(t, claimNames(first), claimNames(second))
	require.Equal(t, sliceNames(first), sliceNames(second))
	require.Equal(t, classNames(first), classNames(second))
	require.Equal(t, stateHash(cycleWith(first)), stateHash(cycleWith(second)))
}

func cycleWith(raw *snapshot.RawKubernetesObjects) *cycleRecord {
	cycle := &cycleRecord{}
	cycle.RawObjects = raw
	return cycle
}

func draCluster(reverse bool) *api.ClusterInfo {
	claims := []*resourceapi.ResourceClaim{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "b"}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "a"}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "a"}},
	}
	slices := []*resourceapi.ResourceSlice{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "b"}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "c"}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "a"}},
	}
	classes := []*resourceapi.DeviceClass{
		{ObjectMeta: metav1.ObjectMeta{Name: "class-z"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "class-a"}},
	}
	if reverse {
		reverseSlice(claims)
		reverseSlice(slices)
		reverseSlice(classes)
	}
	return &api.ClusterInfo{ResourceClaims: claims, ResourceSlices: slices, DeviceClasses: classes}
}

func reverseSlice[T any](items []T) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func claimNames(raw *snapshot.RawKubernetesObjects) []string {
	var names []string
	for _, claim := range raw.ResourceClaims {
		names = append(names, claim.Namespace+"/"+claim.Name)
	}
	return names
}

func sliceNames(raw *snapshot.RawKubernetesObjects) []string {
	var names []string
	for _, slice := range raw.ResourceSlices {
		names = append(names, slice.Namespace+"/"+slice.Name)
	}
	return names
}

func classNames(raw *snapshot.RawKubernetesObjects) []string {
	var names []string
	for _, class := range raw.DeviceClasses {
		names = append(names, class.Name)
	}
	return names
}
