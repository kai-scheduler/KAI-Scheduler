/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"

	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

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

	It("High preemptible PREEMPTING low preemptible", func(ctx context.Context) {
		lowPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		lowPod.Spec.PriorityClassName = lowPreemptiblePriorityClass
		_, err := rd.CreatePod(ctx, testCtx.KubeClientset, lowPod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, lowPod)

		highPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		highPod.Spec.PriorityClassName = highPreemptiblePriorityClass
		_, err = rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)
	})

	It("High non preemptible PREEMPTING low preemptible", func(ctx context.Context) {
		lowPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		lowPod.Spec.PriorityClassName = lowPreemptiblePriorityClass
		lowPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, lowPod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, lowPod)

		highPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		highPod.Spec.PriorityClassName = highNonPreemptiblePriorityClass
		highPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)
	})

	It("High non preemptible should not preempt low non preemptible ", func(ctx context.Context) {
		lowPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		lowPod.Spec.PriorityClassName = lowNonPreemptiblePriorityClass
		lowPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, lowPod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, lowPod)

		highPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		highPod.Spec.PriorityClassName = highNonPreemptiblePriorityClass
		highPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
		Expect(err).To(Succeed())
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, highPod)
	})

	It("Low preemptible should not preempt high preemptible ", func(ctx context.Context) {
		highPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		highPod.Spec.PriorityClassName = highPreemptiblePriorityClass
		highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

		lowPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		lowPod.Spec.PriorityClassName = lowPreemptiblePriorityClass
		lowPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, lowPod)
		Expect(err).To(Succeed())
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, lowPod)
	})

	It("Low non preemptible should not preempt high non preemptible ", func(ctx context.Context) {
		highPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		highPod.Spec.PriorityClassName = highNonPreemptiblePriorityClass
		highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

		lowPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		lowPod.Spec.PriorityClassName = lowNonPreemptiblePriorityClass
		lowPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, lowPod)
		Expect(err).To(Succeed())
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, lowPod)
	})

	It("Low preemptible should not preempt high non preemptible ", func(ctx context.Context) {
		highPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		highPod.Spec.PriorityClassName = highNonPreemptiblePriorityClass
		highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

		lowPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		lowPod.Spec.PriorityClassName = lowPreemptiblePriorityClass
		lowPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, lowPod)
		Expect(err).To(Succeed())
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, lowPod)
	})

	It("Same priority preemptible should not preempt Same priority preemptible ", func(ctx context.Context) {
		pod1 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		pod1.Spec.PriorityClassName = lowPreemptiblePriorityClass
		pod1, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)

		pod2 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		pod2.Spec.PriorityClassName = lowPreemptiblePriorityClass
		pod2, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
		Expect(err).To(Succeed())
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod2)
	})

	It("Same priority non preemptible should not preempt Same priority non preemptible ", func(ctx context.Context) {
		pod1 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		pod1.Spec.PriorityClassName = lowNonPreemptiblePriorityClass
		pod1, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)

		pod2 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("500m"),
			},
		})
		pod2.Spec.PriorityClassName = lowNonPreemptiblePriorityClass
		pod2, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
		Expect(err).To(Succeed())
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod2)
	})

	Context("Dynamic Resources", func() {
		var (
			deviceClassName = "gpu.example.com"
			namespace       string
		)

		BeforeAll(func(ctx context.Context) {
			capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, deviceClassName, 1, 1)
			namespace = queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		})

		AfterEach(func(ctx context.Context) {
			capacity.CleanupResourceClaims(ctx, testCtx.KubeClientset, namespace)
		})

		It("Preempts a pod based on DRA contention", func(ctx context.Context) {
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
})
