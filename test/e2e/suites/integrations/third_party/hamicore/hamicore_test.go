/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package hamicore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kaiv1binder "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/binder"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/api/constants"
	kaiv2 "github.com/kai-scheduler/api/scheduling/v2"
)

const (
	kaiResourceIsolatorWebhookName = "kai-resource-isolator-mutating"
	binderDeploymentName           = "binder"
	binderDeploymentNamespace      = "kai-scheduler"
	binderPluginsFlag              = "--plugins"
	cudaImage                      = "nvidia/cuda:12.6.0-base-ubuntu22.04"
	gpuMemoryRequestMiB            = 2000
)

var _ = Describe("HAMi-core resource isolation", Ordered, func() {
	var testCtx *testcontext.TestContext

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)

		if !isHamiCorePluginEnabled(ctx, testCtx) {
			Skip("hamicore binder plugin is not enabled, skipping HAMi-core tests")
		}

		_, err := testCtx.KubeClientset.AdmissionregistrationV1().
			MutatingWebhookConfigurations().
			Get(ctx, kaiResourceIsolatorWebhookName, metav1.GetOptions{})
		if err != nil {
			Skip(fmt.Sprintf(
				"kai-resource-isolator webhook %q not found, skipping HAMi-core tests: %v",
				kaiResourceIsolatorWebhookName, err,
			))
		}

		capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
			GpuMemory: resource.MustParse(strconv.Itoa(gpuMemoryRequestMiB) + "Mi"),
			PodCount:  1,
		})

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*kaiv2.Queue{childQueue, parentQueue})
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	gpuMemoryAnnotationPod := func() *v1.Pod {
		pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
		pod.Spec.Containers[0].Image = cudaImage
		pod.Annotations[constants.GpuMemory] = strconv.Itoa(gpuMemoryRequestMiB)
		return pod
	}

	It("gpu-memory: CUDA_DEVICE_MEMORY_LIMIT is injected and bounded",
		Label(labels.ReservationPod), func(ctx context.Context) {
			pod := gpuMemoryAnnotationPod()

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodReady(ctx, testCtx.ControllerClient, pod)

			pod, err = rd.GetPod(ctx, testCtx.KubeClientset, pod.Namespace, pod.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.NodeName).NotTo(BeEmpty(), "pod should be scheduled to a node")

			totalGPUMemMiB := nodeGPUMemoryMiB(ctx, testCtx, pod.Spec.NodeName)
			GinkgoLogr.Info("GPU info", "node", pod.Spec.NodeName, "totalGPUMemMiB", totalGPUMemMiB)

			limitStr := strings.TrimSpace(rd.ExecInPod(ctx, testCtx.KubeClientset, testCtx.KubeConfig, pod,
				[]string{"sh", "-c", "echo $CUDA_DEVICE_MEMORY_LIMIT"}))
			GinkgoLogr.Info("CUDA_DEVICE_MEMORY_LIMIT", "value", limitStr)
			Expect(limitStr).NotTo(BeEmpty(), "CUDA_DEVICE_MEMORY_LIMIT should be set in the container")

			limitMiB, err := parseCUDAMemoryLimit(limitStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(limitMiB).To(BeNumerically(">", 0))
			Expect(limitMiB).To(BeNumerically("<", totalGPUMemMiB),
				"CUDA_DEVICE_MEMORY_LIMIT (%d MiB) should be less than total GPU memory (%d MiB)",
				limitMiB, totalGPUMemMiB)
		})

	It("gpu-memory: nvidia-smi reports limited GPU memory matching CUDA_DEVICE_MEMORY_LIMIT",
		Label(labels.ReservationPod), func(ctx context.Context) {
			pod := gpuMemoryAnnotationPod()

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodReady(ctx, testCtx.ControllerClient, pod)

			pod, err = rd.GetPod(ctx, testCtx.KubeClientset, pod.Namespace, pod.Name)
			Expect(err).NotTo(HaveOccurred())

			printNvidiaSmi(ctx, testCtx, pod)

			totalGPUMemMiB := nodeGPUMemoryMiB(ctx, testCtx, pod.Spec.NodeName)
			GinkgoLogr.Info("GPU info", "node", pod.Spec.NodeName, "totalGPUMemMiB", totalGPUMemMiB)

			nvidiaSmiRaw := strings.TrimSpace(rd.ExecInPod(ctx, testCtx.KubeClientset, testCtx.KubeConfig, pod,
				[]string{"nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits"}))
			visibleMemMiB, err := strconv.ParseInt(nvidiaSmiRaw, 10, 64)
			Expect(err).NotTo(HaveOccurred())
			GinkgoLogr.Info("nvidia-smi inside container", "memory.total (MiB)", visibleMemMiB)

			Expect(visibleMemMiB).To(BeNumerically("<", totalGPUMemMiB),
				"nvidia-smi inside container should report less than the full GPU memory (%d MiB)", totalGPUMemMiB)

			cudaLimit := strings.TrimSpace(rd.ExecInPod(ctx, testCtx.KubeClientset, testCtx.KubeConfig, pod,
				[]string{"sh", "-c", "echo $CUDA_DEVICE_MEMORY_LIMIT"}))
			limitMiB, err := parseCUDAMemoryLimit(cudaLimit)
			Expect(err).NotTo(HaveOccurred())
			GinkgoLogr.Info("CUDA_DEVICE_MEMORY_LIMIT", "value", cudaLimit, "parsedMiB", limitMiB)

			Expect(visibleMemMiB).To(BeNumerically("==", limitMiB),
				"nvidia-smi visible memory (%d MiB) should match CUDA_DEVICE_MEMORY_LIMIT (%d MiB)",
				visibleMemMiB, limitMiB)
		})

	It("gpu-fraction: CUDA_DEVICE_MEMORY_LIMIT is injected and proportional",
		Label(labels.ReservationPod), func(ctx context.Context) {
			const gpuFraction = 0.25

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod.Spec.Containers[0].Image = cudaImage
			pod.Annotations[constants.GpuFraction] = fmt.Sprintf("%g", gpuFraction)

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodReady(ctx, testCtx.ControllerClient, pod)

			pod, err = rd.GetPod(ctx, testCtx.KubeClientset, pod.Namespace, pod.Name)
			Expect(err).NotTo(HaveOccurred())

			printNvidiaSmi(ctx, testCtx, pod)

			totalGPUMemMiB := nodeGPUMemoryMiB(ctx, testCtx, pod.Spec.NodeName)
			GinkgoLogr.Info("GPU info", "node", pod.Spec.NodeName, "totalGPUMemMiB", totalGPUMemMiB, "requestedFraction", gpuFraction)

			cudaLimit := strings.TrimSpace(rd.ExecInPod(ctx, testCtx.KubeClientset, testCtx.KubeConfig, pod,
				[]string{"sh", "-c", "echo $CUDA_DEVICE_MEMORY_LIMIT"}))
			limitMiB, err := parseCUDAMemoryLimit(cudaLimit)
			Expect(err).NotTo(HaveOccurred())
			GinkgoLogr.Info("CUDA_DEVICE_MEMORY_LIMIT", "value", cudaLimit, "parsedMiB", limitMiB)

			expectedMiB := int64(float64(totalGPUMemMiB) * gpuFraction)
			tolerance := int64(float64(totalGPUMemMiB) * 0.01)
			Expect(limitMiB).To(BeNumerically("~", expectedMiB, tolerance),
				"CUDA_DEVICE_MEMORY_LIMIT (%d MiB) should be ~%d MiB (%.0f%% of %d MiB)",
				limitMiB, expectedMiB, gpuFraction*100, totalGPUMemMiB)

			nvidiaSmiRaw := strings.TrimSpace(rd.ExecInPod(ctx, testCtx.KubeClientset, testCtx.KubeConfig, pod,
				[]string{"nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits"}))
			visibleMemMiB, err := strconv.ParseInt(nvidiaSmiRaw, 10, 64)
			Expect(err).NotTo(HaveOccurred())
			GinkgoLogr.Info("nvidia-smi inside container", "memory.total (MiB)", visibleMemMiB)
			Expect(visibleMemMiB).To(BeNumerically("<", totalGPUMemMiB),
				"nvidia-smi inside container should report limited GPU memory for fraction request")
			Expect(visibleMemMiB).To(BeNumerically("==", limitMiB),
				"nvidia-smi visible memory (%d MiB) should match CUDA_DEVICE_MEMORY_LIMIT (%d MiB)",
				visibleMemMiB, limitMiB)
		})
})

