/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resources

import (
	"context"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

func DescribeResourcesSpecs() bool {
	return Describe("Schedule pod with resource request", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
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

		It("No resource requests", func(ctx context.Context) {
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		Context("GPU Resources", func() {
			BeforeAll(func(ctx context.Context) {
				capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
					&capacity.ResourceList{
						Gpu:      resource.MustParse("1"),
						PodCount: 1,
					},
				)
			})

			It("Whole GPU request", func(ctx context.Context) {
				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{
						constants.NvidiaGpuResource: resource.MustParse("1"),
					},
				})

				_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			})

			It("Fraction GPU request", Label(labels.ReservationPod), func(ctx context.Context) {
				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
				pod.Annotations = map[string]string{
					constants.GpuFraction: "0.5",
				}

				_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())

				wait.ForPodReady(ctx, testCtx.ControllerClient, pod)
			})

			It("Fraction GPU request - fill all the GPUs", Label(labels.ReservationPod), func(ctx context.Context) {
				resources, err := capacity.GetClusterAllocatableResources(testCtx.KubeClientset)
				Expect(err).NotTo(HaveOccurred())
				numGPUs := int(resources.Gpu.Value())

				numPods := numGPUs * 2
				pods := make([]*v1.Pod, numPods)
				for i := range numPods {
					pods[i] = rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
					pods[i].Annotations = map[string]string{
						constants.GpuFraction: "0.7",
					}
				}
				errs := make(chan error, len(pods))
				var wg sync.WaitGroup
				for _, pod := range pods {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
						errs <- err
					}()
				}
				wg.Wait()
				close(errs)
				for err := range errs {
					Expect(err).NotTo(HaveOccurred(), "Failed to create pod")
				}

				wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, metav1.LabelSelector{
					MatchLabels: map[string]string{constants.AppLabelName: "engine-e2e"},
				}, numPods)

				labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{constants.AppLabelName: "engine-e2e"},
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      constants.GPUGroup,
							Operator: metav1.LabelSelectorOpExists,
						},
					},
				})
				wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(event watch.Event) bool {
					podList, ok := event.Object.(*v1.PodList)
					if !ok {
						return false
					}
					scheduledWithGpuGroup := 0
					for _, pod := range podList.Items {
						if !rd.IsPodScheduled(&pod) {
							continue
						}
						if _, ok := pod.Labels[constants.GPUGroup]; !ok {
							continue
						}
						scheduledWithGpuGroup++
					}
					return scheduledWithGpuGroup == numGPUs
				}, runtimeClient.InNamespace(testCtx.Queues[0].Namespace),
					runtimeClient.MatchingLabelsSelector{Selector: labelSelector})

				var allPods v1.PodList
				Expect(testCtx.ControllerClient.List(ctx, &allPods,
					runtimeClient.InNamespace(testCtx.Queues[0].Namespace),
					runtimeClient.MatchingLabelsSelector{Selector: labelSelector},
				)).To(Succeed(), "Failed to list pods")
				Expect(err).NotTo(HaveOccurred())
				Expect(len(allPods.Items)).To(BeNumerically("==", numGPUs), "Expected exactly %d allocated pods, got %d", numGPUs, len(allPods.Items))

				var allocatedPods []*v1.Pod
				var configMaps v1.ConfigMapList
				Expect(testCtx.ControllerClient.List(ctx, &configMaps,
					runtimeClient.InNamespace(testCtx.Queues[0].Namespace),
				)).To(Succeed(), "Failed to list config maps")

				gpuGroupsMap := make(map[string][]*v1.Pod)
				cmByGPUIndex := make(map[string][]*v1.ConfigMap)
				for _, pod := range allPods.Items {
					Expect(rd.IsPodScheduled(&pod)).To(BeTrue(), "Expected pod to be scheduled", pod.Name, pod.Labels, pod.Status.Conditions)
					allocatedPods = append(allocatedPods, &pod)

					group, ok := pod.Labels[constants.GPUGroup]
					Expect(ok).To(BeTrue(), "Expected GPU group label to be found on pod %s", pod.Name)
					gpuGroupsMap[group] = append(gpuGroupsMap[group], &pod)

					cmName := pod.Annotations[constants.GpuSharingConfigMapAnnotation]
					for _, cm := range configMaps.Items {
						if cm.Name != cmName {
							continue
						}
						index := cm.Data[constants.NvidiaVisibleDevices]
						cmByGPUIndex[index] = append(cmByGPUIndex[index], &cm)
						break
					}
				}
				Expect(len(allocatedPods)).To(BeNumerically("==", numGPUs), "Expected exactly %d allocated pods, got %d", numGPUs, len(allocatedPods))

				for group, pods := range gpuGroupsMap {
					Expect(len(pods)).To(Equal(1), "Expected one pod per group, got %d for group %s", len(pods), group)
				}

				for index, cm := range cmByGPUIndex {
					Expect(len(cm)).To(Equal(1), "Expected one config map per gpu index, got %d for index %s", len(cm), index)
				}
			})

			It("GPU memory request - valid", Label(labels.ReservationPod), func(ctx context.Context) {
				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
				pod.Annotations = map[string]string{
					constants.GpuMemory: "500",
				}

				_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			})

			It("GPU memory request - too much memory for a single gpu", Label(labels.ReservationPod),
				func(ctx context.Context) {
					pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
					pod.Annotations = map[string]string{
						constants.GpuMemory: "500000000",
					}

					_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
					Expect(err).NotTo(HaveOccurred())

					wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
				})
		})

		Context("CPU Resources", func() {
			It("CPU Request", func(ctx context.Context) {
				capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
					&capacity.ResourceList{
						Cpu:      resource.MustParse("1"),
						PodCount: 1,
					},
				)

				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1"),
					},
				})

				_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())

				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			})

			It("Memory Request", func(ctx context.Context) {
				capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
					&capacity.ResourceList{
						Memory:   resource.MustParse("100M"),
						PodCount: 1,
					},
				)

				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{
						v1.ResourceMemory: resource.MustParse("100M"),
					},
				})

				_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			})

			It("Other Resource Request", func(ctx context.Context) {
				capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
					&capacity.ResourceList{
						PodCount: 1,
						OtherResources: map[v1.ResourceName]resource.Quantity{
							v1.ResourceEphemeralStorage: resource.MustParse("100M"),
						},
					},
				)

				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{
						v1.ResourceEphemeralStorage: resource.MustParse("100M"),
					},
				})

				_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			})
		})
	})
}
