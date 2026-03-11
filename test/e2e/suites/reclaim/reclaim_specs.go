/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/pointer"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeReclaimSpecs() bool {
	return Describe("Reclaim", Ordered, func() {
		Context("Quota/Fair-share based reclaim", func() {
			var (
				testCtx *testcontext.TestContext
			)

			BeforeAll(func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
					{
						Gpu:      resource.MustParse("4"),
						PodCount: 4,
					},
				})
			})

			AfterEach(func(ctx context.Context) {
				testCtx.ClusterCleanup(ctx)
			})

			It("Under quota and Over fair share -> Over quota and Over quota - Should reclaim", func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 1, 1)
				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

				pod := CreatePod(ctx, testCtx, reclaimeeQueue, 2)
				pod2 := CreatePod(ctx, testCtx, reclaimeeQueue, 2)

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

				pendingPod := CreatePod(ctx, testCtx, reclaimerQueue, 2)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
			})

			It("Under quota and Over fair share -> Under quota and Under quota - Should reclaim", func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 2, 2)
				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

				pod := CreatePod(ctx, testCtx, reclaimeeQueue, 1)
				pod2 := CreatePod(ctx, testCtx, reclaimeeQueue, 2)

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

				runningPod := CreatePod(ctx, testCtx, reclaimerQueue, 1)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)

				pendingPod := CreatePod(ctx, testCtx, reclaimerQueue, 0.5)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
			})

			It("Under quota and Over fair share -> Over fair share and Over quota - Should not reclaim", func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 1, 1)
				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

				pod := CreatePod(ctx, testCtx, reclaimeeQueue, 2)
				pod2 := CreatePod(ctx, testCtx, reclaimeeQueue, 1)
				pod3 := CreatePod(ctx, testCtx, reclaimeeQueue, 0.5)

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod3)

				runningPod := CreatePod(ctx, testCtx, reclaimerQueue, 0.5)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)

				pendingPod := CreatePod(ctx, testCtx, reclaimerQueue, 2)
				wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pendingPod)
			})

			It("Over quota and Over fair share -> Over quota and Over quota - Should reclaim", func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 0, 0)
				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

				runningPod := CreatePod(ctx, testCtx, reclaimeeQueue, 1)
				runningPod2 := CreatePod(ctx, testCtx, reclaimeeQueue, 2)

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod2)

				runningPod3 := CreatePod(ctx, testCtx, reclaimerQueue, 1)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod3)

				pendingPod := CreatePod(ctx, testCtx, reclaimerQueue, 0.5)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
			})

			It("Under Quota and Over Quota -> Under Quota and Over Quota - Should reclaim", func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 2, 2)
				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

				runningPod := CreatePod(ctx, testCtx, reclaimeeQueue, 2)
				runningPod2 := CreatePod(ctx, testCtx, reclaimeeQueue, 0.9)
				runningPod3 := CreatePod(ctx, testCtx, reclaimeeQueue, 0.3)
				runningPod4 := CreatePod(ctx, testCtx, reclaimeeQueue, 0.3)

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod2)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod3)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod4)

				pendingPod := CreatePod(ctx, testCtx, reclaimerQueue, 1)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
			})

			It("Under Quota and Over Quota -> Over Quota and Over Quota - Should reclaim", func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 0, 0)
				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

				runningPod := CreatePod(ctx, testCtx, reclaimeeQueue, 2)
				runningPod2 := CreatePod(ctx, testCtx, reclaimeeQueue, 0.9)
				runningPod3 := CreatePod(ctx, testCtx, reclaimeeQueue, 0.3)
				runningPod4 := CreatePod(ctx, testCtx, reclaimeeQueue, 0.3)

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod2)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod3)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod4)

				pendingPod := CreatePod(ctx, testCtx, reclaimerQueue, 1)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
			})

			It("Simple priority reclaim", func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 1, 0)
				reclaimerQueue.Spec.Priority = pointer.Int(constants.DefaultQueuePriority + 1)
				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

				pod := CreatePod(ctx, testCtx, reclaimeeQueue, 1)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

				reclaimee := CreatePod(ctx, testCtx, reclaimeeQueue, 3)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimee)

				reclaimer := CreatePod(ctx, testCtx, reclaimerQueue, 1)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimer)
			})

			It("Reclaim based on priority, maintain over-quota weight proportion", func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)

				capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
					{
						Gpu:      resource.MustParse("8"),
						PodCount: 8,
					},
				})

				parentQueue, reclaimee1Queue, reclaimee2Queue := CreateQueues(8, 1, 1)
				reclaimee1Queue.Spec.Resources.GPU.OverQuotaWeight = 1
				reclaimee2Queue.Spec.Resources.GPU.OverQuotaWeight = 2

				reclaimeeQueueName := utils.GenerateRandomK8sName(10)
				reclaimerQueue := queue.CreateQueueObjectWithGpuResource(reclaimeeQueueName,
					v2.QueueResource{
						Quota:           0,
						OverQuotaWeight: 1,
						Limit:           -1,
					}, parentQueue.Name)
				reclaimerQueue.Spec.Priority = pointer.Int(constants.DefaultQueuePriority + 1)

				testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimee1Queue, reclaimee2Queue, reclaimerQueue})
				reclaimee1Namespace := queue.GetConnectedNamespaceToQueue(reclaimee1Queue)
				reclaimee2Namespace := queue.GetConnectedNamespaceToQueue(reclaimee2Queue)

				for range 10 {
					job := rd.CreateBatchJobObject(reclaimee1Queue, v1.ResourceRequirements{
						Limits: map[v1.ResourceName]resource.Quantity{
							constants.GpuResource: resource.MustParse("1"),
						},
					})
					err := testCtx.ControllerClient.Create(ctx, job)
					Expect(err).To(Succeed())
				}

				for range 10 {
					job := rd.CreateBatchJobObject(reclaimee2Queue, v1.ResourceRequirements{
						Limits: map[v1.ResourceName]resource.Quantity{
							constants.GpuResource: resource.MustParse("1"),
						},
					})
					err := testCtx.ControllerClient.Create(ctx, job)
					Expect(err).To(Succeed())
				}

				selector := metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "engine-e2e",
					},
				}
				wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, selector, 20)

				reclaimee1Pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimee1Namespace).List(ctx, metav1.ListOptions{})
				Expect(err).To(Succeed())
				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee1Namespace, PodListToPodsSlice(reclaimee1Pods), 3)

				reclaimee2Pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimee2Namespace).List(ctx, metav1.ListOptions{})
				Expect(err).To(Succeed())
				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee2Namespace, PodListToPodsSlice(reclaimee2Pods), 5)

				reclaimerPod := rd.CreatePodObject(reclaimerQueue, v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{
						constants.GpuResource: resource.MustParse("3"),
					},
				})
				reclaimerPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, reclaimerPod)
				Expect(err).To(Succeed())
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimerPod)
				time.Sleep(3 * time.Second)

				wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(event watch.Event) bool {
					pods, ok := event.Object.(*v1.PodList)
					if !ok {
						return false
					}
					return len(pods.Items) == 10
				},
					runtimeClient.InNamespace(reclaimee1Namespace),
				)

				wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(event watch.Event) bool {
					pods, ok := event.Object.(*v1.PodList)
					if !ok {
						return false
					}
					return len(pods.Items) == 10
				},
					runtimeClient.InNamespace(reclaimee2Namespace),
				)

				reclaimee1Pods, err = testCtx.KubeClientset.CoreV1().Pods(reclaimee1Namespace).List(ctx, metav1.ListOptions{})
				Expect(err).To(Succeed())
				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee1Namespace, PodListToPodsSlice(reclaimee1Pods), 2)
				wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, reclaimee1Namespace, PodListToPodsSlice(reclaimee1Pods), 8)

				reclaimee2Pods, err = testCtx.KubeClientset.CoreV1().Pods(reclaimee2Namespace).List(ctx, metav1.ListOptions{})
				Expect(err).To(Succeed())
				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee2Namespace, PodListToPodsSlice(reclaimee2Pods), 3)
				wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, reclaimee2Namespace, PodListToPodsSlice(reclaimee2Pods), 7)
			})
		})
	})
}
