/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resize

import (
	"context"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	resizeTimeout = 2 * time.Minute
	resizePoll    = 2 * time.Second
)

func DescribeResizeSpecs() bool {
	return Describe("In-place pod resize", Ordered, func() {
		var (
			testCtx        *testcontext.TestContext
			boundedQueue   *v2.Queue
			unboundedQueue *v2.Queue
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			skipIfNoResizeSupport(testCtx)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Cpu:      resource.MustParse("2"),
					PodCount: 1,
				},
			)

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			boundedQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			boundedQueue.Spec.Resources.CPU = v2.QueueResource{Quota: 500, OverQuotaWeight: 1, Limit: 2000}
			unboundedQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{boundedQueue, unboundedQueue, parentQueue})

			Expect(feature_flags.SetInPlacePodResizeValidation(ctx, testCtx, ptr.To(true), nil)).
				To(Succeed(), "failed to enable pod resize validation in kai config")
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		AfterAll(func(ctx context.Context) {
			Expect(feature_flags.SetInPlacePodResizeValidation(ctx, testCtx, nil, nil)).
				To(Succeed(), "failed to restore pod resize validation defaults")
			testCtx.ClusterCleanup(ctx)
		})

		It("rejects an upsize that exceeds the queue CPU limit", func(ctx context.Context) {
			pod := runPodWithCPU(ctx, testCtx, boundedQueue, "500m")

			err := resizeCPU(ctx, testCtx, pod, "3")
			Expect(err).To(HaveOccurred(), "upsize past the queue limit should be denied")
			Expect(err.Error()).To(ContainSubstring("resize rejected"))
		})

		It("allows upsizes within the queue limit and downsizes", func(ctx context.Context) {
			pod := runPodWithCPU(ctx, testCtx, boundedQueue, "500m")

			By("upsizing within the limit")
			Expect(resizeCPU(ctx, testCtx, pod, "1")).To(Succeed())
			expectEnactedCPU(ctx, testCtx, pod, "1")

			By("downsizing")
			Expect(resizeCPU(ctx, testCtx, pod, "250m")).To(Succeed())
			expectEnactedCPU(ctx, testCtx, pod, "250m")
		})

		It("does not charge infeasible resize targets to the queue", func(ctx context.Context) {
			pod := runPodWithCPU(ctx, testCtx, unboundedQueue, "500m")

			By("requesting a resize no node can satisfy")
			Expect(resizeCPU(ctx, testCtx, pod, "1000")).To(Succeed(),
				"unbounded queue must not block the resize; feasibility is the kubelet's call")

			By("waiting for the kubelet to mark the resize Infeasible")
			Eventually(func(g Gomega) {
				current := &v1.Pod{}
				g.Expect(testCtx.ControllerClient.Get(ctx,
					types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, current)).To(Succeed())
				g.Expect(isResizeInfeasible(current)).To(BeTrue())
			}).WithContext(ctx).WithTimeout(resizeTimeout).WithPolling(resizePoll).Should(Succeed())

			By("verifying the queue keeps charging the enacted request, not the infeasible spec")
			Eventually(func(g Gomega) int64 {
				q, err := testCtx.KubeAiSchedClientset.SchedulingV2().Queues("").
					Get(ctx, unboundedQueue.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				allocated := q.Status.Allocated[v1.ResourceCPU]
				return allocated.MilliValue()
			}).WithContext(ctx).WithTimeout(resizeTimeout).WithPolling(resizePoll).
				Should(Equal(int64(500)), "queue allocated must reflect the 500m enacted request")
		})
	})
}

// runPodWithCPU creates a pod in the given queue with the given CPU request and
// waits for it to be running, so a ContainerStatus with enacted resources exists.
func runPodWithCPU(ctx context.Context, testCtx *testcontext.TestContext, podQueue *v2.Queue, cpu string) *v1.Pod {
	pod := rd.CreatePodObject(podQueue, v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse(cpu),
			v1.ResourceMemory: resource.MustParse("256Mi"),
		},
	})
	pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
	Expect(err).NotTo(HaveOccurred(), "failed to create pod")
	wait.ForPodReady(ctx, testCtx.ControllerClient, pod)
	return pod
}

// resizeCPU updates the pod's CPU request through the pods/resize subresource.
// Retries on conflict: the kubelet concurrently writes pod status during a
// resize, so a Get-mutate-Update can race with it.
func resizeCPU(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod, cpu string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &v1.Pod{}
		if err := testCtx.ControllerClient.Get(ctx,
			types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, current); err != nil {
			return err
		}
		current.Spec.Containers[0].Resources.Requests[v1.ResourceCPU] = resource.MustParse(cpu)
		return testCtx.ControllerClient.SubResource("resize").Update(ctx, current)
	})
}

func expectEnactedCPU(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod, cpu string) {
	want := resource.MustParse(cpu)
	Eventually(func(g Gomega) {
		current := &v1.Pod{}
		g.Expect(testCtx.ControllerClient.Get(ctx,
			types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, current)).To(Succeed())
		g.Expect(current.Status.ContainerStatuses).NotTo(BeEmpty())
		g.Expect(current.Status.ContainerStatuses[0].Resources).NotTo(BeNil())
		got := current.Status.ContainerStatuses[0].Resources.Requests[v1.ResourceCPU]
		g.Expect(got.Cmp(want)).To(BeZero(), "enacted cpu: want %s got %s", want.String(), got.String())
	}).WithContext(ctx).WithTimeout(resizeTimeout).WithPolling(resizePoll).Should(Succeed())
}

func isResizeInfeasible(pod *v1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == v1.PodResizePending {
			return c.Status == v1.ConditionTrue && c.Reason == v1.PodReasonInfeasible
		}
	}
	return false
}

// skipIfNoResizeSupport skips the suite on clusters older than 1.33, where
// InPlacePodVerticalScaling is not enabled by default.
func skipIfNoResizeSupport(testCtx *testcontext.TestContext) {
	ver, err := testCtx.KubeClientset.Discovery().ServerVersion()
	Expect(err).NotTo(HaveOccurred(), "failed to read server version")
	minor, err := strconv.Atoi(strings.TrimSuffix(ver.Minor, "+"))
	Expect(err).NotTo(HaveOccurred(), "failed to parse server minor version %q", ver.Minor)
	if ver.Major != "1" || minor < 33 {
		Skip("in-place pod resize requires Kubernetes 1.33+ (InPlacePodVerticalScaling on by default)")
	}
}
