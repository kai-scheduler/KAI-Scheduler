/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"
	"encoding/json"

	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

var _ = DescribePreemptSpecs()

var _ = DescribePreemptDelaySpecs()

var _ = Describe("Priority Preemption", Ordered, func() {
	var (
		testCtx                         *testcontext.TestContext
		lowPreemptiblePriorityClass     string
		highPreemptiblePriorityClass    string
		lowNonPreemptiblePriorityClass  string
		highNonPreemptiblePriorityClass string
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
			Gpu:      resource.MustParse("1"),
			Cpu:      resource.MustParse("500m"),
			PodCount: 1,
		})

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		testQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testQueue.Spec.Resources.CPU.Quota = 500
		testQueue.Spec.Resources.CPU.Limit = 500
		testCtx.InitQueues([]*v2.Queue{testQueue, parentQueue})

		lowPreemptiblePriorityClass = utils.GenerateRandomK8sName(10)
		lowPreemptiblePriorityValue := utils.RandomIntBetween(0, constant.NonPreemptiblePriorityThreshold-2)
		_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(lowPreemptiblePriorityClass, lowPreemptiblePriorityValue),
				metav1.CreateOptions{})
		Expect(err).To(Succeed())

		highPreemptiblePriorityClass = utils.GenerateRandomK8sName(10)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(highPreemptiblePriorityClass, lowPreemptiblePriorityValue+1),
				metav1.CreateOptions{})
		Expect(err).To(Succeed())

		lowNonPreemptiblePriorityClass = utils.GenerateRandomK8sName(10)
		lowNonPreemptiblePriorityValue := utils.RandomIntBetween(constant.NonPreemptiblePriorityThreshold,
			constant.NonPreemptiblePriorityThreshold*2)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(lowNonPreemptiblePriorityClass, lowNonPreemptiblePriorityValue),
				metav1.CreateOptions{})
		Expect(err).To(Succeed())

		highNonPreemptiblePriorityClass = utils.GenerateRandomK8sName(10)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(highNonPreemptiblePriorityClass, lowNonPreemptiblePriorityValue+1),
				metav1.CreateOptions{})
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

	Context("Dynamic Resources", func() {
		var (
			namespace string
		)

		BeforeAll(func(ctx context.Context) {
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, constant.GPUDeviceClassName, 1, 1)
			namespace = queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		})

		AfterEach(func(ctx context.Context) {
			capacity.CleanupResourceClaims(ctx, testCtx.KubeClientset, namespace)
		})

		It("Preempts a pod based on DRA contention - dra template claims", func(ctx context.Context) {
			nodeName := ""
			devices := 0
			nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, constant.GPUDeviceClassName)
			for name, deviceCount := range nodesMap {
				if deviceCount <= 1 {
					continue
				}
				nodeName = name
				devices = deviceCount
			}
			Expect(nodeName).ToNot(Equal(""), "failed to find a node with multiple devices")

			claimTemplate := rd.CreateResourceClaimTemplate(namespace, testCtx.Queues[0].Name, constant.GPUDeviceClassName, 1)
			claimTemplate, err := testCtx.KubeClientset.ResourceV1().ResourceClaimTemplates(namespace).Create(ctx, claimTemplate, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			var pods []*v1.Pod
			for range devices {
				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
					Claims: []v1.ResourceClaim{
						{
							Name:    "claim-template",
							Request: "claim-template",
						},
					},
				})
				pod.Spec.ResourceClaims = []v1.PodResourceClaim{{
					Name:                      "claim-template",
					ResourceClaimTemplateName: ptr.To(claimTemplate.Name),
				}}

				pod.Spec.Affinity = &v1.Affinity{
					NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      v1.LabelHostname,
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{nodeName},
										},
									},
								},
							},
						},
					},
				}

				pod.Spec.PriorityClassName = lowPreemptiblePriorityClass

				pod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred(), "failed to create filler pod")
				pods = append(pods, pod)
			}

			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)
			wait.ForPodsReady(ctx, testCtx.ControllerClient, namespace, pods)

			unschedulablePod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Claims: []v1.ResourceClaim{
					{
						Name:    "claim-template",
						Request: "claim-template",
					},
				},
			})
			unschedulablePod.Spec.ResourceClaims = []v1.PodResourceClaim{{
				Name:                      "claim-template",
				ResourceClaimTemplateName: ptr.To(claimTemplate.Name),
			}}

			unschedulablePod.Spec.Affinity = &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      v1.LabelHostname,
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{nodeName},
									},
								},
							},
						},
					},
				},
			}

			unschedulablePod.Spec.PriorityClassName = lowPreemptiblePriorityClass

			unschedulablePod, err = rd.CreatePod(ctx, testCtx.KubeClientset, unschedulablePod)
			Expect(err).NotTo(HaveOccurred(), "failed to create filler pod")

			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, unschedulablePod)

			schedulablePod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Claims: []v1.ResourceClaim{
					{
						Name:    "claim-template",
						Request: "claim-template",
					},
				},
			})
			schedulablePod.Spec.ResourceClaims = []v1.PodResourceClaim{{
				Name:                      "claim-template",
				ResourceClaimTemplateName: ptr.To(claimTemplate.Name),
			}}

			schedulablePod.Spec.Affinity = &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      v1.LabelHostname,
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{nodeName},
									},
								},
							},
						},
					},
				},
			}
			schedulablePod.Spec.PriorityClassName = highPreemptiblePriorityClass
			schedulablePod, err = rd.CreatePod(ctx, testCtx.KubeClientset, schedulablePod)
			Expect(err).NotTo(HaveOccurred(), "failed to create preemptor pod")

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, schedulablePod)
			wait.ForPodReady(ctx, testCtx.ControllerClient, schedulablePod)
		})
	})

	Context("Dynamic Resources - Extended Resources", func() {
		// Tests KEP-5004: classic nvidia.com/gpu syntax routed through DRA.
		var (
			namespace                   string
			extResourcePreemptClassName = ""
		)

		BeforeAll(func(ctx context.Context) {
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, constant.GPUDeviceClassName, 1, 2)
			namespace = queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			By("patching DeviceClass to advertise extendedResourceName")
			patch, _ := json.Marshal(map[string]any{
				"spec": map[string]any{"extendedResourceName": constant.NvidiaGPUResource},
			})
			_, err := testCtx.KubeClientset.ResourceV1().DeviceClasses().Patch(
				ctx, constant.GPUDeviceClassName, types.MergePatchType, patch, metav1.PatchOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func(ctx context.Context) {
			By("reverting DeviceClass extendedResourceName")
			patch, _ := json.Marshal(map[string]any{
				"spec": map[string]any{"extendedResourceName": nil},
			})
			_, _ = testCtx.KubeClientset.ResourceV1().DeviceClasses().Patch(
				ctx, constant.GPUDeviceClassName, types.MergePatchType, patch, metav1.PatchOptions{})

			if extResourcePreemptClassName != "" {
				_ = testCtx.KubeClientset.SchedulingV1().PriorityClasses().Delete(
					context.Background(), extResourcePreemptClassName, metav1.DeleteOptions{})
			}
		})

		AfterEach(func(ctx context.Context) {
			capacity.CleanupResourceClaims(ctx, testCtx.KubeClientset, namespace)
		})

		It("preempts an extended-resource pod using classic nvidia.com/gpu syntax", func(ctx context.Context) {
			nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, constant.GPUDeviceClassName)
			var nodeName string
			var devices int
			for name, count := range nodesMap {
				if count >= 2 {
					nodeName = name
					devices = count
					break
				}
			}
			if nodeName == "" {
				Skip("no DRA node with ≥ 2 devices found")
			}

			// Give the preemptor strictly higher priority than the fillers.
			extResourcePreemptClassName = utils.GenerateRandomK8sName(10)
			_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().Create(
				ctx, rd.CreatePriorityClass(extResourcePreemptClassName, constant.NonPreemptiblePriorityThreshold-1),
				metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			gpuReq := v1.ResourceRequirements{
				Requests: v1.ResourceList{v1.ResourceName(constant.NvidiaGPUResource): resource.MustParse("1")},
				Limits:   v1.ResourceList{v1.ResourceName(constant.NvidiaGPUResource): resource.MustParse("1")},
			}

			By("filling the node with low-priority extended-resource pods")
			var fillers []*v1.Pod
			for range devices {
				pod := rd.CreatePodObject(testCtx.Queues[0], gpuReq)
				pod.Spec.Affinity = rd.NodeAffinity(nodeName, v1.NodeSelectorOpIn)
				pod.Spec.PriorityClassName = lowPreemptiblePriorityClass
				pod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())
				fillers = append(fillers, pod)
			}
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, fillers)
			wait.ForPodsReady(ctx, testCtx.ControllerClient, namespace, fillers)

			By("creating a higher-priority extended-resource pod that must preempt")
			preemptor := rd.CreatePodObject(testCtx.Queues[0], gpuReq)
			preemptor.Spec.Affinity = rd.NodeAffinity(nodeName, v1.NodeSelectorOpIn)
			preemptor.Spec.PriorityClassName = extResourcePreemptClassName
			preemptor, err = rd.CreatePod(ctx, testCtx.KubeClientset, preemptor)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, preemptor)
			wait.ForPodReady(ctx, testCtx.ControllerClient, preemptor)

			By("verifying the preemptor has a synthetic ResourceClaim")
			Eventually(func() bool {
				claims, err := testCtx.KubeClientset.ResourceV1().ResourceClaims(namespace).List(ctx, metav1.ListOptions{})
				if err != nil {
					return false
				}
				for i := range claims.Items {
					c := &claims.Items[i]
					if c.Annotations[resourceapi.ExtendedResourceClaimAnnotation] != "true" {
						continue
					}
					for _, ref := range c.OwnerReferences {
						if ref.Name == preemptor.Name && ref.Controller != nil && *ref.Controller {
							return true
						}
					}
				}
				return false
			}, "30s", "1s").Should(BeTrue(), "preemptor should have a synthetic ResourceClaim")
		})
	})

	Context("Preemptability-Priority Separation", func() {
		It("High priority preemptible Pod should be preemptible", func(ctx context.Context) {
			mediumPriorityPreemptiblePod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("500m"),
				},
			})
			mediumPriorityPreemptiblePod.Spec.PriorityClassName = lowNonPreemptiblePriorityClass
			mediumPriorityPreemptiblePod.Labels["kai.scheduler/preemptibility"] = string(v2alpha2.Preemptible)
			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, mediumPriorityPreemptiblePod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, mediumPriorityPreemptiblePod)

			createdPod := &v1.Pod{}
			err = testCtx.ControllerClient.Get(ctx, types.NamespacedName{
				Name:      mediumPriorityPreemptiblePod.Name,
				Namespace: mediumPriorityPreemptiblePod.Namespace,
			}, createdPod)
			Expect(err).To(Succeed())
			podGroupName := createdPod.Annotations[constants.PodGroupAnnotationForPod]
			Expect(podGroupName).To(Not(BeEmpty()))
			podGroup := &v2alpha2.PodGroup{}
			err = testCtx.ControllerClient.Get(ctx, types.NamespacedName{
				Name:      podGroupName,
				Namespace: mediumPriorityPreemptiblePod.Namespace,
			}, podGroup)
			Expect(err).To(Succeed())
			Expect(podGroup.Spec.Preemptibility).To(Equal(v2alpha2.Preemptible))

			higherPriorityPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("500m"),
				},
			})
			higherPriorityPod.Spec.PriorityClassName = highNonPreemptiblePriorityClass
			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, higherPriorityPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, higherPriorityPod)
		})

		It("Low priority non-preemptible Pod should not be preemptible", func(ctx context.Context) {
			lowPriorityNonPreemptiblePod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("500m"),
				},
			})
			lowPriorityNonPreemptiblePod.Spec.PriorityClassName = lowPreemptiblePriorityClass
			lowPriorityNonPreemptiblePod.Labels["kai.scheduler/preemptibility"] = string(v2alpha2.NonPreemptible)
			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, lowPriorityNonPreemptiblePod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, lowPriorityNonPreemptiblePod)

			createdPod := &v1.Pod{}
			err = testCtx.ControllerClient.Get(ctx, types.NamespacedName{
				Name:      lowPriorityNonPreemptiblePod.Name,
				Namespace: lowPriorityNonPreemptiblePod.Namespace,
			}, createdPod)
			Expect(err).To(Succeed())
			podGroupName := createdPod.Annotations[constants.PodGroupAnnotationForPod]
			Expect(podGroupName).To(Not(BeEmpty()))
			podGroup := &v2alpha2.PodGroup{}
			err = testCtx.ControllerClient.Get(ctx, types.NamespacedName{
				Name:      podGroupName,
				Namespace: lowPriorityNonPreemptiblePod.Namespace,
			}, podGroup)
			Expect(err).To(Succeed())
			Expect(podGroup.Spec.Preemptibility).To(Equal(v2alpha2.NonPreemptible))

			higherPriorityPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("500m"),
				},
			})
			higherPriorityPod.Spec.PriorityClassName = highPreemptiblePriorityClass
			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, higherPriorityPod)
			Expect(err).To(Succeed())
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, higherPriorityPod)
		})
	})
})
