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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"

	resourcereservationmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

var _ = Describe("Pod Controller", func() {
	It("syncs reservations for completed and deleted Pods without enqueueing reconciles", func() {
		resourceReservation := resourcereservationmock.NewMockInterface(gomock.NewController(GinkgoT()))
		reconciler := &PodReconciler{
			ResourceReservation: resourceReservation,
			SchedulerName:       "kai-scheduler",
		}
		queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
		DeferCleanup(queue.ShutDown)

		completedPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod",
				Namespace: "default",
				Labels: map[string]string{
					constants.GPUGroup: "group",
				},
			},
			Spec:   corev1.PodSpec{SchedulerName: "kai-scheduler"},
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
		}
		pendingPod := completedPod.DeepCopy()
		pendingPod.Status.Phase = corev1.PodPending

		resourceReservation.EXPECT().SyncForGpuGroup(gomock.Any(), "group").Return(nil).Times(2)
		handlers := reconciler.eventHandlers()
		handlers.UpdateFunc(context.TODO(), event.UpdateEvent{
			ObjectOld: pendingPod,
			ObjectNew: completedPod,
		}, queue)
		handlers.DeleteFunc(context.TODO(), event.DeleteEvent{Object: completedPod}, queue)

		Expect(queue.Len()).To(Equal(0))
	})
})
