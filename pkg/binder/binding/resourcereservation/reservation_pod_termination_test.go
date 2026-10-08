// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resourcereservation

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

// fakeWatchSingleEvent emits one event and then stays open, like a real watch on a pod
// that is never annotated with a GPU index.
type fakeWatchSingleEvent struct {
	event   watch.Event
	channel chan watch.Event
}

func (w *fakeWatchSingleEvent) Stop() {}

func (w *fakeWatchSingleEvent) ResultChan() <-chan watch.Event {
	if w.channel == nil {
		w.channel = make(chan watch.Event, 1)
		w.channel <- w.event
	}
	return w.channel
}

var _ = Describe("Reservation pod terminates before a GPU is allocated", func() {
	const (
		nodeName          = "node-1"
		gpuGroup          = "0a8231f4-e9fc-434b-bf77-a1aebd41b442"
		allocationTimeout = 30 * time.Second
	)

	newServiceWithLongTimeout := func(client runtimeClient.WithWatch) *service {
		return NewService(false, client, "", allocationTimeout,
			resourceReservationNameSpace, resourceReservationServiceAccount, resourceReservationAppLabelValue,
			scalingPodsNamespace, "", nil, nil, nil, false)
	}

	reservationPodWithStatus := func(status v1.PodStatus) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: resourceReservationNameSpace,
				Name:      reservationPodName(nodeName, gpuGroup),
			},
			Status: status,
		}
	}

	for testName, event := range map[string]watch.Event{
		"kubelet rejects the pod (UnexpectedAdmissionError)": {
			Type: watch.Modified,
			Object: reservationPodWithStatus(v1.PodStatus{
				Phase:   v1.PodFailed,
				Reason:  "UnexpectedAdmissionError",
				Message: "Pod was rejected: Allocate failed due to requested number of devices unavailable for nvidia.com/gpu. Requested: 1, Available: 0, which is unexpected",
			}),
		},
		"pod completes without an allocation": {
			Type:   watch.Modified,
			Object: reservationPodWithStatus(v1.PodStatus{Phase: v1.PodSucceeded}),
		},
		"pod is deleted while waiting": {
			Type:   watch.Deleted,
			Object: reservationPodWithStatus(v1.PodStatus{Phase: v1.PodPending}),
		},
	} {
		event := event
		It("returns before the allocation timeout when "+testName, func() {
			fractionPod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "fraction-pod"},
			}
			clientWithObjs := fake.NewClientBuilder().WithScheme(testScheme).
				WithRuntimeObjects([]runtime.Object{fractionPod}...).
				WithIndex(&v1.Pod{}, "spec.nodeName", nodeNameIndexer).Build()
			fakeClient := interceptor.NewClient(clientWithObjs, interceptor.Funcs{
				Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
					return &fakeWatchSingleEvent{event: event}, nil
				},
			})
			rsc := newServiceWithLongTimeout(fakeClient)

			started := time.Now()
			gpuIndex, err := rsc.ReserveGpuDevice(context.TODO(), fractionPod, nodeName, schedulingv1alpha2.FractionalGpuGroup{ID: gpuGroup})

			Expect(time.Since(started)).To(BeNumerically("<", allocationTimeout/10))
			Expect(gpuIndex).To(Equal(unknownGpuIndicator))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed waiting for GPU reservation pod to allocate"))

			pods := &v1.PodList{}
			Expect(fakeClient.List(context.TODO(), pods, runtimeClient.InNamespace(resourceReservationNameSpace))).To(Succeed())
			Expect(pods.Items).To(BeEmpty(), "the terminated reservation pod is deleted so the next bind starts clean")
		})
	}
})
