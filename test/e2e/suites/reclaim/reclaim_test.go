/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"

	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = DescribeReclaimSpecs()

var _ = Describe("Reclaim", Ordered, func() {
	Context("Quota/Fair-share based reclaim", func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Gpu:      resource.MustParse("4"),
					PodCount: 4,
				},
			})
		})

		AfterEach(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("Simple priority reclaim - but has min runtime", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 1, 0)
			reclaimerQueue.Spec.Priority = pointer.Int(constants.DefaultQueuePriority + 1)
			reclaimeeQueue.Spec.ReclaimMinRuntime = &metav1.Duration{Duration: 60 * time.Second}
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			pod := CreatePod(ctx, testCtx, reclaimeeQueue, 1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

			reclaimee := CreatePod(ctx, testCtx, reclaimeeQueue, 3)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimee)

			reclaimer := CreatePod(ctx, testCtx, reclaimerQueue, 1)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, reclaimer)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimer)
		})
	})
})
