// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fillers

import (
	"context"
	"maps"
	"slices"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

// VictimTerminationGracePeriod is how long the pods of FillGPUNodesWithSlowTerminatingPods keep
// terminating once evicted.
const VictimTerminationGracePeriod = 30 * time.Second

// FillGPUNodesWithSlowTerminatingPods creates one pod of q requesting gpus GPUs, with labels and
// priorityClass, on each node with that many idle GPUs. Each pod avoids the nodes of the earlier
// ones but is not pinned to its own, so consolidation can move it. Once deleted, a pod keeps
// terminating for VictimTerminationGracePeriod, as its container ignores SIGTERM. It returns the
// pods by node.
func FillGPUNodesWithSlowTerminatingPods(ctx context.Context, testCtx *testcontext.TestContext, q *v2.Queue,
	labels map[string]string, priorityClass string, gpus int64) map[string]*v1.Pod {
	nodesIdleResources, err := capacity.GetNodesIdleResources(testCtx.KubeClientset)
	Expect(err).To(Succeed())
	numPods := 0
	for _, idle := range nodesIdleResources {
		if idle.Gpu.Value() >= gpus {
			numPods++
		}
	}

	pods := map[string]*v1.Pod{}
	for range numPods {
		pod := rd.CreatePodObject(q, GPURequirements(gpus))
		maps.Copy(pod.Labels, labels)
		pod.Spec.PriorityClassName = priorityClass
		pod.Spec.TerminationGracePeriodSeconds = ptr.To(int64(VictimTerminationGracePeriod.Seconds()))
		if len(pods) > 0 {
			pod.Spec.Affinity = &v1.Affinity{NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{{
						Key:      constant.NodeNamePodLabelName,
						Operator: v1.NodeSelectorOpNotIn,
						Values:   slices.Sorted(maps.Keys(pods)),
					}}}},
				},
			}}
		}
		pod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).To(Succeed())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), pod)).To(Succeed())
		pods[pod.Spec.NodeName] = pod
	}
	return pods
}

// CreatePodRepellingVictims creates pod with the victims' labels and a required anti-affinity against
// the pods in namespaces that have them, so it needs a node without victims or other such pods.
func CreatePodRepellingVictims(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod,
	victimLabels map[string]string, namespaces ...string) *v1.Pod {
	maps.Copy(pod.Labels, victimLabels)
	pod.Spec.Affinity = rd.PodAntiAffinity(victimLabels, namespaces...)
	pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
	Expect(err).To(Succeed())
	return pod
}

// EvictedNodes returns a poll function listing, sorted, the nodes whose pod in victims is being
// deleted or is gone.
func EvictedNodes(ctx context.Context, testCtx *testcontext.TestContext, victims map[string]*v1.Pod) func(Gomega) []string {
	return func(g Gomega) []string {
		nodes := []string{}
		for nodeName, victim := range victims {
			pod := &v1.Pod{}
			err := testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(victim), pod)
			if errors.IsNotFound(err) || (err == nil && pod.DeletionTimestamp != nil) {
				nodes = append(nodes, nodeName)
				continue
			}
			g.Expect(err).NotTo(HaveOccurred())
		}
		slices.Sort(nodes)
		return nodes
	}
}

// ExpectEvictionsUntilScheduled checks that exactly n of victims (by node) are evicted for pending,
// that while they terminate no other victim is evicted and no pod of pending is bound beside a
// victim still on its node, and that pending then runs on the evicted victims' nodes.
func ExpectEvictionsUntilScheduled(ctx context.Context, testCtx *testcontext.TestContext,
	victims map[string]*v1.Pod, n int, pending ...*v1.Pod) {
	evictedNodes := EvictedNodes(ctx, testCtx, victims)
	Eventually(evictedNodes).WithTimeout(time.Minute).WithPolling(time.Second).
		Should(WithTransform(func(nodes []string) int { return len(nodes) }, BeNumerically(">=", n)))
	evicted := evictedNodes(Default)
	Expect(evicted).To(HaveLen(n), "only %d victims are needed", n)

	Consistently(func(g Gomega) {
		g.Expect(evictedNodes(g)).To(Equal(evicted), "no other victim is evicted while the first ones terminate")
		for _, pod := range pending {
			current := &v1.Pod{}
			g.Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), current)).To(Succeed())
			if victim, found := victims[current.Spec.NodeName]; found {
				err := testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(victim), &v1.Pod{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "pod %s is bound beside victim %s", pod.Name, victim.Name)
			}
		}
	}).WithTimeout(VictimTerminationGracePeriod / 3).WithPolling(time.Second).Should(Succeed())

	var nodes []string
	for _, pod := range pending {
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		scheduled := &v1.Pod{}
		Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), scheduled)).To(Succeed())
		nodes = append(nodes, scheduled.Spec.NodeName)
	}
	Expect(nodes).To(ConsistOf(evicted))
	Expect(evictedNodes(Default)).To(Equal(evicted))
}

// GPURequirements requests gpus whole GPUs.
func GPURequirements(gpus int64) v1.ResourceRequirements {
	return v1.ResourceRequirements{
		Limits: v1.ResourceList{constants.NvidiaGpuResource: *resource.NewQuantity(gpus, resource.DecimalSI)},
	}
}
