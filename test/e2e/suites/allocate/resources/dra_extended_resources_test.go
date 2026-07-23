/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resources

import (
	"context"
	"encoding/json"
	"fmt"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

const (
	draDeviceClassName = "gpu.nvidia.com"
	// extendedResourceName is the classic resource name advertised by the DeviceClass
	// via spec.extendedResourceName. Pods use this to request DRA-backed GPUs without
	// listing a ResourceClaim in spec.resourceClaims (KEP-5004).
	extendedResourceName = "nvidia.com/gpu"
	// draNodeLabel is set on kind workers that run the fake-gpu-operator DRA plugin.
	// Only these nodes expose GPUs via ResourceSlices; device-plugin nodes use
	// node.Status.Allocatable instead and must not be used for DRA extended resource tests.
	draNodeLabel = "nvidia.com/gpu.deploy.dra-plugin-gpu"
	// devicePluginNodeLabel is set on kind workers that run the fake-gpu-operator device plugin.
	// These nodes expose nvidia.com/gpu via node.Status.Allocatable, not via ResourceSlices.
	devicePluginNodeLabel = "nvidia.com/gpu.deploy.device-plugin"
)

var _ = Describe("Schedule pod with DRA-backed extended resource (KEP-5004)", Ordered, func() {
	var (
		testCtx   *testcontext.TestContext
		namespace string
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
		namespace = queue.GetConnectedNamespaceToQueue(childQueue)

		capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, draDeviceClassName, 1, 1)

		By("patching DeviceClass to set extendedResourceName")
		patch, _ := json.Marshal(map[string]any{
			"spec": map[string]any{
				"extendedResourceName": extendedResourceName,
			},
		})
		_, err := testCtx.KubeClientset.ResourceV1().DeviceClasses().Patch(
			ctx, draDeviceClassName, types.MergePatchType, patch, metav1.PatchOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to patch DeviceClass %s", draDeviceClassName)
	})

	AfterAll(func(ctx context.Context) {
		By("reverting DeviceClass extendedResourceName patch")
		patch, _ := json.Marshal(map[string]any{
			"spec": map[string]any{
				"extendedResourceName": nil,
			},
		})
		_, _ = testCtx.KubeClientset.ResourceV1().DeviceClasses().Patch(
			ctx, draDeviceClassName, types.MergePatchType, patch, metav1.PatchOptions{})

		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		By("delete all pods")
		Expect(rd.DeleteAllPodsInNamespace(ctx, testCtx.ControllerClient, namespace)).To(Succeed())
		wait.ForNoE2EPods(ctx, testCtx.ControllerClient)
		By("cleanup resource claims")
		capacity.CleanupResourceClaims(ctx, testCtx.KubeClientset, namespace)
	})

	It("schedules a pod using classic extended resource syntax without spec.resourceClaims", func(ctx context.Context) {
		draNodeName := firstDRANode(testCtx)

		pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceName(extendedResourceName): resource.MustParse("1"),
			},
			Limits: v1.ResourceList{
				v1.ResourceName(extendedResourceName): resource.MustParse("1"),
			},
		})
		pinPodToNode(pod, draNodeName)
		// No pod.Spec.ResourceClaims — this is the KEP-5004 extended resource path.

		_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).NotTo(HaveOccurred())

		wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

		By("verifying a synthetic ResourceClaim was created for the pod")
		Eventually(func() bool {
			return findExtendedResourceClaim(ctx, testCtx, namespace, pod.Name) != nil
		}, "30s", "1s").Should(BeTrue(), "expected a ResourceClaim with annotation %s to be created",
			resourceapi.ExtendedResourceClaimAnnotation)

		By("verifying pod.Status.ExtendedResourceClaimStatus is set")
		Eventually(func() *v1.PodExtendedResourceClaimStatus {
			updated := &v1.Pod{}
			_ = testCtx.ControllerClient.Get(ctx,
				types.NamespacedName{Namespace: namespace, Name: pod.Name}, updated)
			return updated.Status.ExtendedResourceClaimStatus
		}, "30s", "1s").ShouldNot(BeNil(), "pod status should record the synthetic ResourceClaim")
	})

	It("fills a DRA node with extended resource pods and blocks the next", func(ctx context.Context) {
		nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, draDeviceClassName)
		var draNodeName string
		deviceCount := 0
		for name, count := range nodesMap {
			if count >= 2 {
				draNodeName = name
				deviceCount = count
				break
			}
		}
		if draNodeName == "" {
			Skip("no DRA node with ≥ 2 devices found, skipping fill test")
		}

		By("scheduling one pod per device on the DRA node")
		var pods []*v1.Pod
		for range deviceCount {
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(extendedResourceName): resource.MustParse("1"),
				},
				Limits: v1.ResourceList{
					v1.ResourceName(extendedResourceName): resource.MustParse("1"),
				},
			})
			pinPodToNode(pod, draNodeName)
			pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			pods = append(pods, pod)
		}
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

		By("verifying a one-more pod is unschedulable")
		extra := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceName(extendedResourceName): resource.MustParse("1"),
			},
			Limits: v1.ResourceList{
				v1.ResourceName(extendedResourceName): resource.MustParse("1"),
			},
		})
		pinPodToNode(extra, draNodeName)
		_, err := rd.CreatePod(ctx, testCtx.KubeClientset, extra)
		Expect(err).NotTo(HaveOccurred())
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, extra)
	})

	It("schedules a gang job (minMember=2) using extended resource syntax across DRA nodes", func(ctx context.Context) {
		capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, draDeviceClassName, 2, 1)

		gpuReq := v1.ResourceRequirements{
			Requests: v1.ResourceList{v1.ResourceName(extendedResourceName): resource.MustParse("1")},
			Limits:   v1.ResourceList{v1.ResourceName(extendedResourceName): resource.MustParse("1")},
		}

		pgName := utils.GenerateRandomK8sName(10)
		pg := pod_group.Create(namespace, pgName, testCtx.Queues[0].Name)
		pg.Spec.MinMember = ptr.To(int32(2))
		pg, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx, pg, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Pods are constrained to DRA nodes so the scheduler must exercise the
		// extended resource → DRA path rather than falling back to device-plugin nodes.
		var pods []*v1.Pod
		for range 2 {
			pod := rd.CreatePodObject(testCtx.Queues[0], gpuReq)
			pod.Annotations[pod_group.PodGroupNameAnnotation] = pgName
			pod.Labels[pod_group.PodGroupNameAnnotation] = pgName
			requireDRANode(pod)
			pod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			pods = append(pods, pod)
		}

		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)
	})

	It("aggregates init-container GPU requests correctly (max, not sum)", func(ctx context.Context) {
		// PodRequests uses max(init, main) not sum. Pick the DRA node with the fewest
		// devices (N) and request N GPUs in both the init and main containers.
		// Correct aggregation: max(N, N) = N ≤ N available → schedules.
		// Incorrect sum:       N + N = 2N > N available → unschedulable.
		nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, draDeviceClassName)
		var targetNode string
		deviceCount := 0
		for name, count := range nodesMap {
			if targetNode == "" || count < deviceCount {
				targetNode = name
				deviceCount = count
			}
		}
		if targetNode == "" {
			Skip("no DRA nodes found; skipping init-container aggregation test")
		}

		gpuN := v1.ResourceList{
			v1.ResourceName(extendedResourceName): resource.MustParse(fmt.Sprintf("%d", deviceCount)),
		}
		pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Requests: gpuN,
			Limits:   gpuN,
		})
		pod.Spec.InitContainers = []v1.Container{{
			Name:            "init",
			Image:           pod.Spec.Containers[0].Image,
			Args:            []string{"true"},
			Resources:       v1.ResourceRequirements{Requests: gpuN, Limits: gpuN},
			SecurityContext: pod.Spec.Containers[0].SecurityContext,
			ImagePullPolicy: v1.PullIfNotPresent,
		}}
		pinPodToNode(pod, targetNode)

		_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).NotTo(HaveOccurred())

		wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
	})

	It("schedules a pod on a device-plugin node when the resource is also DRA-backed on other nodes", func(ctx context.Context) {
		dpNode := firstDevicePluginNodeWithGPU(testCtx)
		if dpNode == "" {
			Skip("no device-plugin node with " + extendedResourceName + " in allocatable found, skipping")
		}

		pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Requests: v1.ResourceList{v1.ResourceName(extendedResourceName): resource.MustParse("1")},
			Limits:   v1.ResourceList{v1.ResourceName(extendedResourceName): resource.MustParse("1")},
		})
		requireDevicePluginNode(pod)

		_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).NotTo(HaveOccurred())

		wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

		By("verifying no synthetic ResourceClaim was created for the pod")
		Consistently(func() bool {
			return findExtendedResourceClaim(ctx, testCtx, namespace, pod.Name) == nil
		}, "10s", "1s").Should(BeTrue(),
			"binder must not create a ResourceClaim for a pod bound via the device-plugin path")
	})
})

