// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	faketesting "k8s.io/client-go/testing"

	kaifake "github.com/kai-scheduler/api/client/clientset/versioned/fake"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestPodGroupCompletionPreservesNewerUpdate(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "transient error", err: errors.New("temporary failure")},
		{name: "terminal error", err: apierrors.NewNotFound(schema.GroupResource{Resource: "podgroups"}, "job")},
	} {
		t.Run(test.name, func(t *testing.T) {
			pg := &enginev2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: "test", UID: "uid"}}
			client := kaifake.NewSimpleClientset(pg)
			su := &defaultStatusUpdater{kaiClient: client, nodePoolLabelKey: nodePoolLabelKey}
			su.statusSchemaVerified.Store(true)
			key := su.keyForPodGroupPayload(pg.Name, pg.Namespace, pg.UID)
			original := &inflightUpdate{object: pg, updateStatus: true}
			newest := &inflightUpdate{object: pg.DeepCopy(), updateStatus: true}
			su.inFlightPodGroups.Store(key, original)
			client.PrependReactor("patch", "podgroups", func(faketesting.Action) (bool, runtime.Object, error) {
				su.inFlightPodGroups.Store(key, newest)
				return true, pg.DeepCopy(), test.err
			})
			su.updatePodGroup(context.Background(), key, original)
			pending, found := su.inFlightPodGroups.Load(key)
			require.True(t, found)
			require.Same(t, newest, pending)
			_, found = su.appliedPodGroupUpdates.Load(key)
			require.False(t, found)
		})
	}
}

func TestSyncOwnSchedulingStatus(t *testing.T) {
	for _, clear := range []bool{false, true} {
		name := "update"
		if clear {
			name = "clear"
		}
		t.Run(name, func(t *testing.T) {
			own := enginev2alpha2.SchedulingCondition{NodePool: "pool-a", TransitionID: "1"}
			other := enginev2alpha2.SchedulingCondition{NodePool: "pool-b", TransitionID: "99"}
			snapshot := &enginev2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{nodePoolLabelKey: "pool-a"}},
				Status: enginev2alpha2.PodGroupStatus{
					SchedulingConditions: []enginev2alpha2.SchedulingCondition{own, other},
					ResourcesStatus:      enginev2alpha2.PodGroupResourcesStatus{Requested: v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")}},
				},
			}
			pending := snapshot.DeepCopy()
			pending.Status.SchedulingConditions = nil
			if !clear {
				own.TransitionID = "2"
				pending.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{own}
			}
			pending.Status.SchedulingState = &enginev2alpha2.PodGroupSchedulingState{CorePods: []string{"core"}}
			resources := *snapshot.Status.ResourcesStatus.DeepCopy()
			su := &defaultStatusUpdater{nodePoolLabelKey: nodePoolLabelKey}
			require.Equal(t, snapshotStatusIsOlder, su.syncPodGroup(pending, snapshot))
			require.Equal(t, &other, conditionForNodePool(snapshot, "pool-b"))
			require.Equal(t, resources, snapshot.Status.ResourcesStatus)
			require.Equal(t, []string{"core"}, snapshot.Status.SchedulingState.CorePods)
			if clear {
				require.Nil(t, conditionForNodePool(snapshot, "pool-a"))
			} else {
				require.Equal(t, "2", conditionForNodePool(snapshot, "pool-a").TransitionID)
			}
		})
	}
}

func TestSchedulingReasonChangeIsPublished(t *testing.T) {
	old := enginev2alpha2.SchedulingCondition{NodePool: "default", TransitionID: "1", Reasons: enginev2alpha2.UnschedulableExplanations{{Message: "old explanation"}}}
	pg := &enginev2alpha2.PodGroup{Status: enginev2alpha2.PodGroupStatus{SchedulingConditions: []enginev2alpha2.SchedulingCondition{old}}}
	updated := old.DeepCopy()
	updated.Reasons[0].Message = "new explanation"
	require.True(t, setPodGroupSchedulingCondition(pg, updated))
	require.Equal(t, "new explanation", pg.Status.SchedulingConditions[0].Reasons[0].Message)
}

func TestSchedulingInputsDistinguishDefaultPoolRouting(t *testing.T) {
	original := &enginev2alpha2.PodGroup{}
	routed := original.DeepCopy()
	routed.Labels = map[string]string{nodePoolLabelKey: "default"}
	require.False(t, schedulingInputsEqual(original, routed))
}
