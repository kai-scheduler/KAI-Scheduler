// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

var _ = Describe("Reclaim with pod anti-affinity", Label("nightly"), func() {
	It("evicts only one PodGroup while its pods terminate across scheduling cycles", func(ctx context.Context) {
		testCtx := testcontext.GetConnectivity(ctx, Default)
		DeferCleanup(func(ctx context.Context) { testCtx.ClusterCleanup(ctx) })

		nodes, err := testCtx.KubeClientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		Expect(err).To(Succeed())
		var gpuNodes []v1.Node
		for _, node := range nodes.Items {
			gpus := node.Status.Allocatable[constants.NvidiaGpuResource]
			if !node.Spec.Unschedulable && gpus.Value() > 0 {
				gpuNodes = append(gpuNodes, node)
			}
		}
		if len(gpuNodes) < 2 {
			Skip("requires two GPU nodes")
		}
		gpuNodes = gpuNodes[:2]
		totalGPUs := float64(0)
		for _, node := range gpuNodes {
			gpus := node.Status.Allocatable[constants.NvidiaGpuResource]
			totalGPUs += float64(gpus.Value())
		}
		parent, victimQueue, reclaimerQueue := CreateQueues(totalGPUs, 0, 1)
		testCtx.InitQueues([]*v2.Queue{parent, victimQueue, reclaimerQueue})
		victimNamespace := queue.GetConnectedNamespaceToQueue(victimQueue)
		victimLabel := "reclaim-affinity-victim"
		victims := make([]*v1.Pod, 0, 2)
		victimPodGroups := map[string]struct{}{}

		By("filling both eligible nodes with victims that stay alive during termination")
		for _, node := range gpuNodes {
			pod := rd.CreatePodObject(victimQueue, v1.ResourceRequirements{
				Limits: v1.ResourceList{constants.NvidiaGpuResource: node.Status.Allocatable[constants.NvidiaGpuResource]},
			})
			pod.Labels[victimLabel] = "true"
			pod.Spec.Affinity = rd.NodeAffinity(node.Name, v1.NodeSelectorOpIn)
			pod.Spec.TerminationGracePeriodSeconds = ptr.To(int64(120))
			pod.Spec.Containers[0].Lifecycle = &v1.Lifecycle{PreStop: &v1.LifecycleHandler{
				Exec: &v1.ExecAction{Command: []string{"sleep", "120"}},
			}}
			pod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).To(Succeed())
			victims = append(victims, pod)
			DeferCleanup(func(ctx context.Context) {
				err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name,
					metav1.DeleteOptions{GracePeriodSeconds: ptr.To(int64(0))})
				Expect(err == nil || errors.IsNotFound(err)).To(BeTrue(), "force-delete victim: %v", err)
			})
			wait.ForPodReady(ctx, testCtx.ControllerClient, pod)
			pod, err = rd.GetPod(ctx, testCtx.KubeClientset, pod.Namespace, pod.Name)
			Expect(err).To(Succeed())
			podGroupName := pod.Annotations[constants.PodGroupAnnotationForPod]
			Expect(podGroupName).NotTo(BeEmpty())
			wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, pod.Namespace, podGroupName)
			victimPodGroups[podGroupName] = struct{}{}
		}
		Expect(victimPodGroups).To(HaveLen(2), "victims must belong to distinct PodGroups")

		By("submitting an in-quota pod with required anti-affinity against both victims")
		reclaimer := rd.CreatePodObject(reclaimerQueue, v1.ResourceRequirements{
			Limits: v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("1")},
		})
		nodeSelector := &v1.NodeSelector{}
		for _, node := range gpuNodes {
			nodeSelector.NodeSelectorTerms = append(nodeSelector.NodeSelectorTerms, v1.NodeSelectorTerm{
				MatchFields: []v1.NodeSelectorRequirement{{
					Key: "metadata.name", Operator: v1.NodeSelectorOpIn, Values: []string{node.Name},
				}},
			})
		}
		reclaimer.Spec.Affinity = &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector},
			PodAntiAffinity: &v1.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{victimLabel: "true"}},
				Namespaces:    []string{victimNamespace}, TopologyKey: "kubernetes.io/hostname",
			}}},
		}
		reclaimer, err = rd.CreatePod(ctx, testCtx.KubeClientset, reclaimer)
		Expect(err).To(Succeed())

		var firstVictim *v1.Pod
		Eventually(func(g Gomega) {
			pods, err := testCtx.KubeClientset.CoreV1().Pods(victimNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: victimLabel + "=true",
			})
			g.Expect(err).To(Succeed())
			for _, pod := range pods.Items {
				if pod.DeletionTimestamp != nil {
					firstVictim = pod.DeepCopy()
					return
				}
			}
			g.Expect(firstVictim).NotTo(BeNil())
		}, time.Minute, time.Second).Should(Succeed())

		By("checking subsequent cycles evict exactly one PodGroup and keep the reclaimer unbound")
		Consistently(func(g Gomega) {
			evictedPodGroups := map[string]struct{}{}
			for _, victim := range victims {
				pod, err := rd.GetPod(ctx, testCtx.KubeClientset, victim.Namespace, victim.Name)
				g.Expect(err).To(Succeed())
				g.Expect(pod.UID).To(Equal(victim.UID))
				podGroupName := pod.Annotations[constants.PodGroupAnnotationForPod]
				g.Expect(victimPodGroups).To(HaveKey(podGroupName))
				if pod.DeletionTimestamp != nil {
					evictedPodGroups[podGroupName] = struct{}{}
				}
				if pod.UID == firstVictim.UID {
					g.Expect(pod.DeletionTimestamp).NotTo(BeNil())
				} else {
					g.Expect(pod.Status.Phase).To(Equal(v1.PodRunning))
				}
			}
			g.Expect(evictedPodGroups).To(HaveLen(1), "only one victim PodGroup may be evicted")
			g.Expect(evictedPodGroups).To(HaveKey(firstVictim.Annotations[constants.PodGroupAnnotationForPod]))
			pod, err := rd.GetPod(ctx, testCtx.KubeClientset, reclaimer.Namespace, reclaimer.Name)
			g.Expect(err).To(Succeed())
			g.Expect(pod.Spec.NodeName).To(BeEmpty())
		}, 20*time.Second, time.Second).Should(Succeed())

		By("finishing the first eviction and binding onto its freed node")
		Expect(testCtx.KubeClientset.CoreV1().Pods(firstVictim.Namespace).Delete(ctx, firstVictim.Name,
			metav1.DeleteOptions{GracePeriodSeconds: ptr.To(int64(0))})).To(Succeed())
		Eventually(func() bool {
			_, err := rd.GetPod(ctx, testCtx.KubeClientset, firstVictim.Namespace, firstVictim.Name)
			return errors.IsNotFound(err)
		}, time.Minute, time.Second).Should(BeTrue())
		wait.ForPodReady(ctx, testCtx.ControllerClient, reclaimer)
		reclaimer, err = rd.GetPod(ctx, testCtx.KubeClientset, reclaimer.Namespace, reclaimer.Name)
		Expect(err).To(Succeed())
		Expect(reclaimer.Spec.NodeName).To(Equal(firstVictim.Spec.NodeName))
		for _, victim := range victims {
			if victim.UID != firstVictim.UID {
				pod, err := rd.GetPod(ctx, testCtx.KubeClientset, victim.Namespace, victim.Name)
				Expect(err).To(Succeed())
				Expect(pod.DeletionTimestamp).To(BeNil())
			}
		}
	})
})
