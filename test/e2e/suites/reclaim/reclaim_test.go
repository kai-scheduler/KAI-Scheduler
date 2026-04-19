/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/pointer"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func createPod(ctx context.Context, testCtx *testcontext.TestContext, queue *v2.Queue, gpus float64) *v1.Pod {
	pod := rd.CreatePodObject(queue, v1.ResourceRequirements{})
	if gpus >= 1 {
		pod.Spec.Containers[0].Resources.Limits = map[v1.ResourceName]resource.Quantity{
			constants.GpuResource: resource.MustParse(fmt.Sprintf("%f", gpus)),
		}
	} else if gpus > 0 {
		pod.Annotations = map[string]string{
			constants.GpuFraction: fmt.Sprintf("%f", gpus),
		}
	}

	pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
	Expect(err).To(Succeed())
	return pod
}

func createQueues(parentQueueDeserved, queue1Deserved, queue2Deserved float64) (
	*v2.Queue, *v2.Queue, *v2.Queue) {
	parentQueueName := utils.GenerateRandomK8sName(10)
	parentQueue := queue.CreateQueueObjectWithGpuResource(parentQueueName,
		v2.QueueResource{
			Quota:           parentQueueDeserved,
			OverQuotaWeight: 2,
			Limit:           parentQueueDeserved,
		}, "")

	queue1Name := utils.GenerateRandomK8sName(10)
	queue1 := queue.CreateQueueObjectWithGpuResource(queue1Name,
		v2.QueueResource{
			Quota:           queue1Deserved,
			OverQuotaWeight: 2,
			Limit:           -1,
		}, parentQueueName)

	queue2Name := utils.GenerateRandomK8sName(10)
	queue2 := queue.CreateQueueObjectWithGpuResource(queue2Name,
		v2.QueueResource{
			Quota:           queue2Deserved,
			OverQuotaWeight: 2,
			Limit:           -1,
		}, parentQueueName)

	return parentQueue, queue1, queue2
}