func printNvidiaSmi(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) {
	out := rd.ExecInPod(ctx, testCtx.KubeClientset, testCtx.KubeConfig, pod,
		[]string{"nvidia-smi"})
	fmt.Fprintf(GinkgoWriter, "\n=== nvidia-smi output inside pod %s/%s ===\n%s\n===\n",
		pod.Namespace, pod.Name, out)
}

func nodeGPUMemoryMiB(ctx context.Context, testCtx *testcontext.TestContext, nodeName string) int64 {
	node, err := testCtx.KubeClientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get node %s", nodeName)

	memPerGPUStr, ok := node.Labels["nvidia.com/gpu.memory"]
	Expect(ok).To(BeTrue(), "node %s is missing nvidia.com/gpu.memory label", nodeName)

	memPerGPU, err := strconv.ParseInt(memPerGPUStr, 10, 64)
	Expect(err).NotTo(HaveOccurred())

	numGPUs := node.Status.Allocatable[v1.ResourceName("nvidia.com/gpu")]
	total := memPerGPU * numGPUs.Value()
	Expect(total).To(BeNumerically(">", 0), "node %s reports 0 GPU memory", nodeName)
	return total
}

func parseCUDAMemoryLimit(val string) (int64, error) {
	if val == "" {
		return 0, fmt.Errorf("CUDA_DEVICE_MEMORY_LIMIT is not set")
	}
	s := strings.TrimSuffix(val, "m")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse CUDA_DEVICE_MEMORY_LIMIT value %q: %w", val, err)
	}
	return n, nil
}

func isHamiCorePluginEnabled(ctx context.Context, testCtx *testcontext.TestContext) bool {
	deploy, err := testCtx.KubeClientset.AppsV1().Deployments(binderDeploymentNamespace).
		Get(ctx, binderDeploymentName, metav1.GetOptions{})
	if err != nil {
		return false
	}
	if len(deploy.Spec.Template.Spec.Containers) == 0 {
		return false
	}

	args := deploy.Spec.Template.Spec.Containers[0].Args
	for i, arg := range args {
		if arg == binderPluginsFlag && i+1 < len(args) {
			return hamiCoreEnabledInPluginsJSON(args[i+1])
		}
		// Also handle --plugins=<json> single-token form.
		if strings.HasPrefix(arg, binderPluginsFlag+"=") {
			return hamiCoreEnabledInPluginsJSON(strings.TrimPrefix(arg, binderPluginsFlag+"="))
		}
	}
	return false
}

func hamiCoreEnabledInPluginsJSON(raw string) bool {
	var plugins map[string]kaiv1binder.PluginConfig
	if err := json.Unmarshal([]byte(raw), &plugins); err != nil {
		return false
	}
	cfg, ok := plugins[kaiv1binder.HamiCorePluginName]
	if !ok {
		return false
	}
	return ptr.Deref(cfg.Enabled, false)
}
