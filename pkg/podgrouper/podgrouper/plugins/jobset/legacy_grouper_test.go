// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Tests for the legacy JobSet podgrouper code path. The legacy path preserves
// the previous handler behavior for JobSets that already have an old-shape
// PodGroup in the cluster.
//
// Deprecated: remove this entire file in v0.17.

package jobset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	schedulingv2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func legacyPodGroup(name, namespace string) *schedulingv2.PodGroup {
	return &schedulingv2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       schedulingv2.PodGroupSpec{MinMember: ptr.To(int32(1))},
	}
}

func TestLegacyDetection_FlatPodGroupTriggersOldHandler(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 2, 4),
	})
	setStartupPolicy(js, startupPolicyOrderAnyOrder)
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	existing := legacyPodGroup("pg-js-uid", "default")

	meta, err := newJobSetGrouper(t, existing).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)

	assert.Equal(t, "pg-js-uid", meta.Name)
	assert.Nil(t, meta.MinSubGroup, "legacy mode must not set MinSubGroup")
	assert.Empty(t, meta.SubGroups, "legacy mode must not emit SubGroups")
	assert.Equal(t, int32(1), meta.MinAvailable, "legacy default MinAvailable=1")
}

func TestLegacyDetection_InOrderPerReplicatedJobPodGroup(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("leader", 1, 1),
		replicatedJob("worker", 2, 4),
	})
	setStartupPolicy(js, startupPolicyOrderInOrder)
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	// Only the leader's legacy PG exists; this is enough to trigger legacy mode
	// and the worker pod should be routed to its own per-rj legacy PG name.
	existing := legacyPodGroup("pg-js-uid-leader", "default")

	meta, err := newJobSetGrouper(t, existing).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)

	assert.Equal(t, "pg-js-uid-worker", meta.Name, "InOrder legacy name uses per-rj suffix")
	assert.Nil(t, meta.MinSubGroup)
	assert.Empty(t, meta.SubGroups)
}

func TestLegacyDetection_AnnotationOverridesLegacyMinAvailable(t *testing.T) {
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 1, 4),
	})
	js.SetAnnotations(map[string]string{constants.MinMemberOverrideKey: "3"})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	meta, err := newJobSetGrouper(t, legacyPodGroup("pg-js-uid", "default")).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	assert.Equal(t, int32(3), meta.MinAvailable)
	assert.Nil(t, meta.MinSubGroup)
}

func TestLegacyDetection_PodGroupWithSubGroupsDoesNotTriggerLegacy(t *testing.T) {
	// A PG that was created by the new handler (has SubGroups) must NOT trigger
	// legacy mode; the new shape continues to be emitted.
	js := baseJobSet("js", "default", "uid", []map[string]interface{}{
		replicatedJob("worker", 1, 4),
	})
	pod := podWithJobSetLabels("p", "default", "worker", "0")

	migrated := &schedulingv2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg-js-uid", Namespace: "default"},
		Spec: schedulingv2.PodGroupSpec{
			MinSubGroup: ptr.To(int32(1)),
			SubGroups: []schedulingv2.SubGroup{
				{Name: "worker", MinSubGroup: ptr.To(int32(1))},
				{Name: "worker-replica-0", MinMember: ptr.To(int32(4)), Parent: ptr.To("worker")},
			},
		},
	}

	meta, err := newJobSetGrouper(t, migrated).GetPodGroupMetadata(js, pod)
	require.NoError(t, err)
	require.NotNil(t, meta.MinSubGroup)
	assert.Equal(t, int32(1), *meta.MinSubGroup)
	assert.NotEmpty(t, meta.SubGroups, "should continue to emit new-shape SubGroups")
}
