/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"
	"fmt"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	v2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

// DescribePreemptSemiPreemptibleSpecs proves the two-step semi-preemptible lifecycle: a job first expands
// beyond its minimum (elastic burst, over-quota) when capacity is free, then scales down to exactly its
// core when a higher-priority job needs the elastic capacity.
func DescribePreemptSemiPreemptibleSpecs() bool {
	return Describe("Semi-Preemptible Elastic Lifecycle", Ordered, func() {
		var (
			testCtx                 *testcontext.TestContext
			lowPriority             string
			highPreemptiblePriority string
			nonPreemptiblePriority  string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Cpu:      resource.MustParse("600m"),
					PodCount: 8,
				},
			})

			// Fixed values rather than rd.CreatePreemptiblePriorityClass: it picks at random from the
			// preemptible range, so two calls can collide or invert, and this spec needs low < high.
			lowPriority = createPriorityClass(ctx, testCtx, 10)
			highPreemptiblePriority = createPriorityClass(ctx, testCtx, 50)

			var err error
			nonPreemptiblePriority, err = rd.CreateNonPreemptiblePriorityClass(ctx, testCtx.KubeClientset)
			Expect(err).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
			Expect(err).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("subgroup shape: bursts over-quota then scales down to core subgroups on higher-priority arrival", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			// The parent is capped at the burst size so the elastic tier is the only place the
			// higher-priority job can get capacity from. Without the cap the cluster has spare CPU and
			// the second job simply allocates, so nothing is ever reclaimed.
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			parentQueue.Spec.Resources.CPU.Quota = 400
			parentQueue.Spec.Resources.CPU.Limit = 400
			// low queue quota sized for the 2 core subgroups only (2 * 100m).
			lowQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			lowQueue.Spec.Resources.CPU.Quota = 200
			lowQueue.Spec.Resources.CPU.Limit = 400
			highQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			highQueue.Spec.Resources.CPU.Quota = 200
			highQueue.Spec.Resources.CPU.Limit = 400
			testCtx.InitQueues([]*v2.Queue{lowQueue, highQueue, parentQueue})

			lowNamespace := queue.GetConnectedNamespaceToQueue(lowQueue)
			cpuPerPod := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			// 4 fully-gang leaf subgroups, minSubGroup=2 → 2 core, 2 elastic.
			pg, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
				utils.GenerateRandomK8sName(10), lowQueue, ptr.To[int32](2),
				flatLeaves("sg", 4, 1), nil, v2alpha2.SemiPreemptible, cpuPerPod)

			// Step 1 — expands beyond its minimum: all 4 subgroups scheduled (elastic tier over-quota).
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, h.AllPods, 4)

			// Step 2 — a higher-priority job arrives and needs the elastic capacity.
			highPod := rd.CreatePodObject(highQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("200m"),
				},
			})
			highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

			// The semi-preemptible job scales down to exactly its 2 core subgroups; the core keeps running.
			// Asserting an exact count, not "at least 2" — the latter also passes when nothing is evicted.
			wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
				return len(runningPodNames(ctx, testCtx, lowNamespace, pg.Name)) == 2
			})
		})

		// A subgroup that can never form its own gang must not hold a core slot, must not stop the rest
		// of the job from running, and must be the first thing given back under contention. minSubGroup
		// makes the group whole without it.
		It("broken gang: cores the subgroups that can form, and reclaims the orphan first", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			// The parent is capped at the burst size so the elastic tier is the only place the second
			// job can get capacity from.
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			parentQueue.Spec.Resources.CPU.Quota = 500
			parentQueue.Spec.Resources.CPU.Limit = 500
			// Quota covers the 4 core pods; the limit leaves room for the orphan to burst into.
			testQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testQueue.Spec.Resources.CPU.Quota = 400
			testQueue.Spec.Resources.CPU.Limit = 500
			otherQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			otherQueue.Spec.Resources.CPU.Quota = 100
			otherQueue.Spec.Resources.CPU.Limit = 100
			testCtx.InitQueues([]*v2.Queue{testQueue, otherQueue, parentQueue})

			namespace := queue.GetConnectedNamespaceToQueue(testQueue)
			cpuPerPod := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			// sg-0 and sg-1 have their full gang; sg-2 is one pod short of its minMember and never will
			// be. minSubGroup 2 of 3 means the job is satisfied on the first two alone.
			pg, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
				utils.GenerateRandomK8sName(10), testQueue, ptr.To[int32](2), []pod_group.SubGroupNode{
					{Name: "sg-0", MinMember: ptr.To(int32(2)), PodCount: 2},
					{Name: "sg-1", MinMember: ptr.To(int32(2)), PodCount: 2},
					{Name: "sg-2", MinMember: ptr.To(int32(2)), PodCount: 1},
				}, nil, v2alpha2.SemiPreemptible, cpuPerPod)

			pgClient := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace)

			// All 5 pods land: the orphan bursts into the elastic tier like any other surplus pod.
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, h.AllPods, 5)

			var formedPodNames []string
			for _, leaf := range []string{"sg-0", "sg-1"} {
				for _, pod := range h.Pods[leaf] {
					formedPodNames = append(formedPodNames, pod.Name)
				}
			}

			// The core is exactly the two formed subgroups, and only they are charged to quota - the
			// broken subgroup contributes neither a core slot nor non-preemptible resources.
			Eventually(func(g Gomega) {
				updated, err := pgClient.Get(ctx, pg.Name, metav1.GetOptions{})
				g.Expect(err).To(Succeed())
				g.Expect(updated.Status.SchedulingState).NotTo(BeNil())
				g.Expect(updated.Status.SchedulingState.CorePods).To(ConsistOf(formedPodNames))
				g.Expect(updated.Status.ResourcesStatus.AllocatedNonPreemptible.Cpu().MilliValue()).
					To(Equal(int64(400)))
			}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())

			// A job in a sibling queue now needs capacity the parent no longer has. The orphan is the
			// only pod that delivers nothing - no gang of its own, no core slot - so it goes first and
			// the four core pods keep running.
			otherPod := rd.CreatePodObject(otherQueue, cpuPerPod)
			otherPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, otherPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, otherPod)

			Eventually(func() []string {
				return runningPodNames(ctx, testCtx, namespace, pg.Name)
			}).WithTimeout(time.Minute).WithPolling(time.Second).Should(ConsistOf(formedPodNames))
		})

		It("pod shape: bursts over-quota then scales down to core pods on higher-priority arrival", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			// Parent capped at the burst size — see the subgroup-shape spec above.
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			parentQueue.Spec.Resources.CPU.Quota = 400
			parentQueue.Spec.Resources.CPU.Limit = 400
			lowQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			lowQueue.Spec.Resources.CPU.Quota = 200 // sized for 2 core pods
			lowQueue.Spec.Resources.CPU.Limit = 400
			highQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			highQueue.Spec.Resources.CPU.Quota = 200
			highQueue.Spec.Resources.CPU.Limit = 400
			testCtx.InitQueues([]*v2.Queue{lowQueue, highQueue, parentQueue})

			lowNamespace := queue.GetConnectedNamespaceToQueue(lowQueue)
			cpuPerPod := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			// Single elastic PodSet: minMember=2 (core), 4 pods total (2 elastic).
			podGroupName, pods := createSemiPreemptiblePodGroup(ctx, testCtx, lowQueue, ptr.To[int32](2), 4, "", cpuPerPod)

			// Step 1 — bursts to all 4 pods (elastic over-quota).
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, pods, 4)

			// Accounting: with 4 pods running, only the 2 core pods count as non-preemptible. The
			// scheduler publishes the core set and the podgroupcontroller sums exactly those pods.
			Eventually(func(g Gomega) {
				pg, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(lowNamespace).
					Get(ctx, podGroupName, metav1.GetOptions{})
				g.Expect(err).To(Succeed())
				g.Expect(pg.Status.SchedulingState).NotTo(BeNil())
				g.Expect(pg.Status.SchedulingState.CorePods).To(HaveLen(2))
				g.Expect(pg.Status.ResourcesStatus.Allocated.Cpu().MilliValue()).To(Equal(int64(400)))
				g.Expect(pg.Status.ResourcesStatus.AllocatedNonPreemptible.Cpu().MilliValue()).To(Equal(int64(200)))

				updatedQueue := &v2.Queue{}
				g.Expect(testCtx.ControllerClient.Get(
					ctx, client.ObjectKey{Name: lowQueue.Name}, updatedQueue)).To(Succeed())
				g.Expect(updatedQueue.Status.AllocatedNonPreemptible.Cpu().MilliValue()).To(Equal(int64(200)))
			}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())

			// Step 2 — higher-priority job needs the elastic capacity.
			highPod := rd.CreatePodObject(highQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("200m"),
				},
			})
			highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

			// Scales down to exactly its 2 core pods; the core keeps running.
			wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
				return len(runningPodNames(ctx, testCtx, lowNamespace, podGroupName)) == 2
			})
		})

		// The scenario the feature exists for: a low-priority semi-preemptible job bursts past its
		// minimum, then gives the surplus back to a higher-priority job in the same queue. Unlike the
		// specs above (cross-queue reclaim), this drives the scale-down through preempt, which is what
		// exercises the semi-preemptible victim eligibility in preempt.go / input_jobs.go.
		for _, preemptor := range []struct {
			kind     string
			priority func() string
		}{
			{"preemptible", func() string { return highPreemptiblePriority }},
			{"non-preemptible", func() string { return nonPreemptiblePriority }},
		} {
			It("scales down to its core when preempted by a higher-priority "+preemptor.kind+" job",
				func(ctx context.Context) {
					testCtx = testcontext.GetConnectivity(ctx, Default)
					parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
					testQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
					// Limit equals the burst size, so once the job has expanded the preemptor can only
					// fit by evicting from it — mirroring preempt_elastic_specs, where quota == limit
					// == the whole cluster GPU count. Quota covers the victim's core plus the preemptor:
					// a non-preemptible preemptor must fit in quota, and the core holds half of it
					// permanently, so a smaller quota makes that variant unschedulable by design.
					testQueue.Spec.Resources.CPU.Quota = 400
					testQueue.Spec.Resources.CPU.Limit = 400
					testCtx.InitQueues([]*v2.Queue{testQueue, parentQueue})

					namespace := queue.GetConnectedNamespaceToQueue(testQueue)
					cpuPerPod := v1.ResourceRequirements{
						Limits: map[v1.ResourceName]resource.Quantity{
							v1.ResourceCPU: resource.MustParse("100m"),
						},
					}

					// minMember=2 core, 4 pods total. The explicit preemptibility wins over the priority
					// fallback, so lowPriority is purely an ordering signal here.
					podGroupName, pods := createSemiPreemptiblePodGroup(
						ctx, testCtx, testQueue, ptr.To[int32](2), 4, lowPriority, cpuPerPod)

					// Bursts past its minimum onto the free capacity.
					wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods, 4)

					preemptorPod := rd.CreatePodObject(testQueue, v1.ResourceRequirements{
						Limits: map[v1.ResourceName]resource.Quantity{
							v1.ResourceCPU: resource.MustParse("200m"),
						},
					})
					preemptorPod.Labels[PriorityClassNameLabelName] = preemptor.priority()
					preemptorPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, preemptorPod)
					Expect(err).To(Succeed())
					wait.ForPodScheduled(ctx, testCtx.ControllerClient, preemptorPod)

					wait.ForPodsWithCondition(ctx, testCtx.ControllerClient, func(watch.Event) bool {
						return len(runningPodNames(ctx, testCtx, namespace, podGroupName)) == 2
					})

					// The survivors are exactly the set the scheduler published as core — the end-to-end
					// proof that the published set is the set eviction actually protected.
					Eventually(func(g Gomega) {
						pg, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).
							Get(ctx, podGroupName, metav1.GetOptions{})
						g.Expect(err).To(Succeed())
						g.Expect(pg.Status.SchedulingState).NotTo(BeNil())
						g.Expect(runningPodNames(ctx, testCtx, namespace, podGroupName)).
							To(Equal(pg.Status.SchedulingState.CorePods))
					}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())
				})
		}
	})
}

