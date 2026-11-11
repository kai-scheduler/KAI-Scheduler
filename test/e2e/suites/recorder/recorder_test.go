// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	recorderv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/recorder/v1"
	schedrec "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/api/constants"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

const (
	nodePoolKey      = "kai.scheduler/node-pool"
	tokenPath        = "/var/run/secrets/kubernetes.io/serviceaccount"
	allocatePool     = "kai-rec-pool-a"
	preemptPool      = "kai-rec-pool-b"
	lowPriorityName  = "kai-rec-low"
	highPriorityName = "kai-rec-high"
)

// liveScheduler is the shard the operator actually deployed.
// The scheduler container is named after that deployment, not "scheduler".
type liveScheduler struct {
	namespace     string
	shardName     string
	containerName string
	hashSecret    string
	storageSecret string
}

var _ = Describe("Recorder", Ordered, ContinueOnFailure, Label("recorder"), func() {
	var (
		testCtx      *testcontext.TestContext
		live         liveScheduler
		allocatePod  *v1.Pod
		victimPod    *v1.Pod
		preemptorPod *v1.Pod
		leafQueue    *v2.Queue
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		live = discoverScheduler(ctx, testCtx)
	})

	AfterAll(func(ctx context.Context) {
		if testCtx == nil {
			return
		}
		clearNodePoolLabels(ctx, testCtx.KubeClientset)
		_ = testCtx.KubeClientset.SchedulingV1().PriorityClasses().Delete(ctx, lowPriorityName, metav1.DeleteOptions{})
		_ = testCtx.KubeClientset.SchedulingV1().PriorityClasses().Delete(ctx, highPriorityName, metav1.DeleteOptions{})
		if live.namespace != "" {
			_ = testCtx.KubeClientset.CoreV1().Pods(live.namespace).Delete(ctx, "recorder-log-reader", metav1.DeleteOptions{})
			patchRecorderEnabled(ctx, testCtx.ControllerClient, live.shardName, false)
			_ = testCtx.KubeClientset.CoreV1().Secrets(live.namespace).Delete(ctx, live.storageSecret, metav1.DeleteOptions{})
		}
		testCtx.ClusterCleanup(ctx)
	})

	It("stays off unless a shard enables it", func(ctx context.Context) {
		pod := schedulerPod(ctx, testCtx, live)
		Expect(containerNames(pod)).To(Equal([]string{live.containerName}))
		Expect(otherContainer(pod, live.containerName)).To(BeNil())
		Expect(volumeNames(pod)).NotTo(ContainElement("recorder-data"))
		_, err := testCtx.KubeClientset.CoreV1().Secrets(live.namespace).Get(ctx, live.hashSecret, metav1.GetOptions{})
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("deploys the sidecar and creates the hash-key secret", func(ctx context.Context) {
		ensureRecorder(ctx, testCtx, live)

		var first []byte
		Eventually(func(g Gomega) {
			stored, err := testCtx.KubeClientset.CoreV1().Secrets(live.namespace).Get(ctx, live.hashSecret, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(stored.Namespace).To(Equal(live.namespace))
			g.Expect(stored.Data[recorderv1.HMACKeyDataKey]).NotTo(BeEmpty())
			first = append([]byte(nil), stored.Data[recorderv1.HMACKeyDataKey]...)
		}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())

		// A second look must see the same key. The operator does not rotate it.
		stored, err := testCtx.KubeClientset.CoreV1().Secrets(live.namespace).Get(ctx, live.hashSecret, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(stored.Data[recorderv1.HMACKeyDataKey]).To(Equal(first))
	})

	It("hides the service account token from the sidecar and leaves it on the scheduler", func(ctx context.Context) {
		ensureRecorder(ctx, testCtx, live)
		pod := schedulerPod(ctx, testCtx, live)
		side := otherContainer(pod, live.containerName)
		Expect(side).NotTo(BeNil())
		sideMount := mountedVolume(&pod, side.Name, tokenPath)
		Expect(sideMount).NotTo(BeNil())
		Expect(sideMount.EmptyDir).NotTo(BeNil())
		schedulerMount := mountedVolume(&pod, live.containerName, tokenPath)
		Expect(schedulerMount == nil || schedulerMount.EmptyDir == nil).To(BeTrue())

		sidecarListing := probeTokenDir(ctx, testCtx, live, &pod, side.Name)
		current := schedulerPod(ctx, testCtx, live)
		schedulerListing := probeTokenDir(ctx, testCtx, live, &current, live.containerName)
		Expect(sidecarListing).To(ContainSubstring("PID"))
		Expect(sidecarListing).NotTo(ContainSubstring("LIST_FAILED"))
		Expect(sidecarListing).NotTo(ContainSubstring("NO_APP"))
		Expect(schedulerListing).To(ContainSubstring("PID"))

		Expect(sidecarListing).NotTo(ContainSubstring("token"),
			"the recorder sidecar still has a service-account token; the emptyDir mask did not work:\n%s", sidecarListing)
		Expect(sidecarListing).NotTo(ContainSubstring("ca.crt"),
			"the recorder sidecar still has a service-account certificate; the emptyDir mask did not work:\n%s", sidecarListing)
		Expect(schedulerListing).To(ContainSubstring("token"),
			"the scheduler container lost its service-account token:\n%s", schedulerListing)
	})

	It("records allocate and preempt with hashed names", func(ctx context.Context) {
		// InitQueues uses the context captured when the client was created.
		// The BeforeAll context is already canceled, so refresh it for this spec.
		testCtx = testcontext.GetConnectivity(ctx, Default)
		ensureRecorder(ctx, testCtx, live)
		allocateNode, preemptNode := waitForGPUNodes(ctx, testCtx.KubeClientset)
		labelNode(ctx, testCtx.KubeClientset, allocateNode, allocatePool)
		labelNode(ctx, testCtx.KubeClientset, preemptNode, preemptPool)
		createPriorityClasses(ctx, testCtx.KubeClientset)

		parent := queue.CreateQueueObject("kai-rec-parent", "")
		leafQueue = queue.CreateQueueObject("kai-rec-leaf", parent.Name)
		testCtx.InitQueues([]*v2.Queue{parent, leafQueue})

		allocatePod = gpuPod(leafQueue, 1, highPriorityName, allocatePool)
		allocatePod, err := rd.CreatePod(ctx, testCtx.KubeClientset, allocatePod)
		Expect(err).NotTo(HaveOccurred())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, allocatePod)

		victimGPUs := idleGPUs(testCtx.KubeClientset, preemptNode)
		victimPod = gpuPod(leafQueue, victimGPUs, lowPriorityName, preemptPool)
		victimPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, victimPod)
		Expect(err).NotTo(HaveOccurred())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, victimPod)

		preemptorPod = gpuPod(leafQueue, 1, highPriorityName, preemptPool)
		preemptorPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, preemptorPod)
		Expect(err).NotTo(HaveOccurred())
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, preemptorPod)

		key := hashKey(ctx, testCtx, live)
		h := schedrec.NewHasher(key)
		Eventually(func(g Gomega) {
			body, err := readRecords(ctx, testCtx, live)
			g.Expect(err).NotTo(HaveOccurred())
			requireHashed(g, h, body, allocatePod.Namespace)
			requireHashed(g, h, body, allocatePod.Name)
			requireHashed(g, h, body, victimPod.Name)
			requireHashed(g, h, body, preemptorPod.Name)
			requireHashed(g, h, body, leafQueue.Name)
			requireHashed(g, h, body, lowPriorityName)
			requireHashed(g, h, body, highPriorityName)
			requireHashed(g, h, body, allocatePool)
			requireHashed(g, h, body, preemptPool)
			g.Expect(body).To(ContainSubstring(nodePoolKey))
			g.Expect(decisionLines(body, "allocate")).To(ContainElement(ContainSubstring(h.Hash(allocatePod.Name))))
			g.Expect(decisionLines(body, "preempt")).To(ContainElement(ContainSubstring(h.Hash(victimPod.Name))))
			g.Expect(decisionLines(body, "preempt")).To(ContainElement(ContainSubstring(h.Hash(preemptorPod.Name))))
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	})

	It("replays a recorded preempt cycle onto the same placements", func(ctx context.Context) {
		if victimPod == nil || preemptorPod == nil {
			Fail("preempt workload was not created, so there is no recorded cycle to replay")
		}
		body, err := readRecords(ctx, testCtx, live)
		Expect(err).NotTo(HaveOccurred())
		key := hashKey(ctx, testCtx, live)
		h := schedrec.NewHasher(key)
		state, recorded := preemptCycle(body, h.Hash(victimPod.Name), h.Hash(preemptorPod.Name))
		replayed := replayCycle(state, "preempt")
		Expect(replayed).To(Equal(recorded))
	})
})

