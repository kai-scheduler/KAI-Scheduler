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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/pointer"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

const draDeviceClassName = constant.GPUDeviceClassName

func DescribeReclaimDRASpecs() bool {
	return Describe("Reclaim DRA", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, draDeviceClassName, 1, 4)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("Under quota and Over fair share -> Over fair share and Over quota - Should not reclaim (DRA)", func(ctx context.Context) {
			// 4 GPUs in total (quota of 1). DRA uses integer device counts: 2+1+1 reclaimee, 1 reclaimer, pending 2.
			// reclaimee: 2+1+1 => 4 (OFS) => 2 (OQ)
			// reclaimer: 1 => 1 + requesting 2 (UQ) => 3 (OFS) - should not reclaim
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, draDeviceClassName, 1, 4)

			parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 1, 1)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			reclaimeeNamespace := queue.GetConnectedNamespaceToQueue(reclaimeeQueue)
			reclaimerNamespace := queue.GetConnectedNamespaceToQueue(reclaimerQueue)

			pod := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 2, nil)
			pod2 := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 1, nil)
			pod3 := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 1, nil)

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod3)

			runningPod := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimerQueue, reclaimerNamespace, 1, nil)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, runningPod)

			pendingPod := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimerQueue, reclaimerNamespace, 2, nil)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pendingPod)
		})

		It("Simple priority reclaim - but has min runtime (DRA)", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, draDeviceClassName, 1, 4)

			parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 1, 0)
			reclaimerQueue.Spec.Priority = pointer.Int(constants.DefaultQueuePriority + 1)
			reclaimeeQueue.Spec.ReclaimMinRuntime = &metav1.Duration{Duration: 60 * time.Second}
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			reclaimeeNamespace := queue.GetConnectedNamespaceToQueue(reclaimeeQueue)
			reclaimerNamespace := queue.GetConnectedNamespaceToQueue(reclaimerQueue)

			pod := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 1, nil)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

			reclaimee := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 3, nil)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimee)

			reclaimer := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimerQueue, reclaimerNamespace, 1, nil)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, reclaimer)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimer)
		})

		It("Reclaim tasks from elastic jobs (DRA)", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, draDeviceClassName, 1, 4)

			parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 2, 2)
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})
			reclaimeeNamespace := queue.GetConnectedNamespaceToQueue(reclaimeeQueue)
			reclaimerNamespace := queue.GetConnectedNamespaceToQueue(reclaimerQueue)

			pg1 := pod_group.Create(reclaimeeNamespace, "elastic-reclaimee-job-1", reclaimeeQueue.Name)
			pg1, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(reclaimeeNamespace).Create(ctx, pg1, metav1.CreateOptions{})
			Expect(err).To(Succeed())
			pg2 := pod_group.Create(reclaimeeNamespace, "elastic-reclaimee-job-2", reclaimeeQueue.Name)
			pg2, err = testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(reclaimeeNamespace).Create(ctx, pg2, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			pods1 := []*v1.Pod{
				rd.CreatePodWithGpuClaimAndPodGroup(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 1, pg1.Name),
				rd.CreatePodWithGpuClaimAndPodGroup(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 1, pg1.Name),
			}
			pods2 := []*v1.Pod{
				rd.CreatePodWithGpuClaimAndPodGroup(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 1, pg2.Name),
				rd.CreatePodWithGpuClaimAndPodGroup(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimeeQueue, reclaimeeNamespace, 1, pg2.Name),
			}
			var preempteePods []*v1.Pod
			preempteePods = append(preempteePods, pods1...)
			preempteePods = append(preempteePods, pods2...)
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, reclaimeeNamespace, preempteePods)

			reclaimerPod := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimerQueue, reclaimerNamespace, 2, nil)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimerPod)

			wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
				pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimeeNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", rd.PodGroupLabelName, pg1.Name),
				})
				Expect(err).To(Succeed())
				return len(pods.Items) == 1
			}, runtimeClient.InNamespace(reclaimeeNamespace))

			wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
				pods, err := testCtx.KubeClientset.CoreV1().Pods(reclaimeeNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", rd.PodGroupLabelName, pg2.Name),
				})
				Expect(err).To(Succeed())
				return len(pods.Items) == 1
			}, runtimeClient.InNamespace(reclaimeeNamespace))
		})

		It("Reclaim based on priority, maintain over-quota weight proportion", func(ctx context.Context) {
			// 8 GPUs in total. DRA: reclaimee1/2 use claim templates (1 device per pod), reclaimer uses 3-device claim.
			// reclaimee1: OQW 1: 2 over-quota => 3 allocated; reclaimee2: OQW 2: 4 over-quota => 5 allocated
			// reclaimer: 0 deserved, priority +1, 3 GPUs requested

			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, draDeviceClassName, 1, 8)

			nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, draDeviceClassName)
			var testNodeName string
			for name, deviceCount := range nodesMap {
				if deviceCount >= 8 {
					testNodeName = name
					break
				}
			}
			Expect(testNodeName).ToNot(Equal(""), "need at least one node with 8 DRA devices")

			parentQueue, reclaimee1Queue, reclaimee2Queue := CreateQueues(8, 1, 1)
			reclaimee1Queue.Spec.Resources.GPU.OverQuotaWeight = 1
			reclaimee2Queue.Spec.Resources.GPU.OverQuotaWeight = 2

			reclaimerQueueName := utils.GenerateRandomK8sName(10)
			reclaimerQueue := queue.CreateQueueObjectWithGpuResource(reclaimerQueueName,
				v2.QueueResource{
					Quota:           0,
					OverQuotaWeight: 1,
					Limit:           -1,
				}, parentQueue.Name)
			reclaimerQueue.Spec.Priority = pointer.Int(constants.DefaultQueuePriority + 1)

			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimee1Queue, reclaimee2Queue, reclaimerQueue})
			reclaimee1Namespace := queue.GetConnectedNamespaceToQueue(reclaimee1Queue)
			reclaimee2Namespace := queue.GetConnectedNamespaceToQueue(reclaimee2Queue)
			reclaimerNamespace := queue.GetConnectedNamespaceToQueue(reclaimerQueue)

			template1 := rd.CreateResourceClaimTemplate(reclaimee1Namespace, reclaimee1Queue.Name, draDeviceClassName, 1)
			template1, err := testCtx.KubeClientset.ResourceV1().ResourceClaimTemplates(reclaimee1Namespace).Create(ctx, template1, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			for range 10 {
				job := rd.CreateBatchJobObject(reclaimee1Queue, v1.ResourceRequirements{})
				job.Spec.Template.Spec.ResourceClaims = []v1.PodResourceClaim{{
					Name:                      "gpu",
					ResourceClaimTemplateName: ptr.To(template1.Name),
				}}
				job.Spec.Template.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": testNodeName}
				err := testCtx.ControllerClient.Create(ctx, job)
				Expect(err).To(Succeed())
			}

			template2 := rd.CreateResourceClaimTemplate(reclaimee2Namespace, reclaimee2Queue.Name, draDeviceClassName, 1)
			template2, err = testCtx.KubeClientset.ResourceV1().ResourceClaimTemplates(reclaimee2Namespace).Create(ctx, template2, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			for range 10 {
				job := rd.CreateBatchJobObject(reclaimee2Queue, v1.ResourceRequirements{})
				job.Spec.Template.Spec.ResourceClaims = []v1.PodResourceClaim{{
					Name:                      "gpu",
					ResourceClaimTemplateName: ptr.To(template2.Name),
				}}
				job.Spec.Template.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": testNodeName}
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

			setNodeSelector := func(pod *v1.Pod) {
				pod.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": testNodeName}
			}
			reclaimerPod := rd.CreatePodWithGpuClaim(ctx, testCtx.KubeClientset, testCtx.ControllerClient, reclaimerQueue, reclaimerNamespace, 3, setNodeSelector)
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
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee1Namespace, PodListToPodsSlice(reclaimee1Pods), 2)
			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, reclaimee1Namespace, PodListToPodsSlice(reclaimee1Pods), 8)

			reclaimee2Pods, err = testCtx.KubeClientset.CoreV1().Pods(reclaimee2Namespace).List(ctx, metav1.ListOptions{})
			Expect(err).To(Succeed())
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, reclaimee2Namespace, PodListToPodsSlice(reclaimee2Pods), 3)
			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, reclaimee2Namespace, PodListToPodsSlice(reclaimee2Pods), 7)
		})
	})
}
