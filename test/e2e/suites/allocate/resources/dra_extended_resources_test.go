/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resources

import (
	"context"
	"encoding/json"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
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
)

const (
	// draDeviceClassName is the driver name used by the fake-gpu-operator DRA plugin.
	draDeviceClassName = "gpu.nvidia.com"
	// extendedResourceName is the classic resource name advertised by the DeviceClass
	// via spec.extendedResourceName. Pods use this to request DRA-backed GPUs without
	// listing a ResourceClaim in spec.resourceClaims (KEP-5004).
	extendedResourceName = "nvidia.com/gpu"
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

		// Require at least one DRA-capable node with ≥ 1 device.
		capacity.SkipIfInsufficientDynamicResources(testCtx.KubeClientset, draDeviceClassName, 1, 1)

		// Patch the DeviceClass to declare the extended resource name so that classic
		// `nvidia.com/gpu: N` pod requests are routed through DRA on DRA-only nodes.
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
		// Revert the DeviceClass patch.
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
})

// firstDRANode returns the name of any node that has DRA devices for draDeviceClassName.
func firstDRANode(testCtx *testcontext.TestContext) string {
	nodesMap := capacity.ListDevicesByNode(testCtx.KubeClientset, draDeviceClassName)
	for name := range nodesMap {
		return name
	}
	Fail("no DRA-capable node found")
	return ""
}

// pinPodToNode adds a required NodeAffinity so the pod runs only on nodeName.
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

// findExtendedResourceClaim returns the synthetic ResourceClaim created for podName
// (annotated with ExtendedResourceClaimAnnotation), or nil if none exists yet.
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
