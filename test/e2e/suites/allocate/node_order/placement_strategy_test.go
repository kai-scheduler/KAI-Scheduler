/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package node_order

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	binPackingPluginName = "binpack"
	SpreadingPluginName  = "spread"
	DefaultPluginName    = binPackingPluginName
)

var _ = Describe("Placement strategy", Label(labels.Operated), Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
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

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	Context("Bin Packing", func() {
		BeforeAll(func(ctx context.Context) {
			if err := feature_flags.SetPlacementStrategy(ctx, testCtx, binPackingPluginName); err != nil {
				Fail(fmt.Sprintf("Failed to patch scheduler config with bin packing plugin: %v", err))
			}
		})

		AfterAll(func(ctx context.Context) {
			if err := feature_flags.SetPlacementStrategy(ctx, testCtx, DefaultPluginName); err != nil {
				Fail(fmt.Sprintf("Failed to patch scheduler config with default plugin: %v", err))
			}
		})

		It("Schedule on same node", func(ctx context.Context) {
			pod1 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})
			pod1, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
			Expect(err).To(Succeed())

			pod2 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})
			pod2, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

			scheduledPod1, err := testCtx.KubeClientset.CoreV1().Pods(pod1.Namespace).Get(ctx, pod1.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())
			scheduledPod2, err := testCtx.KubeClientset.CoreV1().Pods(pod2.Namespace).Get(ctx, pod2.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())

			Expect(scheduledPod1.Spec.NodeName).To(Equal(scheduledPod2.Spec.NodeName))
		})

		It("Schedule on same GPU", Label(labels.ReservationPod), func(ctx context.Context) {
			pod1 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod1.Annotations = map[string]string{
				constants.GpuFraction: "0.5",
			}
			pod1, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
			Expect(err).To(Succeed())

			pod2 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod2.Annotations = map[string]string{
				constants.GpuFraction: "0.5",
			}
			pod2, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

			scheduledPod1, err := testCtx.KubeClientset.CoreV1().Pods(pod1.Namespace).Get(ctx, pod1.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())
			scheduledPod2, err := testCtx.KubeClientset.CoreV1().Pods(pod2.Namespace).Get(ctx, pod2.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())

			pod1GpuIndex, err := getGpuIndexOfScheduledFractionPod(ctx, testCtx, scheduledPod1)
			Expect(err).To(Succeed())
			pod2GpuIndex, err := getGpuIndexOfScheduledFractionPod(ctx, testCtx, scheduledPod2)
			Expect(err).To(Succeed())

			Expect(pod1GpuIndex).To(Equal(pod2GpuIndex))
		})
	})

	Context("Spreading", func() {
		BeforeAll(func(ctx context.Context) {
			if err := feature_flags.SetPlacementStrategy(ctx, testCtx, SpreadingPluginName); err != nil {
				Fail(fmt.Sprintf("Failed to patch scheduler config with spreading plugin: %v", err))
			}
		})

		AfterAll(func(ctx context.Context) {
			if err := feature_flags.SetPlacementStrategy(ctx, testCtx, DefaultPluginName); err != nil {
				Fail(fmt.Sprintf("Failed to patch scheduler config with default plugin: %v", err))
			}
		})

		It("Schedule on different nodes", func(ctx context.Context) {
			pod1 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})
			pod1, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
			Expect(err).To(Succeed())

			pod2 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})
			pod2, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

			scheduledPod1, err := testCtx.KubeClientset.CoreV1().Pods(pod1.Namespace).Get(ctx, pod1.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())
			scheduledPod2, err := testCtx.KubeClientset.CoreV1().Pods(pod2.Namespace).Get(ctx, pod2.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())

			Expect(scheduledPod1.Spec.NodeName).ToNot(Equal(scheduledPod2.Spec.NodeName))
		})

		It("Schedule on different nodes - fractions", func(ctx context.Context) {
			pod1 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod1.Annotations = map[string]string{
				constants.GpuFraction: "0.5",
			}
			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
			Expect(err).To(Succeed())

			pod2 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod2.Annotations = map[string]string{
				constants.GpuFraction: "0.5",
			}
			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)

			scheduledPod1, err := testCtx.KubeClientset.CoreV1().Pods(pod1.Namespace).Get(ctx, pod1.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())
			scheduledPod2, err := testCtx.KubeClientset.CoreV1().Pods(pod2.Namespace).Get(ctx, pod2.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())

			Expect(scheduledPod1.Spec.NodeName).ToNot(Equal(scheduledPod2.Spec.NodeName))
		})

		It("Schedule on different gpus", Label(labels.ReservationPod), func(ctx context.Context) {
			pod1 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod1.Annotations = map[string]string{
				constants.GpuFraction: "0.5",
			}
			pod1, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)
			scheduledPod1, err := testCtx.KubeClientset.CoreV1().Pods(pod1.Namespace).Get(ctx, pod1.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())

			pod2 := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod2.Annotations = map[string]string{
				constants.GpuFraction: "0.5",
			}
			pod2.Spec.Affinity = &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      constant.NodeNamePodLabelName,
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{scheduledPod1.Spec.NodeName},
									},
								},
							},
						},
					},
				},
			}
			pod2, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)
			scheduledPod2, err := testCtx.KubeClientset.CoreV1().Pods(pod2.Namespace).Get(ctx, pod2.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())

			Expect(scheduledPod1.Spec.NodeName).To(Equal(scheduledPod2.Spec.NodeName))

			pod1GpuIndex, err := getGpuIndexOfScheduledFractionPod(ctx, testCtx, scheduledPod1)
			Expect(err).To(Succeed())
			pod2GpuIndex, err := getGpuIndexOfScheduledFractionPod(ctx, testCtx, scheduledPod2)
			Expect(err).To(Succeed())

			Expect(pod1GpuIndex).NotTo(Equal(pod2GpuIndex))
		})
	})
})

func getGpuIndexOfScheduledFractionPod(ctx context.Context, testCtx *testcontext.TestContext,
	pod *v1.Pod) (string, error) {
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == constants.NvidiaVisibleDevices {
				configMapName := env.ValueFrom.ConfigMapKeyRef.Name
				wait.ForConfigMapCreation(ctx, testCtx.ControllerClient, pod.Namespace, configMapName)
				cm, err := testCtx.KubeClientset.CoreV1().ConfigMaps(pod.Namespace).
					Get(ctx, configMapName, metav1.GetOptions{})
				if err != nil {
					return "", err
				}
				return cm.Data[env.ValueFrom.ConfigMapKeyRef.Key], nil
			}
		}
	}
	return "", fmt.Errorf("could not find %s env var in pod", constants.NvidiaVisibleDevices)
}
