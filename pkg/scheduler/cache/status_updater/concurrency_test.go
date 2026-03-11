// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	fakecorev1 "k8s.io/client-go/kubernetes/typed/core/v1/fake"
	faketesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	kubeaischedfake "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	fakeschedulingv2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/typed/scheduling/v2alpha2/fake"
	enginev2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

const (
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestConcurrency(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Status Updater Concurrency Suite")
}

var _ = Describe("Status Updater Concurrency - large scale: increase queue size", func() {
	var (
		kubeClient        *fake.Clientset
		kubeAiSchedClient *kubeaischedfake.Clientset
		statusUpdater     *defaultStatusUpdater
	)
	BeforeEach(func() {
		kubeClient = fake.NewSimpleClientset()
		kubeAiSchedClient = kubeaischedfake.NewSimpleClientset()
		recorder := record.NewFakeRecorder(100)
		statusUpdater = New(kubeClient, kubeAiSchedClient, recorder, 4, false,
			nodePoolLabelKey)
	})

	It("should increase queue size", func() {
		wg := sync.WaitGroup{}
		signalCh := make(chan struct{})
		kubeClient.CoreV1().(*fakecorev1.FakeCoreV1).PrependReactor("patch", "pods", func(action faketesting.Action) (handled bool, ret runtime.Object, err error) {
			<-signalCh
			wg.Done()
			return true, nil, nil
		})
		stopCh := make(chan struct{})
		statusUpdater.Run(stopCh)
		defer close(stopCh)

		for i := 0; i < 2000; i++ {
			wg.Add(1)
			statusUpdater.updatePodCondition(
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-" + strconv.Itoa(i),
						Namespace: "default",
					},
				},
				&v1.PodCondition{
					Type:   v1.PodScheduled,
					Status: v1.ConditionTrue,
				},
			)
		}

		close(signalCh)
		wg.Wait()
	})

	It("updatePodGroup - No retry after conflict error", func() {
		updateStatusCalls := 0
		patchCalls := 0

		// Set up reactor to return conflict error on UpdateStatus calls
		kubeAiSchedClient.SchedulingV2alpha2().(*fakeschedulingv2alpha2.FakeSchedulingV2alpha2).PrependReactor(
			"update", "podgroups", func(action faketesting.Action) (handled bool, ret runtime.Object, err error) {
				if updateAction, ok := action.(faketesting.UpdateAction); ok {
					if updateAction.GetSubresource() == "status" {
						updateStatusCalls++
						// Return a conflict error to simulate resource version mismatch
						return true, nil, apierrors.NewConflict(
							schema.GroupResource{Group: "scheduling.run.ai", Resource: "podgroups"},
							"test-pg",
							errors.New("the object has been modified; please apply your changes to the latest version"),
						)
					}
				}
				return false, nil, nil
			},
		)

		// Track patch calls separately
		kubeAiSchedClient.SchedulingV2alpha2().(*fakeschedulingv2alpha2.FakeSchedulingV2alpha2).PrependReactor(
			"patch", "podgroups", func(action faketesting.Action) (handled bool, ret runtime.Object, err error) {
				patchCalls++
				return false, nil, nil
			},
		)

		job := &enginev2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pg",
				Namespace: "test-ns",
				UID:       "test-uid",
			},
			Status: enginev2alpha2.PodGroupStatus{
				SchedulingConditions: []enginev2alpha2.SchedulingCondition{
					{
						TransitionID: "1",
						Type:         enginev2alpha2.UnschedulableOnNodePool,
						NodePool:     "test",
						Reason:       "test",
						Message:      "test",
						Status:       v1.ConditionTrue,
					},
				},
			},
		}

		key := statusUpdater.keyForPodGroupPayload(job.Name, job.Namespace, job.UID)
		updateData := &inflightUpdate{
			object:       job,
			patchData:    nil, // No patch data, only status update
			updateStatus: true,
			subResources: nil,
		}

		// Store the inflight update
		statusUpdater.inFlightPodGroups.Store(key, updateData)

		statusUpdater.Run(make(chan struct{}))

		// Call updatePodGroup directly
		ctx := context.Background()
		statusUpdater.updatePodGroup(ctx, key, updateData)

		// Verify UpdateStatus was called once
		Expect(updateStatusCalls).To(Equal(1), "UpdateStatus should be called once")

		// Verify Patch was not called (no patchData provided)
		Expect(patchCalls).To(Equal(0), "Patch should not be called when no patchData is provided")

		// Verify it's not in the applied cache (since the update failed with conflict)
		_, appliedExists := statusUpdater.appliedPodGroupUpdates.Load(key)
		Expect(appliedExists).To(BeFalse(), "Update should not be in applied cache after conflict error")

		// The key behavior: Verify the queue is empty (no retry was queued)
		// When a conflict error occurs, the function returns early without calling pushToUpdateQueue
		select {
		case <-statusUpdater.updateQueueOut:
			Fail("Update queue should be empty - no retry should be queued for conflict errors")
		case <-time.After(100 * time.Millisecond):
			// Expected - queue is empty, meaning no retry was scheduled
		}
	})
})
