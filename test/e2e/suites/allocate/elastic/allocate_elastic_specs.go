/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package elastic

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeAllocateElasticSpecs() bool {
	return Describe("Elastic allocation basic scenarios", Ordered, func() {
		var (
			testCtx      *testcontext.TestContext
			lowPriority  string
			highPriority string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			childQueue.Spec.Resources.CPU.Quota = 500
			childQueue.Spec.Resources.CPU.Limit = 500
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Cpu:      resource.MustParse("500m"),
					PodCount: 2,
				},
			})

			var err error
			lowPriority, highPriority, err = rd.CreatePreemptibleAndNonPriorityClass(ctx, testCtx.KubeClientset)
			Expect(err).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
			Expect(err).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("Elastic partial allocation", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			_, _, pods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, testCtx.Queues[0],
				rd.DistributedBatchJobOptions{
					Parallelism:       ptr.To(int32(2)),
					MinMember:         ptr.To(int32(1)),
					PriorityClassName: lowPriority,
					Resources: v1.ResourceRequirements{
						Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				})
			Expect(err).To(Succeed())

			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, 1)
			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, pods, 1)
		})

		It("Elastic full allocation", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			_, _, pods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, testCtx.Queues[0],
				rd.DistributedBatchJobOptions{
					Parallelism:       ptr.To(int32(2)),
					MinMember:         ptr.To(int32(1)),
					PriorityClassName: lowPriority,
					Resources: v1.ResourceRequirements{
						Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceCPU: resource.MustParse("200m"),
						},
					},
				})
			Expect(err).To(Succeed())

			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, 2)
		})

		It("Balance 2 elastic jobs", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			pgJob1Name := utils.GenerateRandomK8sName(10)
			numPodsJob1 := 3
			pgJob2Name := utils.GenerateRandomK8sName(10)
			numPodsJob2 := 3

			var allPods []*v1.Pod
			for i := 0; i < numPodsJob1; i++ {
				pod := createElasticPod(ctx, testCtx.KubeClientset, testCtx.Queues[0], pgJob1Name, "100m")
				allPods = append(allPods, pod)
			}
			for i := 0; i < numPodsJob2; i++ {
				pod := createElasticPod(ctx, testCtx.KubeClientset, testCtx.Queues[0], pgJob2Name, "100m")
				allPods = append(allPods, pod)
			}
			_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
				pod_group.Create(namespace, pgJob1Name, testCtx.Queues[0].Name),
				metav1.CreateOptions{})
			Expect(err).To(Succeed())
			_, err = testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
				pod_group.Create(namespace, pgJob2Name, testCtx.Queues[0].Name),
				metav1.CreateOptions{})
			Expect(err).To(Succeed())

			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, allPods, 5)
			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, namespace, allPods, 1)
		})

		It("All pods of an elastic job will be prioritized to job with lower priority", func(ctx context.Context) {
			podRequirements := v1.ResourceRequirements{
				Requests: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("250m"),
				},
			}
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			lowPriorityPod := rd.CreatePodObject(testCtx.Queues[0], podRequirements)
			_, _, pods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, testCtx.Queues[0],
				rd.DistributedBatchJobOptions{
					Parallelism:       ptr.To(int32(2)),
					MinMember:         ptr.To(int32(1)),
					PriorityClassName: highPriority,
					Resources:         podRequirements,
				})
			Expect(err).To(Succeed())

			lowPriorityPod.Spec.PriorityClassName = lowPriority
			lowPriorityPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, lowPriorityPod)
			Expect(err).To(Succeed())
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, 2)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, lowPriorityPod)
		})
	})
}

func createElasticPod(ctx context.Context, client *kubernetes.Clientset, queue *v2.Queue, podGroupName string,
	cpuPerPod string) *v1.Pod {
	pod := rd.CreatePodWithPodGroupReference(queue, podGroupName, v1.ResourceRequirements{
		Limits: map[v1.ResourceName]resource.Quantity{
			v1.ResourceCPU: resource.MustParse(cpuPerPod),
		},
	})
	pod, err := rd.CreatePod(ctx, client, pod)
	Expect(err).To(Succeed())
	return pod
}
