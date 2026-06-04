/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package podgroup

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kubeAiSchedClient "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned"
	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func createMinSubGroupPodGroup(testCtx *testcontext.TestContext, minSubGroup *int32, minMember int32,
	subGroups []schedulingv2alpha2.SubGroup) *schedulingv2alpha2.PodGroup {
	namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
	pg := pod_group.Create(namespace, utils.GenerateRandomK8sName(10), testCtx.Queues[0].Name)
	if minMember != 0 {
		pg.Spec.MinMember = ptr.To(minMember)
	} else {
		pg.Spec.MinMember = nil
	}
	pg.Spec.MinSubGroup = minSubGroup
	pg.Spec.SubGroups = subGroups
	return pg
}

var _ = Describe("MinSubGroup validation", Ordered, func() {
	var testCtx *testcontext.TestContext

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

	It("should reject PodGroup with both minMember and minSubGroup set", func(ctx context.Context) {
		pg := createMinSubGroupPodGroup(testCtx, ptr.To[int32](3), 24,
			[]schedulingv2alpha2.SubGroup{
				{Name: "sub-0", MinMember: ptr.To[int32](8)},
				{Name: "sub-1", MinMember: ptr.To[int32](8)},
				{Name: "sub-2", MinMember: ptr.To[int32](8)},
			})

		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(pg.Namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).ToNot(Succeed())
	})

	It("should reject minSubGroup on leaf SubGroup with no children", func(ctx context.Context) {
		pg := createMinSubGroupPodGroup(testCtx, ptr.To[int32](3), 0,
			[]schedulingv2alpha2.SubGroup{
				{Name: "prefill-0", MinSubGroup: ptr.To[int32](2)},
				{Name: "prefill-1", MinMember: ptr.To[int32](8)},
				{Name: "prefill-2", MinMember: ptr.To[int32](8)},
			})

		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(pg.Namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).ToNot(Succeed())
	})

	It("should reject minSubGroup exceeding child count", func(ctx context.Context) {
		pg := createMinSubGroupPodGroup(testCtx, ptr.To[int32](5), 0,
			[]schedulingv2alpha2.SubGroup{
				{Name: "prefill-0", MinMember: ptr.To[int32](8)},
				{Name: "prefill-1", MinMember: ptr.To[int32](8)},
				{Name: "prefill-2", MinMember: ptr.To[int32](8)},
				{Name: "prefill-3", MinMember: ptr.To[int32](8)},
			})
		warningCapture := &utils.ClientGoWarningHandler{}
		cfg := *testCtx.KubeConfig
		cfg.WarningHandlerWithContext = warningCapture
		cs, err := kubeAiSchedClient.NewForConfig(&cfg)

		_, err = cs.SchedulingV2alpha2().PodGroups(pg.Namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).To(Succeed()) // We expect a warning in this case, not an error
		Expect(warningCapture.Messages).To(HaveLen(1))
		Expect(warningCapture.Messages[0]).To(ContainSubstring("minSubGroup (5) exceeds the number of direct child SubGroups (4)"))
	})

	It("should reject minSubGroup = 0", func(ctx context.Context) {
		pg := createMinSubGroupPodGroup(testCtx, ptr.To[int32](0), 0,
			[]schedulingv2alpha2.SubGroup{
				{Name: "prefill-0", MinMember: ptr.To[int32](8)},
				{Name: "prefill-1", MinMember: ptr.To[int32](8)},
			})

		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(pg.Namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).ToNot(Succeed())
	})

	It("should reject SubGroup with both minMember and minSubGroup", func(ctx context.Context) {
		parent := "decode"
		pg := createMinSubGroupPodGroup(testCtx, ptr.To[int32](1), 0,
			[]schedulingv2alpha2.SubGroup{
				{Name: "decode", MinMember: ptr.To[int32](5), MinSubGroup: ptr.To[int32](2)},
				{Name: "decode-leaders", Parent: &parent, MinMember: ptr.To[int32](1)},
				{Name: "decode-workers", Parent: &parent, MinMember: ptr.To[int32](4)},
			})

		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(pg.Namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).ToNot(Succeed())
	})

	It("should reject mid-level SubGroup using minMember instead of minSubGroup", func(ctx context.Context) {
		parent := "decode"
		pg := createMinSubGroupPodGroup(testCtx, ptr.To[int32](1), 0,
			[]schedulingv2alpha2.SubGroup{
				{Name: "decode", MinMember: ptr.To[int32](5)},
				{Name: "decode-leaders", Parent: &parent, MinMember: ptr.To[int32](1)},
				{Name: "decode-workers", Parent: &parent, MinMember: ptr.To[int32](4)},
			})

		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(pg.Namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).ToNot(Succeed())
	})

	It("should accept valid minSubGroup configuration", func(ctx context.Context) {
		pg := createMinSubGroupPodGroup(testCtx, ptr.To[int32](3), 0,
			[]schedulingv2alpha2.SubGroup{
				{Name: "prefill-0", MinMember: ptr.To[int32](8)},
				{Name: "prefill-1", MinMember: ptr.To[int32](8)},
				{Name: "prefill-2", MinMember: ptr.To[int32](8)},
				{Name: "prefill-3", MinMember: ptr.To[int32](8)},
			})

		_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(pg.Namespace).Create(ctx,
			pg, metav1.CreateOptions{})
		Expect(err).To(Succeed())
	})
})
