// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
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
