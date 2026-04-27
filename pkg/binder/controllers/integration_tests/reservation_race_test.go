// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	scheudlingv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/common"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This test reproduces a race condition where the binder's resource reservation
// sync logic prematurely deletes GPU reservation pods during concurrent binding.
//
// The race:
//  1. Pod 1 binds to a GPU group, creating a reservation pod and getting a GPU
//     group label. The label is written to the API server but may not have
//     propagated to the informer cache yet.
//  2. A concurrent sync (triggered by Pod 2 binding, BindRequest deletion, or
//     pod completion) calls SyncForGpuGroup, which lists pods by GPU group label
//     from the cache. Due to cache lag, it doesn't see Pod 1's label.
//  3. The sync sees the reservation pod but no fraction pods → deletes the
//     reservation pod.
//
// This test creates the preconditions (reservation pod + active BindRequest but
// no labeled fraction pod) and calls SyncForGpuGroup to demonstrate that the
// reservation pod is deleted.
var _ = Describe("Reservation pod race condition", Ordered, func() {
	const (
		fracNamespaceName = "frac-test-ns"
		fracNodeName      = "frac-test-node"
		gpuGroup          = "node1-gpu0-group"
	)

	BeforeAll(func() {
		ns := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: fracNamespaceName},
		}
		Expect(k8sClient.Create(context.Background(), ns)).To(Succeed())

		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: fracNodeName},
		}
		Expect(k8sClient.Create(context.Background(), node)).To(Succeed())

		reservationNs := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: resourceReservationNameSpace},
		}
		err := k8sClient.Create(context.Background(), reservationNs)
		if err != nil {
			// Namespace may already exist from suite setup
			Expect(client.IgnoreAlreadyExists(err)).To(Succeed())
		}
	})

	// This test verifies that SyncForGpuGroup preserves reservation pods when
	// active BindRequests exist for the GPU group, even if no fraction pods are
	// visible (simulating cache lag).
	It("should preserve reservation pod when sync runs with active BindRequest and no visible fraction pods", func() {
		ctx := context.Background()

		// Step 1: Create a reservation pod (as if ReserveGpuDevice just created it)
		reservationPod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("gpu-reservation-%s-test1", fracNodeName),
				Namespace: resourceReservationNameSpace,
				Labels: map[string]string{
					constants.AppLabelName: resourceReservationAppLabelValue,
					constants.GPUGroup:     gpuGroup,
				},
			},
			Spec: v1.PodSpec{
				NodeName:           fracNodeName,
				ServiceAccountName: resourceReservationServiceAccount,
				Containers: []v1.Container{
					{
						Name:  "resource-reservation",
						Image: "test-image",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, reservationPod)).To(Succeed())

		// Step 2: Create a fraction pod WITHOUT the GPU group label.
		// This simulates the state after ReserveGpuDevice created the
		// reservation pod but the label patch on the fraction pod hasn't
		// propagated to the cache yet.
		fractionPod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fraction-pod-1",
				Namespace: fracNamespaceName,
				// NOTE: No GPU group label - simulates cache lag
			},
			Spec: v1.PodSpec{
				// NOTE: No NodeName - pod not yet bound, simulates in-flight binding
				SchedulerName: "kai-scheduler",
				Containers: []v1.Container{
					{
						Name:  "worker",
						Image: "test-image",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, fractionPod)).To(Succeed())

		// Step 3: Create an active BindRequest for this GPU group.
		// This represents the in-flight binding that is about to label the pod.
		bindReq := &scheudlingv1alpha2.BindRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "frac-bind-request-1",
				Namespace: fracNamespaceName,
			},
			Spec: scheudlingv1alpha2.BindRequestSpec{
				PodName:              fractionPod.Name,
				SelectedNode:         fracNodeName,
				ReceivedResourceType: common.ReceivedTypeFraction,
				SelectedGPUGroups:    []string{gpuGroup},
				ReceivedGPU:          &scheudlingv1alpha2.ReceivedGPU{Count: 1, Portion: "0.5"},
			},
		}
		Expect(k8sClient.Create(ctx, bindReq)).To(Succeed())

		// Step 4: Call SyncForGpuGroup - this simulates what happens when
		// a concurrent binding triggers a sync.
		err := rrs.SyncForGpuGroup(ctx, gpuGroup)
		Expect(err).To(Succeed())

		// Step 5: Check if the reservation pod survived.
		// After fix: reservation pod should be preserved because an active
		// BindRequest exists for this GPU group.
		resultPod := &v1.Pod{}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(reservationPod), resultPod)
		Expect(err).NotTo(HaveOccurred(),
			"Reservation pod should be preserved when an active BindRequest exists")

		// Cleanup
		_ = k8sClient.Delete(ctx, fractionPod)
		_ = k8sClient.Delete(ctx, bindReq)
		_ = k8sClient.Delete(ctx, reservationPod)
	})

	AfterAll(func() {
		ctx := context.Background()
		_ = k8sClient.DeleteAllOf(ctx, &v1.Pod{}, client.InNamespace(fracNamespaceName))
		_ = k8sClient.DeleteAllOf(ctx, &v1.Pod{}, client.InNamespace(resourceReservationNameSpace))
		_ = k8sClient.DeleteAllOf(ctx, &scheudlingv1alpha2.BindRequest{}, client.InNamespace(fracNamespaceName))
		_ = k8sClient.Delete(ctx, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: fracNodeName}})
	})
})
