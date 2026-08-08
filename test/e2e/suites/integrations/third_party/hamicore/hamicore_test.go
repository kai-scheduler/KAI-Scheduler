/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package hamicore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	kaiv1binder "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/binder"
	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	kaiResourceIsolatorWebhookName = "kai-resource-isolator-mutating"
	kaiResourceIsolatorNamespace   = "kai-resource-isolator"
	kaiVGPUMonitorDaemonSetName    = "kai-resource-isolator-monitor"
	kaiVGPUMonitorLabel            = "app.kubernetes.io/component=kai-vgpu-monitor"
	kaiVGPUMonitorMetricsPort      = "9394"
	binderDeploymentName           = "binder"
	binderDeploymentNamespace      = "kai-scheduler"
	binderPluginsFlag              = "--plugins"
	cudaImage                      = "nvidia/cuda:12.6.0-base-ubuntu22.04"
	// devel image is needed so the metrics workload can nvcc a tiny cudaMalloc hold binary.
	cudaDevelImage      = "nvidia/cuda:12.6.0-devel-ubuntu22.04"
	gpuMemoryRequestMiB = 2000
	cudaAllocHoldMiB    = 64
	vgpuMetricsTimeout  = 60 * time.Second
	vgpuMetricsInterval = 5 * time.Second
	hamiVGPUMemoryUsed  = "hami_vgpu_memory_used_bytes"
)

// Compiles and runs a small CUDA alloc that holds memory so kai-vgpu-monitor can
// scrape hami_vgpu_memory_used_bytes > 0. Kept as a container entrypoint (not
// ExecInPod) so the allocation outlives the ready wait.
const cudaAllocHoldScript = `set -euo pipefail
cat >/tmp/hold.cu <<'EOF'
#include <cuda_runtime.h>
#include <stdio.h>
#include <unistd.h>
int main(void) {
  void *p = NULL;
  size_t bytes = (size_t)64 * 1024 * 1024;
  cudaError_t err = cudaMalloc(&p, bytes);
  if (err != cudaSuccess) {
    fprintf(stderr, "cudaMalloc failed: %s\n", cudaGetErrorString(err));
    return 1;
  }
  printf("allocated %zu bytes at %p\n", bytes, p);
  fflush(stdout);
  for (;;) {
    sleep(3600);
  }
  return 0;
}
EOF
nvcc -O0 -o /tmp/hold /tmp/hold.cu
exec /tmp/hold
`

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

	It("gpu-memory: kai-vgpu-monitor reports hami_vgpu_memory_used_bytes > 0",
		Label(labels.ReservationPod), func(ctx context.Context) {
			if !isKaiVGPUMonitorInstalled(ctx, testCtx.KubeClientset) {
				Skip(fmt.Sprintf(
					"kai-vgpu-monitor DaemonSet %q not found in namespace %q; "+
						"install via hack/third_party_integrations/deploy_isolator.sh",
					kaiVGPUMonitorDaemonSetName, kaiResourceIsolatorNamespace,
				))
			}

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod.Annotations[constants.GpuMemory] = strconv.Itoa(gpuMemoryRequestMiB)
			pod.Spec.Containers[0].Image = cudaDevelImage
			pod.Spec.Containers[0].Command = []string{"bash", "-c"}
			pod.Spec.Containers[0].Args = []string{cudaAllocHoldScript}

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodReady(ctx, testCtx.ControllerClient, pod)

			pod, err = rd.GetPod(ctx, testCtx.KubeClientset, pod.Namespace, pod.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.NodeName).NotTo(BeEmpty(), "pod should be scheduled to a node")
			containerName := pod.Spec.Containers[0].Name

			By("waiting for kai-vgpu-monitor to expose hami_vgpu_memory_used_bytes for the workload")
			Eventually(func(g Gomega) {
				used, scrapeErr := scrapeHamiVGPUMemoryUsedBytes(
					ctx, testCtx.KubeClientset, pod.Spec.NodeName, pod.Namespace, pod.Name, containerName,
				)
				g.Expect(scrapeErr).NotTo(HaveOccurred())
				GinkgoLogr.Info("hami_vgpu_memory_used_bytes",
					"namespace", pod.Namespace, "pod", pod.Name, "container", containerName, "bytes", used)
				g.Expect(used).To(BeNumerically(">", 0),
					"expected hami_vgpu_memory_used_bytes > 0 for %s/%s container %s (held ~%d MiB)",
					pod.Namespace, pod.Name, containerName, cudaAllocHoldMiB)
			}, vgpuMetricsTimeout, vgpuMetricsInterval).Should(Succeed())
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

func isKaiVGPUMonitorInstalled(ctx context.Context, client kubernetes.Interface) bool {
	_, err := client.AppsV1().DaemonSets(kaiResourceIsolatorNamespace).
		Get(ctx, kaiVGPUMonitorDaemonSetName, metav1.GetOptions{})
	return err == nil
}

func scrapeHamiVGPUMemoryUsedBytes(
	ctx context.Context,
	client kubernetes.Interface,
	nodeName, namespace, podName, containerName string,
) (float64, error) {
	monitorPod, err := findMonitorPodOnNode(ctx, client, nodeName)
	if err != nil {
		return 0, err
	}

	raw, err := client.CoreV1().Pods(monitorPod.Namespace).
		ProxyGet("http", monitorPod.Name, kaiVGPUMonitorMetricsPort, "/metrics", nil).
		DoRaw(ctx)
	if err != nil {
		return 0, fmt.Errorf("proxy GET /metrics from monitor pod %s/%s: %w",
			monitorPod.Namespace, monitorPod.Name, err)
	}

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("parse prometheus metrics: %w", err)
	}

	family, ok := families[hamiVGPUMemoryUsed]
	if !ok {
		return 0, fmt.Errorf("metric %s not present in monitor scrape", hamiVGPUMemoryUsed)
	}

	want := map[string]string{
		"namespace": namespace,
		"pod":       podName,
		"container": containerName,
	}
	for _, metric := range family.GetMetric() {
		if !metricHasLabels(metric, want) {
			continue
		}
		if metric.GetGauge() == nil {
			return 0, fmt.Errorf("metric %s for %v is not a gauge", hamiVGPUMemoryUsed, want)
		}
		return metric.GetGauge().GetValue(), nil
	}
	return 0, fmt.Errorf("metric %s with labels %v not found", hamiVGPUMemoryUsed, want)
}

func findMonitorPodOnNode(ctx context.Context, client kubernetes.Interface, nodeName string) (*v1.Pod, error) {
	pods, err := client.CoreV1().Pods(kaiResourceIsolatorNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: kaiVGPUMonitorLabel,
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return nil, fmt.Errorf("list kai-vgpu-monitor pods on node %s: %w", nodeName, err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if rd.IsPodReady(pod) {
			return pod, nil
		}
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no kai-vgpu-monitor pods scheduled on node %s", nodeName)
	}
	return nil, fmt.Errorf("kai-vgpu-monitor pod(s) on node %s are not Ready", nodeName)
}

func metricHasLabels(metric *dto.Metric, want map[string]string) bool {
	got := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		got[label.GetName()] = label.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
