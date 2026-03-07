/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package consolidation

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"
	"k8s.io/utils/strings/slices"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/fillers"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

func DescribeConsolidationSpecs() bool {
	return Describe("Consolidation", Ordered, func() {
		var (
			testCtx       *testcontext.TestContext
			priorityClass string
			namespace     string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			testQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{testQueue, parentQueue})

			var err error
			priorityClass, err = rd.CreatePreemptiblePriorityClass(ctx, testCtx.KubeClientset)
			Expect(err).NotTo(HaveOccurred())
		})

		BeforeEach(func(ctx context.Context) {
			testQueue := testCtx.Queues[0]
			namespace = queue.GetConnectedNamespaceToQueue(testQueue)
		})

		AfterAll(func(ctx context.Context) {
			err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
			Expect(err).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			backgroundPropagation := metav1.DeletePropagationBackground
			err := testCtx.KubeClientset.BatchV1().Jobs(namespace).DeleteCollection(
				ctx,
				metav1.DeleteOptions{PropagationPolicy: &backgroundPropagation, GracePeriodSeconds: ptr.To(int64(0))},
				metav1.ListOptions{},
			)
			Expect(err).To(Succeed())

			testCtx.TestContextCleanup(ctx)
		})

		It("Node level consolidation", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Gpu:      resource.MustParse("2"),
					PodCount: 2,
				},
				{
					Gpu:      resource.MustParse("2"),
					PodCount: 2,
				},
			})

			testQueue := testCtx.Queues[0]
			requirements := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			}

			fillerJobs, _, err := fillers.FillAllNodesWithJobs(
				ctx, testCtx, testQueue, requirements, nil, nil, priorityClass,
			)
			Expect(err).To(Succeed())
			Expect(len(fillerJobs)).Should(BeNumerically(">", 0))

			numReleasedGPUs := int64(0)
			lastNodeName := ""
			for _, job := range fillerJobs {
				pods := rd.GetJobPods(ctx, testCtx.KubeClientset, job)
				Expect(len(pods)).Should(BeNumerically(">", 0))
				pod := pods[0]
				if pod.Spec.NodeName == lastNodeName {
					continue
				}
				lastNodeName = pod.Spec.NodeName
				rd.DeleteJob(ctx, testCtx.KubeClientset, job)
				numReleasedGPUs += 1

				if numReleasedGPUs >= 2 {
					break
				}
			}
			Expect(numReleasedGPUs).Should(BeNumerically(">", 1))

			gpuQuantity := resource.NewQuantity(numReleasedGPUs, resource.DecimalSI)
			testedPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: *gpuQuantity,
				},
			})
			testedPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, testedPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, testedPod)

			jobs, err := testCtx.KubeClientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).To(Succeed())
			for _, job := range jobs.Items {
				pods := rd.GetJobPods(ctx, testCtx.KubeClientset, &job)
				for _, pod := range pods {
					wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pod)
				}
			}
		})

		It("GPU device level consolidation", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Gpu:      resource.MustParse("2"),
					PodCount: 4,
				},
			})

			node := rd.FindNodeWithEnoughGPUs(ctx, testCtx.ControllerClient, 2)
			Expect(node).NotTo(BeNil())

			testQueue := testCtx.Queues[0]
			annotations := map[string]string{
				constants.GpuFraction: "0.5",
			}
			nodeSelector := map[string]string{constant.NodeNamePodLabelName: node.Name}
			fillerJobs, _, err := fillers.FillAllNodesWithJobs(
				ctx, testCtx, testQueue, v1.ResourceRequirements{},
				annotations, nil, priorityClass, node.Name,
			)
			Expect(err).To(Succeed())
			Expect(len(fillerJobs)).Should(BeNumerically(">", 0))

			numDeletedJobs := int64(0)
			var undeletedJobs []*batchv1.Job
			var gpuGroups []string
			for _, job := range fillerJobs {
				pods := rd.GetJobPods(ctx, testCtx.KubeClientset, job)
				Expect(len(pods)).Should(BeNumerically(">", 0))
				pod := &pods[0]

				wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
					pod, err := testCtx.KubeClientset.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
					Expect(err).To(Succeed())
					_, found := pod.Labels[constants.GPUGroup]
					return found
				})

				pod, err := testCtx.KubeClientset.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
				Expect(err).To(Succeed())
				gpuGroup, found := pod.Labels[constants.GPUGroup]
				Expect(found).To(BeTrue())
				if slices.Contains(gpuGroups, gpuGroup) {
					undeletedJobs = append(undeletedJobs, job)
					continue
				}
				gpuGroups = append(gpuGroups, gpuGroup)
				rd.DeleteJob(ctx, testCtx.KubeClientset, job)
				numDeletedJobs += 1
			}
			Expect(numDeletedJobs).Should(BeNumerically(">", 1))

			testedPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})
			testedPod.Name = "consolidator-pod"
			testedPod.Spec.NodeSelector = nodeSelector
			testedPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, testedPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, testedPod)

			Expect(err).To(Succeed())
			for _, job := range undeletedJobs {
				pods := rd.GetJobPods(ctx, testCtx.KubeClientset, job)
				for _, pod := range pods {
					wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pod)
				}
			}
		})
	})
}
