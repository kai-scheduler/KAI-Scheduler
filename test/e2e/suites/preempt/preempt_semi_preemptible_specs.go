/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

// A semi-preemptible PodGroup shaped like a segmented workload: minSubGroup=2 over 4 fully-gang
// segments (minMember=2 each). The 2 highest-priority segments are core (non-preemptible, in-quota);
// the other 2 are elastic and reclaimed a whole segment at a time. This verifies subgroup-level
// semi-preemptibility: under the old leaf-only model every segment would be core and nothing could
// be preempted, and a segment could be torn in half.
func DescribePreemptSemiPreemptibleSpecs() bool {
	return Describe("Semi-Preemptible with Segmented Subgroups", Ordered, func() {
		var testCtx *testcontext.TestContext

		const (
			numSegments  = 4
			segmentSize  = 2
			totalPods    = numSegments * segmentSize
			coreSegments = 2
			corePods     = coreSegments * segmentSize
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)

			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Cpu:      resource.MustParse("1200m"),
					PodCount: totalPods + 1,
				},
			})
		})

		AfterAll(func(ctx context.Context) {
			err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
			Expect(err).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("preempts whole elastic segments while preserving the core segments", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			// Quota covers the 2 core segments; limit allows bursting to all 4.
			lowQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			lowQueue.Spec.Resources.CPU.Quota = 400
			lowQueue.Spec.Resources.CPU.Limit = 800
			highQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			highQueue.Spec.Resources.CPU.Quota = 400
			highQueue.Spec.Resources.CPU.Limit = 600
			testCtx.InitQueues([]*v2.Queue{lowQueue, highQueue, parentQueue})

			lowNamespace := queue.GetConnectedNamespaceToQueue(lowQueue)
			cpuPerPod := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
				utils.GenerateRandomK8sName(10), lowQueue, ptr.To[int32](coreSegments),
				segmentLeaves("segment", numSegments, segmentSize), nil, v2alpha2.SemiPreemptible, cpuPerPod)

			// All segments schedule (2 core in-quota, 2 elastic over-quota).
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, h.AllPods, totalPods)

			// A higher-priority workload needing 2 segments' worth of CPU forces reclaim/preemption.
			highPod := rd.CreatePodObject(highQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("400m"),
				},
			})
			highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

			// The 2 elastic segments (corePods..totalPods) are preempted; the core segments survive.
			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, lowNamespace, h.AllPods, totalPods-corePods)

			// Exactly the core pods remain, and every segment is intact: never partially preempted.
			scheduledTotal := 0
			intactSegments := 0
			for segmentName, segmentPods := range h.Pods {
				scheduled := countScheduledPods(ctx, testCtx.ControllerClient, segmentPods)
				Expect(scheduled).To(Or(Equal(0), Equal(segmentSize)),
					fmt.Sprintf("segment %q must be wholly scheduled or wholly preempted, got %d/%d",
						segmentName, scheduled, segmentSize))
				scheduledTotal += scheduled
				if scheduled == segmentSize {
					intactSegments++
				}
			}
			Expect(scheduledTotal).To(Equal(corePods))
			Expect(intactSegments).To(Equal(coreSegments))
		})
	})
}

func segmentLeaves(prefix string, count, podsPerSegment int) []pod_group.SubGroupNode {
	nodes := make([]pod_group.SubGroupNode, 0, count)
	for i := 0; i < count; i++ {
		nodes = append(nodes, pod_group.SubGroupNode{
			Name:      fmt.Sprintf("%s-%d", prefix, i),
			MinMember: ptr.To(int32(podsPerSegment)),
			PodCount:  podsPerSegment,
		})
	}
	return nodes
}

func countScheduledPods(ctx context.Context, c runtimeClient.WithWatch, pods []*v1.Pod) int {
	scheduled := 0
	for _, pod := range pods {
		fetched := &v1.Pod{}
		if err := c.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), fetched); err != nil {
			continue
		}
		if rd.IsPodScheduled(fetched) {
			scheduled++
		}
	}
	return scheduled
}
