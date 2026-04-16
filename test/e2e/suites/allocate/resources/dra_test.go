/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resources

import (
	"context"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Schedule pod with dynamic resource request", Ordered, func() {
	var (
		testCtx   *testcontext.TestContext
		namespace string
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)

		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
		namespace = queue.GetConnectedNamespaceToQueue(childQueue)
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	Context("Dynamic Resources", func() {
		var (
			deviceClassName = "gpu.example.com"
		)

		BeforeAll(func(ctx context.Context) {
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, deviceClassName, 1, 1)
		})

		AfterEach(func(ctx context.Context) {
			capacity.CleanupResourceClaims(ctx, testCtx.KubeClientset, namespace)
		})

		It("Allocate simple request", func(ctx context.Context) {
			claim := rd.CreateResourceClaim(namespace, deviceClassName, 1)
			claim, err := testCtx.KubeClientset.ResourceV1().ResourceClaims(namespace).Create(ctx, claim, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			// Wait for the ResourceClaim to be accessible via the controller client
			// This ensures ExtractDRAGPUResources can successfully read the claim
			Eventually(func() error {
				claimObj := &resourceapi.ResourceClaim{}
				return testCtx.ControllerClient.Get(ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      claim.Name,
				}, claimObj)
			}).Should(Succeed(), "ResourceClaim should be accessible via controller client")

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Claims: []v1.ResourceClaim{
					{
						Name:    "claim",
						Request: "claim",
					},
				},
			})
			pod.Spec.ResourceClaims = []v1.PodResourceClaim{{
				Name:              "claim",
				ResourceClaimName: ptr.To(claim.Name),
			}}

			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			wait.ForPodReady(ctx, testCtx.ControllerClient, pod)
		})

		It("Fails to allocate request with wrong device class", func(ctx context.Context) {
			claim := rd.CreateResourceClaim(namespace, "fake-device-class", 1)
			claim, err := testCtx.KubeClientset.ResourceV1().ResourceClaims(namespace).Create(ctx, claim, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			// Wait for the ResourceClaim to be accessible via the controller client
			// This ensures ExtractDRAGPUResources can successfully read the claim
			Eventually(func() error {
				claimObj := &resourceapi.ResourceClaim{}
				return testCtx.ControllerClient.Get(ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      claim.Name,
				}, claimObj)
			}).Should(Succeed(), "ResourceClaim should be accessible via controller client")

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Claims: []v1.ResourceClaim{
					{
						Name:    "claim",
						Request: "claim",
					},
				},
			})
			pod.Spec.ResourceClaims = []v1.PodResourceClaim{{
				Name:              "claim",
				ResourceClaimName: ptr.To(claim.Name),
			}}

			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
		})

		It("Fills a node", func(ctx context.Context) {
			nodeName := ""
			devices := 0
			nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, deviceClassName)
			for name, deviceCount := range nodesMap {
				if deviceCount <= 1 {
					continue
				}
				nodeName = name
				devices = deviceCount
			}
			Expect(nodeName).ToNot(Equal(""), "failed to find a node with multiple devices")

			claimTemplate := rd.CreateResourceClaimTemplate(namespace, deviceClassName, 1)
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

			unschedulablePod, err = rd.CreatePod(ctx, testCtx.KubeClientset, unschedulablePod)
			Expect(err).NotTo(HaveOccurred(), "failed to create filler pod")

			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, unschedulablePod)
		})
	})

	Context("DRA GPU Queue Bookkeeping", func() {
		var (
			gpuDeviceClassName = constants.GpuResource // "nvidia.com/gpu"
		)

		BeforeAll(func(ctx context.Context) {
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, gpuDeviceClassName, 1, 2)
		})

		AfterEach(func(ctx context.Context) {
			capacity.CleanupResourceClaims(ctx, testCtx.KubeClientset, namespace)
		})

		It("Should track DRA GPU resources in queue status", func(ctx context.Context) {
			// Create ResourceClaims for GPU devices
			claim1 := rd.CreateResourceClaim(namespace, gpuDeviceClassName, 1)
			claim1, err := testCtx.KubeClientset.ResourceV1().ResourceClaims(namespace).Create(ctx, claim1, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			claim2 := rd.CreateResourceClaim(namespace, gpuDeviceClassName, 2)
			claim2, err = testCtx.KubeClientset.ResourceV1().ResourceClaims(namespace).Create(ctx, claim2, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Wait for the ResourceClaims to be accessible via the controller client
			// This ensures ExtractDRAGPUResources can successfully read the claims
			Eventually(func() error {
				claimObj := &resourceapi.ResourceClaim{}
				return testCtx.ControllerClient.Get(ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      claim1.Name,
				}, claimObj)
			}).Should(Succeed(), "ResourceClaim1 should be accessible via controller client")

			Eventually(func() error {
				claimObj := &resourceapi.ResourceClaim{}
				return testCtx.ControllerClient.Get(ctx, types.NamespacedName{
					Namespace: namespace,
					Name:      claim2.Name,
				}, claimObj)
			}).Should(Succeed(), "ResourceClaim2 should be accessible via controller client")

			// Create PodGroup
			podGroupName := utils.GenerateRandomK8sName(10)
			_, err = testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podGroupName,
						Namespace: namespace,
						Labels: map[string]string{
							constants.AppLabelName: "engine-e2e",
						},
					},
					Spec: v2alpha2.PodGroupSpec{
						MinMember: 2,
						Queue:     testCtx.Queues[0].Name,
					},
				}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Create first pod with claim1 (1 GPU)
			pod1 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod1.Name = utils.GenerateRandomK8sName(10)
			pod1.Annotations[constants.PodGroupAnnotationForPod] = podGroupName
			pod1.Labels[constants.PodGroupAnnotationForPod] = podGroupName
			pod1.Spec.ResourceClaims = []v1.PodResourceClaim{
				{
					Name:              "gpu-claim-1",
					ResourceClaimName: ptr.To(claim1.Name),
				},
			}
			pod1, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
			Expect(err).NotTo(HaveOccurred())

			// Create second pod with claim2 (2 GPUs)
			pod2 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod2.Name = utils.GenerateRandomK8sName(10)
			pod2.Annotations[constants.PodGroupAnnotationForPod] = podGroupName
			pod2.Labels[constants.PodGroupAnnotationForPod] = podGroupName
			pod2.Spec.ResourceClaims = []v1.PodResourceClaim{
				{
					Name:              "gpu-claim-2",
					ResourceClaimName: ptr.To(claim2.Name),
				},
			}
			pod2, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
			Expect(err).NotTo(HaveOccurred())

			// Wait for pods to be scheduled
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

			// Wait for pods to be ready
			wait.ForPodReady(ctx, testCtx.ControllerClient, pod1)
			wait.ForPodReady(ctx, testCtx.ControllerClient, pod2)

			// Wait a bit for queue controller to reconcile
			// Then verify queue status contains DRA GPU resources
			Eventually(func() v2.QueueStatus {
				queue, err := testCtx.KubeAiSchedClientset.SchedulingV2().Queues(namespace).Get(ctx, testCtx.Queues[0].Name, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				return queue.Status
			}).Should(And(
				WithTransform(func(status v2.QueueStatus) bool {
					// Check that Requested contains GPU resources
					if gpuQty, found := status.Requested[v1.ResourceName(gpuDeviceClassName)]; found {
						return gpuQty.Value() >= 3 // At least 1 + 2 = 3 GPUs requested
					}
					return false
				}, BeTrue()),
				WithTransform(func(status v2.QueueStatus) bool {
					// Check that Allocated contains GPU resources
					if gpuQty, found := status.Allocated[v1.ResourceName(gpuDeviceClassName)]; found {
						return gpuQty.Value() >= 3 // At least 1 + 2 = 3 GPUs allocated
					}
					return false
				}, BeTrue()),
			))

			// Verify exact counts
			queue, err := testCtx.KubeAiSchedClientset.SchedulingV2().Queues(namespace).Get(ctx, testCtx.Queues[0].Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			requestedGPUs, found := queue.Status.Requested[v1.ResourceName(gpuDeviceClassName)]
			Expect(found).To(BeTrue(), "Queue Requested should have GPU resource")
			Expect(requestedGPUs.Value()).To(Equal(int64(3)), "Queue should have 3 GPUs requested (1 + 2)")

			allocatedGPUs, found := queue.Status.Allocated[v1.ResourceName(gpuDeviceClassName)]
			Expect(found).To(BeTrue(), "Queue Allocated should have GPU resource")
			Expect(allocatedGPUs.Value()).To(Equal(int64(3)), "Queue should have 3 GPUs allocated (1 + 2)")
		})
	})
})
