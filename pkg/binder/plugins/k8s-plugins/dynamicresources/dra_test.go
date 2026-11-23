// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestDynamicResources(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Binder DynamicResources Suite")
}

var _ = Describe("dynamicResourcesPlugin extended-resource rollback", func() {
	const (
		namespace = "test-ns"
		podName   = "test-pod"
		podUID    = "pod-uid-123"
	)

	var (
		ctx     context.Context
		client  *fake.Clientset
		plugin  *dynamicResourcesPlugin
		pod     *corev1.Pod
		request *v1alpha2.BindRequest
	)

	listClaims := func() []resourceapi.ResourceClaim {
		list, err := client.ResourceV1().ResourceClaims(namespace).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		return list.Items
	}

	getPod := func() *corev1.Pod {
		p, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		return p
	}

	BeforeEach(func() {
		ctx = context.Background()
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				UID:       types.UID(podUID),
			},
		}
		client = fake.NewSimpleClientset(pod)
		// The fake tracker does not populate Name from GenerateName; do it here so the
		// claim created during Bind is retrievable by the rollback path.
		client.PrependReactor("create", "resourceclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
			claim := action.(k8stesting.CreateAction).GetObject().(*resourceapi.ResourceClaim)
			if claim.Name == "" && claim.GenerateName != "" {
				claim.Name = claim.GenerateName + "generated"
			}
			return false, claim, nil
		})

		plugin = &dynamicResourcesPlugin{client: client, bindTimeout: 5}
		request = &v1alpha2.BindRequest{
			Spec: v1alpha2.BindRequestSpec{
				ExtendedResourceClaimAllocation: &v1alpha2.ExtendedResourceClaimAllocation{
					Allocation:     &resourceapi.AllocationResult{},
					DeviceRequests: []resourceapi.DeviceRequest{{Name: "req0"}},
					ContainerMappings: []corev1.ContainerExtendedResourceRequest{
						{ContainerName: "main", ResourceName: "nvidia.com/gpu", RequestName: "req0"},
					},
				},
			},
		}
	})

	It("rolls back the created claim and clears pod status after a later bind failure", func() {
		Expect(plugin.Bind(ctx, pod, request, nil)).To(Succeed())

		claims := listClaims()
		Expect(claims).To(HaveLen(1))
		Expect(claims[0].Annotations).To(HaveKeyWithValue(resourceapi.ExtendedResourceClaimAnnotation, "true"))
		Expect(getPod().Status.ExtendedResourceClaimStatus).NotTo(BeNil())

		// A later plugin's Bind fails: the framework rolls back by calling UnAllocate.
		plugin.UnAllocate(ctx, pod, "node", nil)

		Expect(listClaims()).To(BeEmpty())
		Expect(getPod().Status.ExtendedResourceClaimStatus).To(BeNil())
	})

	It("is idempotent when rollback runs more than once", func() {
		Expect(plugin.Bind(ctx, pod, request, nil)).To(Succeed())
		Expect(listClaims()).To(HaveLen(1))

		plugin.UnAllocate(ctx, pod, "node", nil)
		Expect(listClaims()).To(BeEmpty())
		Expect(getPod().Status.ExtendedResourceClaimStatus).To(BeNil())

		// Second rollback must be a no-op: the claim is already gone and status already nil.
		Expect(func() { plugin.UnAllocate(ctx, pod, "node", nil) }).NotTo(Panic())
		Expect(listClaims()).To(BeEmpty())
		Expect(getPod().Status.ExtendedResourceClaimStatus).To(BeNil())
	})

	It("does nothing when there is no extended-resource claim to roll back", func() {
		plugin.UnAllocate(ctx, pod, "node", nil)
		Expect(listClaims()).To(BeEmpty())
		Expect(getPod().Status.ExtendedResourceClaimStatus).To(BeNil())
	})
})