// createSemiPreemptiblePodGroup creates a single-PodSet semi-preemptible PodGroup with the given core
// minMember and total pod count, returning its name and the created pods. An empty priorityClassName
// leaves the pods at the default priority.
func createSemiPreemptiblePodGroup(
	ctx context.Context, testCtx *testcontext.TestContext, q *v2.Queue, minMember *int32, numPods int,
	priorityClassName string, requirements v1.ResourceRequirements,
) (string, []*v1.Pod) {
	namespace := queue.GetConnectedNamespaceToQueue(q)
	podGroupName := utils.GenerateRandomK8sName(10)

	podGroup := pod_group.Create(namespace, podGroupName, q.Name)
	podGroup.Spec.MinMember = minMember
	podGroup.Spec.Preemptibility = v2alpha2.SemiPreemptible
	// Job priority comes from the PodGroup, not the pod labels - without this the group falls back to
	// the default priority and never ranks below a preemptor that uses the same default.
	podGroup.Spec.PriorityClassName = priorityClassName
	_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx, podGroup, metav1.CreateOptions{})
	Expect(err).To(Succeed())

	var pods []*v1.Pod
	for i := 0; i < numPods; i++ {
		pod := rd.CreatePodWithPodGroupReference(q, podGroupName, requirements)
		if priorityClassName != "" {
			pod.Labels[PriorityClassNameLabelName] = priorityClassName
		}
		pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).To(Succeed())
		pods = append(pods, pod)
	}
	return podGroupName, pods
}

// createPriorityClass creates an e2e-labelled PriorityClass with an explicit value, so relative
// ordering between classes in a spec is deterministic.
func createPriorityClass(ctx context.Context, testCtx *testcontext.TestContext, value int) string {
	name := utils.GenerateRandomK8sName(10)
	_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().
		Create(ctx, rd.CreatePriorityClass(name, value), metav1.CreateOptions{})
	Expect(err).To(Succeed())
	return name
}

// runningPodNames lists the names of the pods still present for a PodGroup. Evicted pods are deleted,
// so this is the post-preemption survivor set.
func runningPodNames(
	ctx context.Context, testCtx *testcontext.TestContext, namespace, podGroupName string,
) []string {
	pods, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", rd.PodGroupLabelName, podGroupName),
	})
	Expect(err).To(Succeed())

	names := make([]string, 0, len(pods.Items))
	for _, pod := range pods.Items {
		names = append(names, pod.Name)
	}
	sort.Strings(names)
	return names
}
