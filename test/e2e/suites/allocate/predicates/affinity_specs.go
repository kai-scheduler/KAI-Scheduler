/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package predicates

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

const (
	specPodAffinityLabelName  = "pod-affinity-test"
	specPodAffinityLabelValue = "true"
)

func DescribeAffinitySpecs() bool {
	return Describe("Affinity", Ordered, func() {
		var (
			testCtx   *testcontext.TestContext
			namespace string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
			namespace = queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		Context("Node Affinity", func() {
			var (
				nodeWithGPU string
			)

			BeforeAll(func(ctx context.Context) {
				capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
					{
						Gpu:      resource.MustParse("1"),
						PodCount: 1,
					},
				})
				pod := specSubmitWaitAndGetPod(ctx, testCtx, testCtx.Queues[0], nil, map[string]string{})
				nodeWithGPU = pod.Spec.NodeName
				err := testCtx.ControllerClient.Delete(ctx, pod)
				Expect(err).To(Succeed())
			})

			It("schedules pod to node with correct affinity", func(ctx context.Context) {
				pod := specSubmitWaitAndGetPod(
					ctx, testCtx, testCtx.Queues[0], rd.NodeAffinity(nodeWithGPU, v1.NodeSelectorOpIn), map[string]string{},
				)
				Expect(pod.Spec.NodeName).To(Equal(nodeWithGPU))
			})

			It("doesn't schedule pod to nodes without requested affinity", func(ctx context.Context) {
				cpuNode := rd.FindNodeWithNoGPU(ctx, testCtx.ControllerClient, testCtx.KubeClientset)
				if cpuNode == nil {
					Skip("Failed to find node without GPUs")
				}
				pod := specSubmitPod(ctx, testCtx.KubeClientset,
					testCtx.Queues[0], rd.NodeAffinity(cpuNode.Name, v1.NodeSelectorOpIn), map[string]string{},
				)
				wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
			})
		})

		Context("Pod Affinity", func() {
			It("Not scheduling if pod affinity condition is not met", func(ctx context.Context) {
				capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
					{
						Gpu:      resource.MustParse("1"),
						PodCount: 1,
					},
					{
						Gpu:      resource.MustParse("1"),
						PodCount: 1,
					},
				})
				podAffinityLabels := map[string]string{specPodAffinityLabelName: specPodAffinityLabelValue}
				podAffinity := &v1.Affinity{
					PodAffinity: &v1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: podAffinityLabels,
								},
								Namespaces:  []string{namespace},
								TopologyKey: constant.NodeNamePodLabelName,
							},
						},
					},
				}
				pod1 := specSubmitWaitAndGetPod(
					ctx, testCtx, testCtx.Queues[0], podAffinity, podAffinityLabels)

				podAffinityToPod1WithNodeAffinityToDifferentNode := &v1.Affinity{
					PodAffinity:  podAffinity.PodAffinity,
					NodeAffinity: rd.NodeAffinity(pod1.Spec.NodeName, v1.NodeSelectorOpNotIn).NodeAffinity,
				}
				pod2 := specSubmitPod(ctx, testCtx.KubeClientset, testCtx.Queues[0],
					podAffinityToPod1WithNodeAffinityToDifferentNode, podAffinityLabels)

				wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod2)
			})
		})
	})
}

func specSubmitPod(
	ctx context.Context, client *kubernetes.Clientset,
	queue *v2.Queue, affinity *v1.Affinity,
	extraLabels map[string]string,
) *v1.Pod {
	requirements := v1.ResourceRequirements{
		Limits: map[v1.ResourceName]resource.Quantity{
			constants.NvidiaGpuResource: resource.MustParse("1"),
		},
	}
	pod := rd.CreatePodObject(queue, requirements)
	pod.Spec.Affinity = affinity
	maps.Copy(pod.ObjectMeta.Labels, extraLabels)
	pod, err := rd.CreatePod(ctx, client, pod)
	Expect(err).To(Succeed())
	return pod
}

func specSubmitWaitAndGetPod(
	ctx context.Context, testCtx *testcontext.TestContext,
	queue *v2.Queue, affinity *v1.Affinity,
	extraLabels map[string]string,
) *v1.Pod {
	pod := specSubmitPod(ctx, testCtx.KubeClientset, queue, affinity, extraLabels)
	wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
	Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), pod)).To(Succeed())
	return pod
}
