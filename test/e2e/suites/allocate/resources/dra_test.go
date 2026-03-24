/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resources

import (
	"context"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
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
})