var _ = Describe("dynamicResourcesPlugin standalone ResourceClaim bind", func() {
	const (
		namespace = "test-ns"
		podName   = "test-pod"
		podUID    = "pod-uid-456"
		podClaim  = "gpu-claim"
	)

	var (
		ctx     context.Context
		client  *fake.Clientset
		plugin  *dynamicResourcesPlugin
		pod     *corev1.Pod
		claim   *resourceapi.ResourceClaim
		request *v1alpha2.BindRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		claim = &resourceapi.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "standalone-claim",
				Namespace: namespace,
			},
			Spec: resourceapi.ResourceClaimSpec{
				Devices: resourceapi.DeviceClaim{
					Requests: []resourceapi.DeviceRequest{{Name: "req0"}},
				},
			},
		}
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				UID:       types.UID(podUID),
			},
			Spec: corev1.PodSpec{
				ResourceClaims: []corev1.PodResourceClaim{{
					Name:              podClaim,
					ResourceClaimName: ptr.To(claim.Name),
				}},
			},
		}
		client = fake.NewSimpleClientset(claim, pod)
		plugin = &dynamicResourcesPlugin{client: client, bindTimeout: 5}
		request = &v1alpha2.BindRequest{
			Spec: v1alpha2.BindRequestSpec{
				ResourceClaimAllocations: []v1alpha2.ResourceClaimAllocation{{
					Name:       podClaim,
					Allocation: &resourceapi.AllocationResult{},
				}},
			},
		}
	})

	getClaim := func() *resourceapi.ResourceClaim {
		c, err := client.ResourceV1().ResourceClaims(namespace).Get(ctx, claim.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		return c
	}

	It("adds the delete-protection finalizer when binding a standalone claim", func() {
		Expect(plugin.Bind(ctx, pod, request, nil)).To(Succeed())

		var updates []k8stesting.UpdateAction
		for _, action := range client.Actions() {
			if action.Matches("update", "resourceclaims") {
				updates = append(updates, action.(k8stesting.UpdateAction))
			}
		}
		Expect(updates).To(HaveLen(2))
		Expect(updates[0].GetSubresource()).To(BeEmpty())
		metadataUpdate := updates[0].GetObject().(*resourceapi.ResourceClaim)
		Expect(metadataUpdate.Finalizers).To(ContainElement(resourceapi.Finalizer))
		Expect(metadataUpdate.Status.Allocation).To(BeNil())
		Expect(metadataUpdate.Status.ReservedFor).To(BeEmpty())
		Expect(updates[1].GetSubresource()).To(Equal("status"))
		statusUpdate := updates[1].GetObject().(*resourceapi.ResourceClaim)
		Expect(statusUpdate.Status.Allocation).NotTo(BeNil())
		Expect(statusUpdate.Status.ReservedFor).To(HaveLen(1))

		bound := getClaim()
		Expect(bound.Finalizers).To(ContainElement(resourceapi.Finalizer))
		Expect(bound.Status.Allocation).NotTo(BeNil())
		Expect(bound.Status.ReservedFor).To(ContainElement(
			resourceapi.ResourceClaimConsumerReference{
				APIGroup: "", Resource: "pods", Name: podName, UID: types.UID(podUID),
			},
		))
	})

	It("does not update status when adding the finalizer fails", func() {
		client.PrependReactor("update", "resourceclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() == "" {
				return true, nil, errors.New("finalizer update failed")
			}
			return false, nil, nil
		})

		Expect(plugin.Bind(ctx, pod, request, nil)).To(MatchError(ContainSubstring("finalizer update failed")))
		for _, action := range client.Actions() {
			if action.Matches("update", "resourceclaims") {
				Expect(action.GetSubresource()).To(BeEmpty())
			}
		}
		bound := getClaim()
		Expect(bound.Finalizers).NotTo(ContainElement(resourceapi.Finalizer))
		Expect(bound.Status.Allocation).To(BeNil())
		Expect(bound.Status.ReservedFor).To(BeEmpty())
	})

	It("does not duplicate the finalizer on repeated binds", func() {
		Expect(plugin.Bind(ctx, pod, request, nil)).To(Succeed())
		Expect(plugin.Bind(ctx, pod, request, nil)).To(Succeed())

		bound := getClaim()
		count := 0
		for _, f := range bound.Finalizers {
			if f == resourceapi.Finalizer {
				count++
			}
		}
		Expect(count).To(Equal(1))
	})

	It("does not duplicate the finalizer when UpdateStatus conflicts", func() {
		// A 409 on the first UpdateStatus forces RetryOnConflict to re-read the
		// claim and re-run the append logic; the slices.Contains guard must
		// prevent a duplicate finalizer on that second pass.
		var statusUpdates int
		client.PrependReactor("update", "resourceclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() != "status" {
				return false, nil, nil
			}
			statusUpdates++
			if statusUpdates == 1 {
				return true, nil, apierrors.NewConflict(
					schema.GroupResource{Resource: "resourceclaims"},
					claim.Name,
					errors.New("conflict"),
				)
			}
			return false, nil, nil
		})

		Expect(plugin.Bind(ctx, pod, request, nil)).To(Succeed())
		Expect(statusUpdates).To(BeNumerically(">", 1))

		var metadataUpdates int
		for _, action := range client.Actions() {
			if action.Matches("update", "resourceclaims") && action.GetSubresource() == "" {
				metadataUpdates++
			}
		}
		Expect(metadataUpdates).To(Equal(1))

		bound := getClaim()
		count := 0
		for _, f := range bound.Finalizers {
			if f == resourceapi.Finalizer {
				count++
			}
		}
		Expect(count).To(Equal(1))
	})
})