func requireHashed(g Gomega, h schedrec.Hasher, body, plain string) {
	g.Expect(body).NotTo(ContainSubstring(plain), "plain value %q was written into the recording", plain)
	g.Expect(body).To(ContainSubstring(h.Hash(plain)))
}

func schedulerPod(ctx context.Context, testCtx *testcontext.TestContext, live liveScheduler) v1.Pod {
	pods, err := testCtx.KubeClientset.CoreV1().Pods(live.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: constants.AppLabelName + "=" + live.containerName,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(pods.Items).NotTo(BeEmpty())
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp == nil {
			return pod
		}
	}
	return pods.Items[0]
}

func otherContainer(pod v1.Pod, schedulerContainer string) *v1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name != schedulerContainer {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}

func mountedVolume(pod *v1.Pod, containerName, path string) *v1.Volume {
	name := mountPath(pod, containerName, path)
	if name == "" {
		return nil
	}
	for i := range pod.Spec.Volumes {
		if pod.Spec.Volumes[i].Name == name {
			return &pod.Spec.Volumes[i]
		}
	}
	return nil
}

func containerNames(pod v1.Pod) []string {
	names := make([]string, 0, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		names = append(names, container.Name)
	}
	return names
}

func volumeNames(pod v1.Pod) []string {
	names := make([]string, 0, len(pod.Spec.Volumes))
	for _, volume := range pod.Spec.Volumes {
		names = append(names, volume.Name)
	}
	return names
}

