// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mock_resourcereservation "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/api/constants"
)

var _ = Describe("Pod Controller", func() {
	const schedulerName = "kai-scheduler"

	var (
		resourceReservation *mock_resourcereservation.MockInterface
		reconciler          *PodReconciler
		queue               workqueue.TypedRateLimitingInterface[reconcile.Request]
		completedPod        *corev1.Pod
		pendingPod          *corev1.Pod
	)

	BeforeEach(func() {
		resourceReservation = mock_resourcereservation.NewMockInterface(gomock.NewController(GinkgoT()))
		reconciler = &PodReconciler{
			ResourceReservation: resourceReservation,
			SchedulerName:       schedulerName,
		}
		queue = workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
		DeferCleanup(queue.ShutDown)

		completedPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod",
				Namespace: "default",
				Labels: map[string]string{
					constants.GPUGroup: "group",
				},
			},
			Spec:   corev1.PodSpec{SchedulerName: schedulerName},
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
		}
		pendingPod = completedPod.DeepCopy()
		pendingPod.Status.Phase = corev1.PodPending
	})

	It("syncs reservations for completed and deleted Pods", func() {
		resourceReservation.EXPECT().SyncForGpuGroup(gomock.Any(), "group").Return(nil).Times(2)

		handlers := reconciler.eventHandlers()
		handlers.UpdateFunc(context.TODO(), event.UpdateEvent{
			ObjectOld: pendingPod,
			ObjectNew: completedPod,
		}, queue)
		handlers.DeleteFunc(context.TODO(), event.DeleteEvent{Object: completedPod}, queue)
	})

	It("does not sync reservations while the Pod is still running", func() {
		runningPod := completedPod.DeepCopy()
		runningPod.Status.Phase = corev1.PodRunning

		reconciler.eventHandlers().UpdateFunc(context.TODO(), event.UpdateEvent{
			ObjectOld: pendingPod,
			ObjectNew: runningPod,
		}, queue)
	})

	It("ignores Pods of another scheduler", func() {
		otherSchedulerPod := completedPod.DeepCopy()
		otherSchedulerPod.Spec.SchedulerName = "default-scheduler"

		handlers := reconciler.eventHandlers()
		handlers.UpdateFunc(context.TODO(), event.UpdateEvent{
			ObjectOld: pendingPod,
			ObjectNew: otherSchedulerPod,
		}, queue)
		handlers.DeleteFunc(context.TODO(), event.DeleteEvent{Object: otherSchedulerPod}, queue)

		Expect(queue.Len()).To(Equal(0))
	})
})
