/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resources

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

var _ = Describe("Schedule pod with resource request", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	It("No resource requests", func(ctx context.Context) {
		pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})

		_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).NotTo(HaveOccurred())

		wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
	})

	Context("GPU Resources", func() {
		BeforeAll(func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Gpu:      resource.MustParse("1"),
					PodCount: 1,
				},
			)
		})

		It("Whole GPU request", func(ctx context.Context) {
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.GpuResource: resource.MustParse("1"),
				},
			})

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		It("Fraction GPU request", Label(labels.ReservationPod), func(ctx context.Context) {
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod.Annotations = map[string]string{
				constants.GpuFraction: "0.5",
			}

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodReady(ctx, testCtx.ControllerClient, pod)
		})

		It("GPU memory request - valid", Label(labels.ReservationPod), func(ctx context.Context) {
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod.Annotations = map[string]string{
				constants.GpuMemory: "500",
			}

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		It("GPU memory request - too much memory for a single gpu", Label(labels.ReservationPod),
			func(ctx context.Context) {
				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
				pod.Annotations = map[string]string{
					constants.GpuMemory: "500000000",
				}

				_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())

				wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
			})
	})

	Context("CPU Resources", func() {
		It("CPU Request", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Cpu:      resource.MustParse("1"),
					PodCount: 1,
				},
			)

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Requests: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("1"),
				},
			})

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		It("Memory Request", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Memory:   resource.MustParse("100M"),
					PodCount: 1,
				},
			)

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Requests: map[v1.ResourceName]resource.Quantity{
					v1.ResourceMemory: resource.MustParse("100M"),
				},
			})

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		It("Other Resource Request", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					PodCount: 1,
					OtherResources: map[v1.ResourceName]resource.Quantity{
						v1.ResourceEphemeralStorage: resource.MustParse("100M"),
					},
				},
			)

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Requests: map[v1.ResourceName]resource.Quantity{
					v1.ResourceEphemeralStorage: resource.MustParse("100M"),
				},
			})

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})
	})
})