func mountPath(pod *v1.Pod, containerName, path string) string {
	for _, container := range pod.Spec.Containers {
		if container.Name != containerName {
			continue
		}
		for _, mount := range container.VolumeMounts {
			if mount.MountPath == path {
				return mount.Name
			}
		}
	}
	return ""
}

func patchRecorderEnabled(ctx context.Context, c client.Client, shardName string, enabled bool) {
	shard := &unstructured.Unstructured{}
	shard.SetGroupVersionKind(kaiv1.GroupVersion.WithKind("SchedulingShard"))
	shard.SetName(shardName)
	payload := []byte(fmt.Sprintf(`{"spec":{"recorder":{"enabled":%t}}}`, enabled))
	Expect(c.Patch(ctx, shard, client.RawPatch(types.MergePatchType, payload))).To(Succeed())
}

func discoverScheduler(ctx context.Context, testCtx *testcontext.TestContext) liveScheduler {
	kaiConfig := &kaiv1.Config{}
	Expect(testCtx.ControllerClient.Get(ctx, client.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName}, kaiConfig)).To(Succeed())
	Expect(kaiConfig.Spec.Namespace).NotTo(BeEmpty())
	schedulerName := constants.DefaultSchedulerName
	if kaiConfig.Spec.Global != nil && kaiConfig.Spec.Global.SchedulerName != nil && *kaiConfig.Spec.Global.SchedulerName != "" {
		schedulerName = *kaiConfig.Spec.Global.SchedulerName
	}

	var shards kaiv1.SchedulingShardList
	Expect(testCtx.ControllerClient.List(ctx, &shards)).To(Succeed())
	var found *liveScheduler
	for i := range shards.Items {
		deployment := fmt.Sprintf("%s-%s", schedulerName, shards.Items[i].Name)
		pods, err := testCtx.KubeClientset.CoreV1().Pods(kaiConfig.Spec.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: constants.AppLabelName + "=" + deployment,
		})
		Expect(err).NotTo(HaveOccurred())
		if len(pods.Items) == 0 {
			continue
		}
		hashSecret, storageSecret := recorderSecretNames(ctx, testCtx, shards.Items[i].Name)
		found = &liveScheduler{
			namespace:     kaiConfig.Spec.Namespace,
			shardName:     shards.Items[i].Name,
			containerName: deployment,
			hashSecret:    hashSecret,
			storageSecret: storageSecret,
		}
		break
	}
	Expect(found).NotTo(BeNil(), "no scheduler pod matched a SchedulingShard in %s", kaiConfig.Spec.Namespace)
	return *found
}