var _ = Describe("Reclaim", Ordered, func() {
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
			// 4 GPUs in total (quota of 1)
			// reclaimee: 2+2 => 4 (OFS)           => 2 (OQ)
			// reclaimer: 0   => requesting 2 (UQ) => 2 (OQ)

			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := createQueues(4, 1, 1)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			pod := createPod(ctx, testCtx, reclaimeeQueue, 2)
			pod2 := createPod(ctx, testCtx, reclaimeeQueue, 2)

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

			pendingPod := createPod(ctx, testCtx, reclaimerQueue, 2)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
		})

		It("Under quota and Over fair share -> Under quota and Under quota - Should reclaim", func(ctx context.Context) {
			// 4 GPUs in total (quota of 2)
			// reclaimee: 1+2 => 3 (OFS)                 => 1 (UQ)
			// reclaimer: 1   => 1 + requesting 0.5 (UQ) => 1.5 (UQ)

			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := createQueues(4, 2, 2)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			pod := createPod(ctx, testCtx, reclaimeeQueue, 1)
			pod2 := createPod(ctx, testCtx, reclaimeeQueue, 2)

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

			runningPod := createPod(ctx, testCtx, reclaimerQueue, 1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)

			pendingPod := createPod(ctx, testCtx, reclaimerQueue, 0.5)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
		})

		It("Under quota and Over fair share -> Over fair share and Over quota - Should not reclaim", func(ctx context.Context) {
			// 4 GPUs in total (quota of 1)
			// reclaimee: 2+1+0.5 => 3.5 (OFS)               => 1.5 (OQ)
			// reclaimer: 0.5     => 0.5 + requesting 2 (UQ) => 2.5 (OFS)

			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := createQueues(4, 1, 1)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			pod := createPod(ctx, testCtx, reclaimeeQueue, 2)
			pod2 := createPod(ctx, testCtx, reclaimeeQueue, 1)
			pod3 := createPod(ctx, testCtx, reclaimeeQueue, 0.5)

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod3)

			runningPod := createPod(ctx, testCtx, reclaimerQueue, 0.5)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)

			pendingPod := createPod(ctx, testCtx, reclaimerQueue, 2)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pendingPod)
		})

		It("Over quota and Over fair share -> Over quota and Over quota - Should reclaim", func(ctx context.Context) {
			// 4 GPUs in total (quota of 0)
			// reclaimee: 1+2 => 3 (OFS)                  =>  1 (OQ)
			// reclaimer: 1   => 1 + requesting 0.5 (OQ)  =>  1.5 (OQ)

			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := createQueues(4, 0, 0)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			runningPod := createPod(ctx, testCtx, reclaimeeQueue, 1)
			runningPod2 := createPod(ctx, testCtx, reclaimeeQueue, 2)

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod2)

			runningPod3 := createPod(ctx, testCtx, reclaimerQueue, 1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod3)

			pendingPod := createPod(ctx, testCtx, reclaimerQueue, 0.5)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
		})

		It("Under Quota and Over Quota -> Under Quota and Over Quota - Should reclaim", func(ctx context.Context) {
			// 4 GPUs in total (quota of 2)
			// reclaimee: 2+0.9+0.3+0.3 => 3.5 (OQ)          => 2.9/2.6 (OQ)
			// reclaimer: 0             => requesting 1 (UQ) => 1 (UQ)

			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := createQueues(4, 2, 2)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			runningPod := createPod(ctx, testCtx, reclaimeeQueue, 2)
			runningPod2 := createPod(ctx, testCtx, reclaimeeQueue, 0.9)
			runningPod3 := createPod(ctx, testCtx, reclaimeeQueue, 0.3)
			runningPod4 := createPod(ctx, testCtx, reclaimeeQueue, 0.3)

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod2)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod3)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod4)

			pendingPod := createPod(ctx, testCtx, reclaimerQueue, 1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
		})

		It("Under Quota and Over Quota -> Over Quota and Over Quota - Should reclaim", func(ctx context.Context) {
			// 4 GPUs in total (quota of 0)
			// reclaimee: 2 + 0.9 + 0.3 + 0.3 => 3.5 (OQ) => 2.9/2.6 (OQ)
			// reclaimer: 0                   => requesting 1 (UQ)  => 1 (OQ)

			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := createQueues(4, 0, 0)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			runningPod := createPod(ctx, testCtx, reclaimeeQueue, 2)
			runningPod2 := createPod(ctx, testCtx, reclaimeeQueue, 0.9)
			runningPod3 := createPod(ctx, testCtx, reclaimeeQueue, 0.3)
			runningPod4 := createPod(ctx, testCtx, reclaimeeQueue, 0.3)

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod2)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod3)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod4)

			pendingPod := createPod(ctx, testCtx, reclaimerQueue, 1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
		})

		It("Simple priority reclaim", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := createQueues(4, 1, 0)
			reclaimerQueue.Spec.Priority = pointer.Int(constants.DefaultQueuePriority + 1)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			pod := createPod(ctx, testCtx, reclaimeeQueue, 1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

			reclaimee := createPod(ctx, testCtx, reclaimeeQueue, 3)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimee)

			reclaimer := createPod(ctx, testCtx, reclaimerQueue, 1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimer)
		})

		It("Simple priority reclaim - but has min runtime", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := createQueues(4, 1, 0)
			reclaimerQueue.Spec.Priority = pointer.Int(constants.DefaultQueuePriority + 1)
			reclaimeeQueue.Spec.ReclaimMinRuntime = &metav1.Duration{Duration: 60 * time.Second}
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			pod := createPod(ctx, testCtx, reclaimeeQueue, 1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

			reclaimee := createPod(ctx, testCtx, reclaimeeQueue, 3)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimee)

			reclaimer := createPod(ctx, testCtx, reclaimerQueue, 1)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, reclaimer)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimer)
		})

		It("Reclaim based on priority, maintain over-quota weight proportion", func(ctx context.Context) {
			// 8 GPUs in total
			// reclaimee1, reclaimee 2: 1 deserved each, 6 GPUs leftover
			// reclaimee1: OQW 1: 2 over-quota
			// reclaimee2: OQW 2: 4 over-quota
			// Total: reclaimee 1: 3 allocated, reclaimee 2: 5 allocated
			// reclaimer: 0 deserved, priority +1, 3 GPUs requested

			testCtx = testcontext.GetConnectivity(ctx, Default)

			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Gpu:      resource.MustParse("8"),
					PodCount: 8,
				},
			})

			nodes := rd.FindNodesWithExactGPUs(ctx, testCtx.ControllerClient, 8)
			Expect(len(nodes)).To(BeNumerically(">", 0))
			testNodeName := nodes[0].Name

			parentQueue, reclaimee1Queue, reclaimee2Queue := createQueues(8, 1, 1)
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

			// Submit more jobs then can schedule
			for range 10 {
				job := rd.CreateBatchJobObject(reclaimee1Queue, v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{
						constants.GpuResource: resource.MustParse("1"),
					},
				})
				job.Spec.Template.Spec.NodeSelector = map[string]string{
					"kubernetes.io/hostname": testNodeName,
				}
				err := testCtx.ControllerClient.Create(ctx, job)
				Expect(err).To(Succeed())
			}

			for range 10 {
				job := rd.CreateBatchJobObject(reclaimee2Queue, v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{
						constants.GpuResource: resource.MustParse("1"),
					},
				})
				job.Spec.Template.Spec.NodeSelector = map[string]string{
					"kubernetes.io/hostname": testNodeName,
				}
				err := testCtx.ControllerClient.Create(ctx, job)
				Expect(err).To(Succeed())
			}

			selector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "engine-e2e",
				},
			}
			wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, selector, 20)

			// Verify expected ratios between queues: 1 + 2 for reclaimee1, 1 + 4 for reclaimee2 - based on weights
			reclaimee1Pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimee1Namespace).List(ctx, metav1.ListOptions{})
			Expect(err).To(Succeed())
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee1Namespace, podListToPodsSlice(reclaimee1Pods), 3)

			reclaimee2Pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimee2Namespace).List(ctx, metav1.ListOptions{})
			Expect(err).To(Succeed())
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee2Namespace, podListToPodsSlice(reclaimee2Pods), 5)

			// Submit reclaimer pod that requests 3 GPUs
			reclaimerPod := rd.CreatePodObject(reclaimerQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.GpuResource: resource.MustParse("3"),
				},
			})
			reclaimerPod.Spec.NodeSelector = map[string]string{
				"kubernetes.io/hostname": testNodeName,
			}
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

			// Verify that reclaimees maintain over-quota weights: 1 + 1 for reclaimee1, 1 + 2 for reclaimee2
			reclaimee1Pods, err = testCtx.KubeClientset.CoreV1().Pods(reclaimee1Namespace).List(ctx, metav1.ListOptions{})
			Expect(err).To(Succeed())
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee1Namespace, podListToPodsSlice(reclaimee1Pods), 2)
			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, reclaimee1Namespace, podListToPodsSlice(reclaimee1Pods), 8)

			reclaimee2Pods, err = testCtx.KubeClientset.CoreV1().Pods(reclaimee2Namespace).List(ctx, metav1.ListOptions{})
			Expect(err).To(Succeed())
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee2Namespace, podListToPodsSlice(reclaimee2Pods), 3)
			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, reclaimee2Namespace, podListToPodsSlice(reclaimee2Pods), 7)
		})
	})
})

func podListToPodsSlice(podList *v1.PodList) []*v1.Pod {
	pods := make([]*v1.Pod, 0)
	for _, pod := range podList.Items {
		pods = append(pods, &pod)
	}
	return pods
}
