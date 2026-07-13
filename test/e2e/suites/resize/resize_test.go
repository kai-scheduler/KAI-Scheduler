/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resize

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func TestResize(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Deferred In-Place Resize Suite")
}

// NOTE (CI / Kind only): this suite requires Kubernetes 1.33+ with the InPlacePodVerticalScaling
// feature (GA in 1.35) and a schedulable node with spare CPU; it cannot run in the unit
// environment. It fills a node, requests an in-place CPU resize the kubelet defers, and asserts
// KAI frees room so the resize actuates when the queue is within quota, and does not when over
// quota. The absolute CPU sizes are derived from a node's idle capacity at runtime; on unusual
// nodes the fractions below may need tuning.
var _ = Describe("Deferred in-place resize", Ordered, func() {
	var (
		testCtx      *testcontext.TestContext
		lowPriority  string
		highPriority string
		hostNode     string
		unitMilli    int64 // one "unit" of CPU; victim=1 unit, important grows by 1 unit
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		skipIfInPlaceResizeUnavailable(ctx, testCtx)

		// Pick a node with at least 3 units of idle CPU so victim(1) + important(1) fill 2 units and
		// the resize (+1 unit) needs the victim's room. Skip if the cluster has no such node.
		hostNode, unitMilli = pickNodeWithIdleCPU(testCtx)
		if hostNode == "" {
			Skip("no schedulable node with enough idle CPU for the deferred-resize scenario")
		}

		lowPriority = utils.GenerateRandomK8sName(10)
		lowValue := utils.RandomIntBetween(0, constant.NonPreemptiblePriorityThreshold-2)
		_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(lowPriority, lowValue), metav1.CreateOptions{})
		Expect(err).To(Succeed())

		// Important workload is non-preemptible (>= threshold) so it is subject to the queue quota
		// gate, matching the fairness model the feature relies on.
		highPriority = utils.GenerateRandomK8sName(10)
		highValue := utils.RandomIntBetween(constant.NonPreemptiblePriorityThreshold, constant.NonPreemptiblePriorityThreshold*2)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(highPriority, highValue), metav1.CreateOptions{})
		Expect(err).To(Succeed())
	})

	AfterAll(func(ctx context.Context) {
		_ = rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
	})

	AfterEach(func(ctx context.Context) {
		wait.ForNoE2EPods(ctx, testCtx.ControllerClient)
	})

	// grownMilli is the important pod's post-resize size (2 units); it must be covered by the queue
	// quota for the resize to be allowed to evict.
	grownMilli := func() int64 { return 2 * unitMilli }

	It("frees room so the resize actuates when the queue is within quota", func(ctx context.Context) {
		q := makeQueue(testCtx, grownMilli()) // quota covers the grown important pod

		victim := placePod(ctx, testCtx, q, lowPriority, hostNode, unitMilli, false)
		important := placePod(ctx, testCtx, q, highPriority, hostNode, unitMilli, true)
		wait.ForPodReady(ctx, testCtx.ControllerClient, victim)
		wait.ForPodReady(ctx, testCtx.ControllerClient, important)

		// Node is full: resizing important up by one unit is deferred by the kubelet.
		resizeCPU(ctx, testCtx, important, grownMilli())

		// KAI represents the growth as a node-pinned demand and preempts the lower-priority victim;
		// the kubelet then actuates the resize.
		Eventually(func(g Gomega) {
			g.Expect(podGone(ctx, testCtx, victim)).To(BeTrue(), "victim should be evicted to free room")
			g.Expect(actualCPUMilli(ctx, testCtx, important)).To(BeNumerically(">=", grownMilli()),
				"resize should actuate to the grown size once room is freed")
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	})

	It("leaves the resize deferred when the queue is over its quota", func(ctx context.Context) {
		q := makeQueue(testCtx, unitMilli) // quota covers only the pre-resize size, not the growth

		victim := placePod(ctx, testCtx, q, lowPriority, hostNode, unitMilli, false)
		important := placePod(ctx, testCtx, q, highPriority, hostNode, unitMilli, true)
		wait.ForPodReady(ctx, testCtx.ControllerClient, victim)
		wait.ForPodReady(ctx, testCtx.ControllerClient, important)

		resizeCPU(ctx, testCtx, important, grownMilli())

		// Over quota: KAI must not evict the victim, and the resize stays deferred.
		Consistently(func(g Gomega) {
			g.Expect(podGone(ctx, testCtx, victim)).To(BeFalse(), "over-quota resize must not evict the victim")
			g.Expect(actualCPUMilli(ctx, testCtx, important)).To(BeNumerically("<", grownMilli()),
				"over-quota resize must stay deferred")
		}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
	})
})

// makeQueue creates a parent+leaf queue whose leaf CPU quota+limit is quotaMilli (converted to the
// cores unit the Queue API uses).
func makeQueue(testCtx *testcontext.TestContext, quotaMilli int64) *v2.Queue {
	parent := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
	leaf := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parent.Name)
	cores := float64(quotaMilli) / 1000.0
	leaf.Spec.Resources.CPU.Quota = cores
	leaf.Spec.Resources.CPU.Limit = -1
	parent.Spec.Resources.CPU.Quota = cores
	parent.Spec.Resources.CPU.Limit = -1
	testCtx.InitQueues([]*v2.Queue{leaf, parent})
	return leaf
}

