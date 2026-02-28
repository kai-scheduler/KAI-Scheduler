/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resources

import (
	"context"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

const numFractionRaceRounds = 5

// DescribeReservationPodRaceSpecs tests for a race condition in the binder's
// resource reservation sync logic. When many 0.5 fraction pods bind
// concurrently under binpack mode, the informer cache may lag behind the API
// server. This causes SyncForGpuGroup to see a reservation pod but no fraction
// pod for a GPU group, leading to premature reservation pod deletion and stuck
// workloads.
func DescribeReservationPodRaceSpecs() bool {
	return Describe("Reservation pod race under concurrent fraction binding",
		Label(labels.ReservationPod, labels.Operated), Ordered, func() {
			var (
				testCtx *testcontext.TestContext
			)

			BeforeAll(func(ctx context.Context) {
				testCtx = testcontext.GetConnectivity(ctx, Default)
				capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
					&capacity.ResourceList{
						Gpu:      resource.MustParse("4"),
						PodCount: 16,
					},
				)

				parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
				childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
				testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

				Expect(feature_flags.SetPlacementStrategy(ctx, testCtx, "binpack")).To(Succeed(),
					"Failed to set binpack placement strategy")
			})

			AfterAll(func(ctx context.Context) {
				err := feature_flags.SetPlacementStrategy(ctx, testCtx, "binpack")
				if err != nil {
					fmt.Printf("Warning: failed to restore placement strategy: %v\n", err)
				}
				testCtx.ClusterCleanup(ctx)
			})

			AfterEach(func(ctx context.Context) {
				testCtx.TestContextCleanup(ctx)
			})

			// Creates many pairs of 0.5 fraction pods concurrently across
			// multiple rounds. Under binpack, each pair targets the same GPU.
			// High concurrency increases the chance of triggering the race
			// where a reservation pod is prematurely deleted due to cache lag.
			It("should schedule all fraction pod pairs without losing reservation pods", func(ctx context.Context) {
				resources, err := capacity.GetClusterAllocatableResources(testCtx.KubeClientset)
				Expect(err).NotTo(HaveOccurred())
				numGPUs := int(resources.Gpu.Value())

				for round := range numFractionRaceRounds {
					numPods := numGPUs * 2
					By(fmt.Sprintf("Round %d/%d: creating %d fraction pods (0.5 each, 2 per GPU)",
						round+1, numFractionRaceRounds, numPods))

					pods := make([]*v1.Pod, numPods)
					for i := range numPods {
						pods[i] = rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
						pods[i].Annotations = map[string]string{
							constants.GpuFraction: "0.5",
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
						Expect(err).NotTo(HaveOccurred(), "Round %d: failed to create pod", round+1)
					}

					namespace := pods[0].Namespace
					wait.ForPodsReady(ctx, testCtx.ControllerClient, namespace, pods)

					By(fmt.Sprintf("Round %d/%d: verifying scheduling and reservation pods", round+1, numFractionRaceRounds))
					var podList v1.PodList
					Expect(testCtx.ControllerClient.List(ctx, &podList,
						runtimeClient.InNamespace(namespace),
						runtimeClient.MatchingLabels{constants.AppLabelName: "engine-e2e"},
					)).To(Succeed())

					scheduledCount := 0
					gpuGroups := map[string]int{}
					for _, pod := range podList.Items {
						if !rd.IsPodScheduled(&pod) {
							continue
						}
						scheduledCount++
						group, ok := pod.Labels[constants.GPUGroup]
						Expect(ok).To(BeTrue(),
							"Round %d: pod %s should have GPU group label", round+1, pod.Name)
						gpuGroups[group]++
					}

					Expect(scheduledCount).To(Equal(numPods),
						"Round %d: expected %d pods scheduled, got %d", round+1, numPods, scheduledCount)

					for group, count := range gpuGroups {
						Expect(count).To(Equal(2),
							"Round %d: GPU group %s should have 2 pods, got %d", round+1, group, count)
					}

					// Verify reservation pods exist for each active GPU group.
					// The race causes premature deletion of reservation pods.
					reservationNamespace := testconfig.GetConfig().ReservationNamespace
					var reservationPods v1.PodList
					Expect(testCtx.ControllerClient.List(ctx, &reservationPods,
						runtimeClient.InNamespace(reservationNamespace),
					)).To(Succeed())

					reservationGroups := map[string]bool{}
					for _, rPod := range reservationPods.Items {
						group := rPod.Labels[constants.GPUGroup]
						if group != "" {
							reservationGroups[group] = true
						}
					}

					for group := range gpuGroups {
						Expect(reservationGroups).To(HaveKey(group),
							"Round %d: reservation pod missing for GPU group %s", round+1, group)
					}

					// Clean up between rounds
					By(fmt.Sprintf("Round %d/%d: cleaning up", round+1, numFractionRaceRounds))
					Expect(rd.DeleteAllPodsInNamespace(ctx, testCtx.ControllerClient, namespace)).To(Succeed())
					Expect(rd.DeleteAllConfigMapsInNamespace(ctx, testCtx.ControllerClient, namespace)).To(Succeed())
					wait.ForNoE2EPods(ctx, testCtx.ControllerClient)
					wait.ForNoReservationPods(ctx, testCtx.ControllerClient)
					time.Sleep(2 * time.Second)
				}
			})
		})
}