func recorderSecretNames(ctx context.Context, testCtx *testcontext.TestContext, shardName string) (hashSecret, storageSecret string) {
	shard := &unstructured.Unstructured{}
	shard.SetGroupVersionKind(kaiv1.GroupVersion.WithKind("SchedulingShard"))
	Expect(testCtx.ControllerClient.Get(ctx, client.ObjectKey{Name: shardName}, shard)).To(Succeed())
	hashSecret, _, _ = unstructured.NestedString(shard.Object, "spec", "recorder", "hashKeySecretName")
	storageSecret, _, _ = unstructured.NestedString(shard.Object, "spec", "recorder", "storageSecretName")
	if hashSecret == "" {
		hashSecret = recorderv1.DefaultHashKeySecretName
	}
	if storageSecret == "" {
		storageSecret = recorderv1.DefaultStorageSecretName
	}
	return hashSecret, storageSecret
}

func ensureRecorder(ctx context.Context, testCtx *testcontext.TestContext, live liveScheduler) {
	secret := &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: live.storageSecret, Namespace: live.namespace}}
	_, err := testCtx.KubeClientset.CoreV1().Secrets(live.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	patchRecorderEnabled(ctx, testCtx.ControllerClient, live.shardName, true)
	Eventually(func(g Gomega) {
		pod := schedulerPod(ctx, testCtx, live)
		g.Expect(rd.IsPodRunning(&pod)).To(BeTrue())
		side := otherContainer(pod, live.containerName)
		g.Expect(side).NotTo(BeNil())
		g.Expect(side.Image).NotTo(BeEmpty())
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
}

func hashKey(ctx context.Context, testCtx *testcontext.TestContext, live liveScheduler) []byte {
	stored, err := testCtx.KubeClientset.CoreV1().Secrets(live.namespace).Get(ctx, live.hashSecret, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(stored.Data[recorderv1.HMACKeyDataKey]).NotTo(BeEmpty())
	return stored.Data[recorderv1.HMACKeyDataKey]
}

func waitForGPUNodes(ctx context.Context, kube *kubernetes.Clientset) (allocateNode, preemptNode string) {
	Eventually(func(g Gomega) {
		nodes, err := capacity.GetNodesIdleResources(kube)
		g.Expect(err).NotTo(HaveOccurred())
		var found []string
		for name, resources := range nodes {
			if resources.Gpu.Cmp(resource.MustParse("1")) >= 0 {
				found = append(found, name)
			}
		}
		g.Expect(len(found)).To(BeNumerically(">=", 2), "need two GPU nodes with a free GPU, got %v", found)
		allocateNode, preemptNode = found[0], found[1]
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	return allocateNode, preemptNode
}

func idleGPUs(kube *kubernetes.Clientset, nodeName string) int64 {
	nodes, err := capacity.GetNodesIdleResources(kube)
	Expect(err).NotTo(HaveOccurred())
	resources, found := nodes[nodeName]
	Expect(found).To(BeTrue(), "node %s disappeared", nodeName)
	Expect(resources.Gpu.Value()).To(BeNumerically(">", 0))
	return resources.Gpu.Value()
}

func labelNode(ctx context.Context, kube *kubernetes.Clientset, nodeName, pool string) {
	payload := []byte(fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, nodePoolKey, pool))
	_, err := kube.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, payload, metav1.PatchOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func clearNodePoolLabels(ctx context.Context, kube *kubernetes.Clientset) {
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	payload := []byte(fmt.Sprintf(`{"metadata":{"labels":{%q:null}}}`, nodePoolKey))
	for _, node := range nodes.Items {
		if _, found := node.Labels[nodePoolKey]; !found {
			continue
		}
		_, _ = kube.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, payload, metav1.PatchOptions{})
	}
}

func createPriorityClasses(ctx context.Context, kube *kubernetes.Clientset) {
	for _, class := range []*schedulingv1.PriorityClass{
		{ObjectMeta: metav1.ObjectMeta{Name: lowPriorityName}, Value: 40},
		{ObjectMeta: metav1.ObjectMeta{Name: highPriorityName}, Value: 80},
	} {
		_, err := kube.SchedulingV1().PriorityClasses().Create(ctx, class, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
	}
}

func gpuPod(q *v2.Queue, gpus int64, priorityClass, pool string) *v1.Pod {
	pod := rd.CreatePodObject(q, v1.ResourceRequirements{
		Limits: v1.ResourceList{constants.NvidiaGpuResource: *resource.NewQuantity(gpus, resource.DecimalSI)},
	})
	pod.Spec.PriorityClassName = priorityClass
	pod.Spec.NodeSelector = map[string]string{nodePoolKey: pool}
	return pod
}

func readRecords(ctx context.Context, testCtx *testcontext.TestContext, live liveScheduler) (string, error) {
	pod := schedulerPod(ctx, testCtx, live)
	return execReader(ctx, testCtx, live, &pod)
}

func execReader(ctx context.Context, testCtx *testcontext.TestContext, live liveScheduler, sched *v1.Pod) (string, error) {
	reader := ensureReader(ctx, testCtx, live, sched)
	command := []string{"sh", "-c", `
set -eu
ls -la /volumes || true
find /volumes -type f | sort | while read -r f; do
  echo "===== $f"
  case "$f" in
    *.gz) gzip -dc "$f" || true ;;
    *) cat "$f" || true ;;
  esac
  echo
done
`}
	return execContainer(ctx, testCtx.KubeClientset, testCtx.KubeConfig, reader, "reader", command)
}

func ensureReader(ctx context.Context, testCtx *testcontext.TestContext, live liveScheduler, sched *v1.Pod) *v1.Pod {
	cfg := testconfig.GetConfig()
	existing, err := testCtx.KubeClientset.CoreV1().Pods(live.namespace).Get(ctx, "recorder-log-reader", metav1.GetOptions{})
	if err == nil && readerMatches(existing, sched) {
		if rd.IsPodRunning(existing) {
			return existing
		}
	}
	if err == nil {
		Expect(testCtx.KubeClientset.CoreV1().Pods(live.namespace).Delete(ctx, existing.Name, metav1.DeleteOptions{})).To(Succeed())
		Eventually(func() bool {
			_, getErr := testCtx.KubeClientset.CoreV1().Pods(live.namespace).Get(ctx, "recorder-log-reader", metav1.GetOptions{})
			return errors.IsNotFound(getErr)
		}).WithTimeout(time.Minute).Should(BeTrue())
	}
	hostPath := fmt.Sprintf("/var/lib/kubelet/pods/%s/volumes", sched.UID)
	dir := v1.HostPathDirectory
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "recorder-log-reader", Namespace: live.namespace},
		Spec: v1.PodSpec{
			NodeName:      sched.Spec.NodeName,
			RestartPolicy: v1.RestartPolicyAlways,
			Containers: []v1.Container{{
				Name:    "reader",
				Image:   cfg.ContainerImage,
				Command: []string{"sleep", "infinity"},
				SecurityContext: &v1.SecurityContext{
					Privileged: ptr.To(true),
					RunAsUser:  ptr.To(int64(0)),
				},
				VolumeMounts: []v1.VolumeMount{{Name: "volumes", MountPath: "/volumes"}},
			}},
			Volumes: []v1.Volume{{
				Name:         "volumes",
				VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: hostPath, Type: &dir}},
			}},
		},
	}
	created, err := testCtx.KubeClientset.CoreV1().Pods(live.namespace).Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		current, err := testCtx.KubeClientset.CoreV1().Pods(live.namespace).Get(ctx, created.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(rd.IsPodRunning(current)).To(BeTrue())
	}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
	return created
}

func readerMatches(pod, sched *v1.Pod) bool {
	if pod.Spec.NodeName != sched.Spec.NodeName || len(pod.Spec.Volumes) == 0 || pod.Spec.Volumes[0].HostPath == nil {
		return false
	}
	return strings.Contains(pod.Spec.Volumes[0].HostPath.Path, string(sched.UID))
}

func probeTokenDir(ctx context.Context, testCtx *testcontext.TestContext, live liveScheduler, pod *v1.Pod, containerName string) string {
	name := "token-probe-" + containerName
	addTokenProbe(ctx, testCtx, pod, name, containerName)
	Eventually(func(g Gomega) {
		current := schedulerPod(ctx, testCtx, live)
		g.Expect(ephemeralRunning(&current, name)).To(BeTrue())
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

	script := `
found=0
for d in /proc/[0-9]*; do
  cmd=$(tr '\0' ' ' < "$d/cmdline" 2>/dev/null || true)
  case "$cmd" in
    /workspace/app*)
      echo "PID ${d#/proc/}"
      ls -la "$d/root` + tokenPath + `" || echo "LIST_FAILED"
      found=1
      ;;
  esac
done
if [ "$found" -eq 0 ]; then
  echo "NO_APP"
  ls /proc
  exit 1
fi
`
	current := schedulerPod(ctx, testCtx, live)
	out, err := execContainer(ctx, testCtx.KubeClientset, testCtx.KubeConfig, &current, name, []string{"sh", "-c", script})
	Expect(err).NotTo(HaveOccurred(), out)
	return out
}

func addTokenProbe(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod, name, target string) {
	current, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	for _, container := range current.Spec.EphemeralContainers {
		if container.Name == name {
			return
		}
	}
	current.Spec.EphemeralContainers = append(current.Spec.EphemeralContainers, v1.EphemeralContainer{
		EphemeralContainerCommon: v1.EphemeralContainerCommon{
			Name:    name,
			Image:   testconfig.GetConfig().ContainerImage,
			Command: []string{"sleep", "infinity"},
			SecurityContext: &v1.SecurityContext{
				Privileged:   ptr.To(true),
				RunAsUser:    ptr.To(int64(0)),
				RunAsNonRoot: ptr.To(false),
			},
		},
		TargetContainerName: target,
	})
	_, err = testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).UpdateEphemeralContainers(ctx, current.Name, current, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func ephemeralRunning(pod *v1.Pod, name string) bool {
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if status.Name == name && status.State.Running != nil {
			return true
		}
	}
	return false
}

func execContainer(ctx context.Context, kube *kubernetes.Clientset, config *rest.Config, pod *v1.Pod, container string, command []string) (string, error) {
	req := kube.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	if err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String() + stderr.String(), nil
}

func decisionLines(body, action string) []string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, `"kind":"decision"`) && !strings.Contains(line, `"kind": "decision"`) {
			continue
		}
		var env schedrec.Envelope
		if json.Unmarshal([]byte(line), &env) != nil || env.Kind != schedrec.KindDecision {
			continue
		}
		var decision schedrec.DecisionRecord
		if json.Unmarshal(env.Record, &decision) != nil || decision.Action != action {
			continue
		}
		for _, op := range decision.Ops {
			if op.NotAttempted {
				continue
			}
			lines = append(lines, decision.Action+" "+op.Kind+" "+op.Namespace+"/"+op.Name+"@"+op.Node)
		}
	}
	return lines
}