func firstDRANode(testCtx *testcontext.TestContext) string {
	nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, draDeviceClassName)
	for name := range nodesMap {
		return name
	}
	Fail("no DRA-capable node found")
	return ""
}

func requireDRANode(pod *v1.Pod) {
	pod.Spec.Affinity = &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{{
						Key:      draNodeLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"true"},
					}},
				}},
			},
		},
	}
}

func pinPodToNode(pod *v1.Pod, nodeName string) {
	pod.Spec.Affinity = &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{{
						Key:      v1.LabelHostname,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{nodeName},
					}},
				}},
			},
		},
	}
}

func requireDevicePluginNode(pod *v1.Pod) {
	pod.Spec.Affinity = &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{{
						Key:      devicePluginNodeLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"true"},
					}},
				}},
			},
		},
	}
}

func firstDevicePluginNodeWithGPU(testCtx *testcontext.TestContext) string {
	nodes, err := testCtx.KubeClientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", devicePluginNodeLabel),
	})
	if err != nil || len(nodes.Items) == 0 {
		return ""
	}
	for _, node := range nodes.Items {
		if qty, ok := node.Status.Allocatable[v1.ResourceName(extendedResourceName)]; ok && !qty.IsZero() {
			return node.Name
		}
	}
	return ""
}

func findExtendedResourceClaim(ctx context.Context, testCtx *testcontext.TestContext, namespace, podName string) *resourceapi.ResourceClaim {
	claims, err := testCtx.KubeClientset.ResourceV1().ResourceClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.Annotations[resourceapi.ExtendedResourceClaimAnnotation] != "true" {
			continue
		}
		for _, ref := range claim.OwnerReferences {
			if ref.Name == podName && ref.Controller != nil && *ref.Controller {
				return claim
			}
		}
	}
	return nil
}