// placePod creates a pod pinned to hostNode requesting cpuMilli; important pods carry a NotRequired
// cpu resize policy so the kubelet can grow them in place.
func placePod(ctx context.Context, testCtx *testcontext.TestContext, q *v2.Queue, priorityClass, hostNode string,
	cpuMilli int64, important bool) *v1.Pod {
	req := v1.ResourceList{v1.ResourceCPU: *resource.NewMilliQuantity(cpuMilli, resource.DecimalSI)}
	pod := rd.CreatePodObject(q, v1.ResourceRequirements{Requests: req, Limits: req})
	pod.Spec.PriorityClassName = priorityClass
	pod.Spec.Affinity = &v1.Affinity{NodeAffinity: &v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{{
				Key: v1.LabelHostname, Operator: v1.NodeSelectorOpIn, Values: []string{hostNode},
			}}}},
		},
	}}
	if important {
		pod.Spec.Containers[0].ResizePolicy = []v1.ContainerResizePolicy{{
			ResourceName: v1.ResourceCPU, RestartPolicy: v1.NotRequired,
		}}
	}
	created, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
	Expect(err).To(Succeed())
	return created
}

// resizeCPU patches the pod's cpu request+limit via the resize subresource.
func resizeCPU(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod, cpuMilli int64) {
	cpu := resource.NewMilliQuantity(cpuMilli, resource.DecimalSI).String()
	patch := fmt.Sprintf(
		`{"spec":{"containers":[{"name":%q,"resources":{"requests":{"cpu":%q},"limits":{"cpu":%q}}}]}}`,
		pod.Spec.Containers[0].Name, cpu, cpu)
	_, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).
		Patch(ctx, pod.Name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}, "resize")
	Expect(err).To(Succeed())
}

func actualCPUMilli(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) int64 {
	got, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return 0
	}
	for _, cs := range got.Status.ContainerStatuses {
		if cs.Resources != nil {
			if q, ok := cs.Resources.Requests[v1.ResourceCPU]; ok {
				return q.MilliValue()
			}
		}
	}
	return 0
}

func podGone(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) bool {
	got, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	return err != nil || got.DeletionTimestamp != nil
}

// pickNodeWithIdleCPU returns a node with >= 3 units of idle CPU and the unit size in milli-cores
// (one third of that node's idle), or "" if none qualifies.
func pickNodeWithIdleCPU(testCtx *testcontext.TestContext) (string, int64) {
	idle, err := capacity.GetNodesIdleResources(testCtx.KubeClientset)
	if err != nil {
		return "", 0
	}
	for node, rl := range idle {
		if rl == nil {
			continue
		}
		milli := rl.Cpu.MilliValue()
		if milli >= 3000 {
			return node, milli / 3
		}
	}
	return "", 0
}

// skipIfInPlaceResizeUnavailable skips the suite when the API server predates the resize
// subresource / InPlacePodVerticalScaling (Kubernetes < 1.33).
func skipIfInPlaceResizeUnavailable(ctx context.Context, testCtx *testcontext.TestContext) {
	ver, err := testCtx.KubeClientset.Discovery().ServerVersion()
	if err != nil {
		Skip("could not determine server version for in-place resize support: " + err.Error())
	}
	minor, err := strconv.Atoi(strings.TrimRight(ver.Minor, "+"))
	if err != nil || minor < 33 {
		Skip("in-place pod vertical scaling requires Kubernetes 1.33+, found " + ver.String())
	}
}