func preemptCycle(body, victimHash, preemptorHash string) (schedrec.StateRecord, []string) {
	states := map[string]schedrec.StateRecord{}
	opsBySession := map[string][]string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var env schedrec.Envelope
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		switch env.Kind {
		case schedrec.KindState:
			var state schedrec.StateRecord
			if json.Unmarshal(env.Record, &state) == nil && state.SessionID != "" {
				states[state.SessionID] = state
			}
		case schedrec.KindDecision:
			var decision schedrec.DecisionRecord
			if json.Unmarshal(env.Record, &decision) != nil || decision.Action != "preempt" {
				continue
			}
			for _, op := range decision.Ops {
				if op.NotAttempted {
					continue
				}
				opsBySession[decision.SessionID] = append(opsBySession[decision.SessionID],
					decision.Action+" "+op.Kind+" "+op.Namespace+"/"+op.Name+"@"+op.Node)
			}
		}
	}
	for sessionID, ops := range opsBySession {
		joined := strings.Join(ops, "\n")
		if !strings.Contains(joined, victimHash) || !strings.Contains(joined, preemptorHash) {
			continue
		}
		state, found := states[sessionID]
		Expect(found).To(BeTrue(), "preempt session %s has no state copy", sessionID)
		return state, sortedCopy(ops)
	}
	Fail("recorded preempt cycle for the test pods was not found")
	return schedrec.StateRecord{}, nil
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
