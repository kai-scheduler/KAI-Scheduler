// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestDRA(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DRA GPU Extraction Suite")
}

var _ = Describe("DRA GPU Extraction", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		testScheme := scheme.Scheme
		Expect(resourceapi.AddToScheme(testScheme)).To(Succeed())
		fakeClient = fake.NewClientBuilder().WithScheme(testScheme).Build()
	})

	Describe("countGPUDevicesFromClaim", func() {
		Context("when claim has GPU device requests", func() {
			It("should count devices with ExactCount mode and explicit count", func() {
				claim := &resourceapi.ResourceClaim{
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           2,
									},
								},
							},
						},
					},
				}

				count := countGPUDevicesFromClaim(claim)
				Expect(count).To(Equal(int64(2)))
			})

			It("should default to 1 when ExactCount mode has no count specified", func() {
				claim := &resourceapi.ResourceClaim{
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           0,
									},
								},
							},
						},
					},
				}

				count := countGPUDevicesFromClaim(claim)
				Expect(count).To(Equal(int64(1)))
			})

			It("should count devices with AllocationModeAll", func() {
				claim := &resourceapi.ResourceClaim{
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeAll,
									},
								},
							},
						},
					},
				}

				count := countGPUDevicesFromClaim(claim)
				Expect(count).To(Equal(int64(1))) // Conservative estimate for "All" mode
			})

			It("should sum multiple GPU device requests", func() {
				claim := &resourceapi.ResourceClaim{
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           2,
									},
								},
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           3,
									},
								},
							},
						},
					},
				}

				count := countGPUDevicesFromClaim(claim)
				Expect(count).To(Equal(int64(5)))
			})

			It("should ignore non-GPU device requests", func() {
				claim := &resourceapi.ResourceClaim{
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: "other-device-class",
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           5,
									},
								},
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           2,
									},
								},
							},
						},
					},
				}

				count := countGPUDevicesFromClaim(claim)
				Expect(count).To(Equal(int64(2))) // Only GPU devices counted
			})
		})

		Context("when claim has no GPU device requests", func() {
			It("should return 0 for empty claim", func() {
				claim := &resourceapi.ResourceClaim{
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{},
						},
					},
				}

				count := countGPUDevicesFromClaim(claim)
				Expect(count).To(Equal(int64(0)))
			})

			It("should return 0 for claim with no Exactly field", func() {
				claim := &resourceapi.ResourceClaim{
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: nil,
								},
							},
						},
					},
				}

				count := countGPUDevicesFromClaim(claim)
				Expect(count).To(Equal(int64(0)))
			})

			It("should skip unknown allocation modes", func() {
				claim := &resourceapi.ResourceClaim{
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  "UnknownMode",
										Count:           5,
									},
								},
							},
						},
					},
				}

				count := countGPUDevicesFromClaim(claim)
				Expect(count).To(Equal(int64(0))) // Unknown mode skipped
			})
		})
	})

	Describe("ExtractDRAGPUResources", func() {
		Context("when pod has no ResourceClaims", func() {
			It("should return empty ResourceList", func() {
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeEmpty())
			})
		})

		Context("when pod has ResourceClaims with GPU devices", func() {
			It("should extract GPU resources from a single claim", func() {
				claim := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-claim-1",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           2,
									},
								},
							},
						},
					},
				}

				Expect(fakeClient.Create(ctx, claim)).To(Succeed())

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:              "gpu-claim",
								ResourceClaimName: ptr.To("gpu-claim-1"),
							},
						},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).NotTo(HaveOccurred())
				gpuQuantity, exists := result[constants.NvidiaGpuResource]
				Expect(exists).To(BeTrue(), "result should contain GPU resource")
				Expect(gpuQuantity.Value()).To(Equal(int64(2)))
			})

			It("should aggregate GPU resources from multiple claims", func() {
				claim1 := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-claim-1",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           2,
									},
								},
							},
						},
					},
				}

				claim2 := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-claim-2",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           3,
									},
								},
							},
						},
					},
				}

				Expect(fakeClient.Create(ctx, claim1)).To(Succeed())
				Expect(fakeClient.Create(ctx, claim2)).To(Succeed())

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:              "gpu-claim-1",
								ResourceClaimName: ptr.To("gpu-claim-1"),
							},
							{
								Name:              "gpu-claim-2",
								ResourceClaimName: ptr.To("gpu-claim-2"),
							},
						},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).NotTo(HaveOccurred())
				gpuQuantity, exists := result[constants.NvidiaGpuResource]
				Expect(exists).To(BeTrue(), "result should contain GPU resource")
				Expect(gpuQuantity.Value()).To(Equal(int64(5)))
			})

			It("should handle ResourceClaimTemplate references", func() {
				claim := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-claim-from-template",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           1,
									},
								},
							},
						},
					},
				}

				Expect(fakeClient.Create(ctx, claim)).To(Succeed())

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:                      "gpu-claim",
								ResourceClaimTemplateName: ptr.To("gpu-template"),
							},
						},
					},
					Status: v1.PodStatus{
						ResourceClaimStatuses: []v1.PodResourceClaimStatus{
							{
								Name:              "gpu-claim",
								ResourceClaimName: ptr.To("gpu-claim-from-template"),
							},
						},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).NotTo(HaveOccurred())
				gpuQuantity, exists := result[constants.NvidiaGpuResource]
				Expect(exists).To(BeTrue(), "result should contain GPU resource")
				Expect(gpuQuantity.Value()).To(Equal(int64(1)))
			})
		})

		Context("when ResourceClaims don't exist", func() {
			It("should return error when claim doesn't exist", func() {
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:              "missing-claim",
								ResourceClaimName: ptr.To("non-existent-claim"),
							},
						},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})

			It("should return error when claim name cannot be determined", func() {
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:                      "gpu-claim",
								ResourceClaimTemplateName: ptr.To("gpu-template"),
							},
						},
					},
					Status: v1.PodStatus{
						ResourceClaimStatuses: []v1.PodResourceClaimStatus{},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})
		})

		Context("when ResourceClaims have non-GPU devices", func() {
			It("should ignore non-GPU device claims", func() {
				claim := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-gpu-claim",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: "other-device-class",
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           5,
									},
								},
							},
						},
					},
				}

				Expect(fakeClient.Create(ctx, claim)).To(Succeed())

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:              "non-gpu-claim",
								ResourceClaimName: ptr.To("non-gpu-claim"),
							},
						},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeEmpty())
			})

			It("should extract GPU resources and ignore non-GPU resources", func() {
				gpuClaim := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-claim",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           2,
									},
								},
							},
						},
					},
				}

				nonGpuClaim := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-gpu-claim",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: "other-device-class",
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           10,
									},
								},
							},
						},
					},
				}

				Expect(fakeClient.Create(ctx, gpuClaim)).To(Succeed())
				Expect(fakeClient.Create(ctx, nonGpuClaim)).To(Succeed())

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:              "gpu-claim",
								ResourceClaimName: ptr.To("gpu-claim"),
							},
							{
								Name:              "non-gpu-claim",
								ResourceClaimName: ptr.To("non-gpu-claim"),
							},
						},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).NotTo(HaveOccurred())
				gpuQuantity, exists := result[constants.NvidiaGpuResource]
				Expect(exists).To(BeTrue(), "result should contain GPU resource")
				Expect(gpuQuantity.Value()).To(Equal(int64(2)))
			})
		})

		Context("when ResourceClaims have mixed allocation modes", func() {
			It("should handle ExactCount and All modes together", func() {
				exactCountClaim := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "exact-count-claim",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
										Count:           2,
									},
								},
							},
						},
					},
				}

				allModeClaim := &resourceapi.ResourceClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "all-mode-claim",
						Namespace: "default",
					},
					Spec: resourceapi.ResourceClaimSpec{
						Devices: resourceapi.DeviceClaim{
							Requests: []resourceapi.DeviceRequest{
								{
									Exactly: &resourceapi.ExactDeviceRequest{
										DeviceClassName: constants.NvidiaGpuResource,
										AllocationMode:  resourceapi.DeviceAllocationModeAll,
									},
								},
							},
						},
					},
				}

				Expect(fakeClient.Create(ctx, exactCountClaim)).To(Succeed())
				Expect(fakeClient.Create(ctx, allModeClaim)).To(Succeed())

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:              "exact-count-claim",
								ResourceClaimName: ptr.To("exact-count-claim"),
							},
							{
								Name:              "all-mode-claim",
								ResourceClaimName: ptr.To("all-mode-claim"),
							},
						},
					},
				}

				result, err := ExtractDRAGPUResources(ctx, pod, fakeClient)
				Expect(err).NotTo(HaveOccurred())
				gpuQuantity, exists := result[constants.NvidiaGpuResource]
				Expect(exists).To(BeTrue(), "result should contain GPU resource")
				// ExactCount: 2, All: 1 (conservative estimate) = 3 total
				Expect(gpuQuantity.Value()).To(Equal(int64(3)))
			})
		})
	})
})
