/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/fillers"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

func DescribePreemptDistributedSpecs() bool {
	return Describe("preempt Distributed Jobs", Ordered, func() {
		Context("Over more than one nodes - gpu extended resources", func() {
			var (
				testCtx      *testcontext.TestContext
				testQueue    *v2.Queue
				lowPriority  string
				highPriority string
			)

			BeforeAll(func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
					{
						Gpu:      resource.MustParse("4"),
						PodCount: 4,
					},
					{
						Gpu:      resource.MustParse("4"),
						PodCount: 4,
					},
				})

				parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
				testQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
				testCtx.InitQueues([]*v2.Queue{parentQueue, testQueue})

				var err error
				lowPriority, highPriority, err = rd.CreatePreemptibleAndNonPriorityClass(ctx, testCtx.KubeClientset)
				Expect(err).To(Succeed())
			})

			AfterEach(func(ctx context.Context) {
				testCtx.TestContextCleanup(ctx)
			})

			AfterAll(func(ctx context.Context) {
				err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
				Expect(err).To(Succeed())
				testCtx.ClusterCleanup(ctx)
			})

			It("should preempt jobs", func(ctx context.Context) {
				// This test assumes that the cluster has homogeneous GPU, so we can force the preemptor tasks to allocate on different nodes.
				gpusPerNode := capacity.SkipIfNonHomogeneousGpuCounts(testCtx.KubeClientset)

				resources := v1.ResourceRequirements{
					Limits: v1.ResourceList{
						constants.NvidiaGpuResource: resource.MustParse("1"),
					},
					Requests: v1.ResourceList{
						constants.NvidiaGpuResource: resource.MustParse("1"),
					},
				}

				_, fillerPods, err := fillers.FillAllNodesWithJobs(
					ctx, testCtx, testQueue, resources, nil, nil, lowPriority,
				)
				Expect(err).To(Succeed())
				nodesNamesMap := map[string]bool{}
				for _, pod := range fillerPods {
					Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), pod)).To(Succeed())
					if pod.Spec.NodeName != "" {
						nodesNamesMap[pod.Spec.NodeName] = true
					}
				}

				preemptorGpu := gpusPerNode
				preemptorResources := resources.DeepCopy()
				preemptorResources.Requests[constants.NvidiaGpuResource] = resource.MustParse(fmt.Sprintf("%d", preemptorGpu))
				preemptorResources.Limits[constants.NvidiaGpuResource] = resource.MustParse(fmt.Sprintf("%d", preemptorGpu))

				_, pods := pod_group.CreateDistributedJob(
					ctx, testCtx.KubeClientset, testCtx.ControllerClient,
					testQueue, 2, *preemptorResources, highPriority,
				)

				namespace := queue.GetConnectedNamespaceToQueue(testQueue)
				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, 2)
			})
		})

		Context("Over more than one nodes - DRA claims", func() {
			const (
				deviceClassName = constant.GPUDeviceClassName
				devicesPerNode  = 4
				preemptorCount  = 2
			)
			var (
				testCtx      *testcontext.TestContext
				testQueue    *v2.Queue
				lowPriority  string
				highPriority string
				namespace    string
			)

			BeforeAll(func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, deviceClassName, 2, devicesPerNode)

				parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
				testQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
				testCtx.InitQueues([]*v2.Queue{parentQueue, testQueue})
				namespace = queue.GetConnectedNamespaceToQueue(testQueue)

				var err error
				lowPriority, highPriority, err = rd.CreatePreemptibleAndNonPriorityClass(ctx, testCtx.KubeClientset)
				Expect(err).To(Succeed())
			})

			AfterEach(func(ctx context.Context) {
				testCtx.TestContextCleanup(ctx)
				capacity.CleanupResourceClaims(ctx, testCtx.KubeClientset, namespace)
			})

			AfterAll(func(ctx context.Context) {
				err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
				Expect(err).To(Succeed())
				testCtx.ClusterCleanup(ctx)
			})

			It("should preempt jobs", func(ctx context.Context) {
				perNodeDeviceCount := 0
				nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, deviceClassName)
				var targetNodes []string
				for nodeName, deviceCount := range nodesMap {
					if deviceCount >= devicesPerNode {
						targetNodes = append(targetNodes, nodeName)
						if perNodeDeviceCount == 0 {
							perNodeDeviceCount = deviceCount
						}
						if perNodeDeviceCount != deviceCount {
							Skip(fmt.Sprintf("node %s has %d DRA devices, expected %d. Non-homogeneous DRA devices are not supported for the test.", nodeName, deviceCount, perNodeDeviceCount))
						}
						if len(targetNodes) == preemptorCount {
							break
						}
					}
				}
				Expect(targetNodes).To(HaveLen(preemptorCount), "need at least 2 nodes with %d DRA devices each", devicesPerNode)

				fillerTemplate := rd.CreateResourceClaimTemplate(namespace, testQueue.Name, deviceClassName, 1)
				fillerTemplate, err := testCtx.KubeClientset.ResourceV1().ResourceClaimTemplates(namespace).Create(ctx, fillerTemplate, metav1.CreateOptions{})
				Expect(err).To(Succeed())

				var fillerPods []*v1.Pod
				for _, nodeName := range targetNodes {
					for i := 0; i < devicesPerNode; i++ {
						pod := rd.CreatePodObject(testQueue, v1.ResourceRequirements{
							Claims: []v1.ResourceClaim{
								{Name: "gpu", Request: "gpu"},
							},
						})
						pod.Spec.ResourceClaims = []v1.PodResourceClaim{{
							Name:                      "gpu",
							ResourceClaimTemplateName: ptr.To(fillerTemplate.Name),
						}}
						pod.Spec.Affinity = &v1.Affinity{
							NodeAffinity: &v1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
									NodeSelectorTerms: []v1.NodeSelectorTerm{{
										MatchExpressions: []v1.NodeSelectorRequirement{{
											Key:      v1.LabelHostname,
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{nodeName},
										}},
									}},
								},
							},
						}
						pod.Spec.PriorityClassName = lowPriority
						pod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
						Expect(err).To(Succeed())
						fillerPods = append(fillerPods, pod)
					}
				}

				wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, fillerPods)

				preemptorTemplate := rd.CreateResourceClaimTemplate(namespace, testQueue.Name, deviceClassName, perNodeDeviceCount)
				preemptorTemplate, err = testCtx.KubeClientset.ResourceV1().ResourceClaimTemplates(namespace).Create(ctx, preemptorTemplate, metav1.CreateOptions{})
				Expect(err).To(Succeed())

				pg := pod_group.Create(namespace, "distributed-preemptor-"+utils.GenerateRandomK8sName(6), testQueue.Name)
				pg.Spec.PriorityClassName = highPriority
				pg.Spec.MinMember = preemptorCount
				Expect(testCtx.ControllerClient.Create(ctx, pg)).To(Succeed())

				var preemptorPods []*v1.Pod
				for i := 0; i < preemptorCount; i++ {
					pod := rd.CreatePodObject(testQueue, v1.ResourceRequirements{
						Claims: []v1.ResourceClaim{
							{Name: "gpu", Request: "gpu"},
						},
					})
					pod.Name = "preemptor-" + utils.GenerateRandomK8sName(6)
					pod.Spec.ResourceClaims = []v1.PodResourceClaim{{
						Name:                      "gpu",
						ResourceClaimTemplateName: ptr.To(preemptorTemplate.Name),
					}}
					pod.Annotations[pod_group.PodGroupNameAnnotation] = pg.Name
					pod.Labels[pod_group.PodGroupNameAnnotation] = pg.Name
					pod.Spec.PriorityClassName = highPriority
					createdPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
					Expect(err).To(Succeed())
					preemptorPods = append(preemptorPods, createdPod)
				}

				wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, preemptorPods, 2)
			})
		})
	})
}
