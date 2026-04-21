// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package env_tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xyproto/randomstring"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	schedulingv2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/env-tests/binder"
	"github.com/NVIDIA/KAI-scheduler/pkg/env-tests/utils"
)

// This test reproduces a race condition in the binder's resource reservation
// sync logic at scale (4 nodes × 8 GPUs = 32 GPU groups). The full binder
// controller runs autonomously; the test only creates Kubernetes objects and
// observes outcomes.
//
// The race:
//  1. A fraction pod binds, creating a reservation pod. The binder patches the
//     fraction pod with a GPU group label, but the informer cache may lag.
//  2. A concurrent SyncForGpuGroup (triggered by another BindRequest reconcile
//     or deletion) lists pods by GPU group label from the cache.
//  3. Due to cache lag, the sync sees the reservation pod but no fraction pods
//     for that group → deletes the reservation pod prematurely.
//
// The test pre-creates reservation pods (as if ReserveGpuDevice placed them)
// and fraction BindRequests. Because envtest has no GPU device plugin, the
// BindRequest reconciliation will fail to fully bind the fractional pods (no
// GPU index annotation on reservation pods). However, the reconciler still
// calls SyncForNode at the start of each reconcile, which triggers the
// problematic reservation pod cleanup path.
var _ = Describe("Reservation pod race at scale", Ordered, func() {
	const (
		numNodes    = 4
		gpusPerNode = 8

		gpuIndexAnnotationKey  = "run.ai/reserve_for_gpu_index"
		gpuIndexHeartbeatKey   = "run.ai/test-annotator-heartbeat"
		annotatorPollInterval  = 100 * time.Millisecond
		annotatorGPUIndexValue = "0"
	)

	var (
		testNamespace  *corev1.Namespace
		testDepartment *schedulingv2.Queue
		testQueue      *schedulingv2.Queue
		nodes          []*corev1.Node

		backgroundCtx context.Context
		cancel        context.CancelFunc
	)

	type gpuGroupState struct {
		gpuGroup       string
		nodeName       string
		reservationPod *corev1.Pod
		fractionPod    *corev1.Pod
		bindRequest    *kaiv1alpha2.BindRequest
	}

	var groups []gpuGroupState

	startReservationPodAnnotator := func(ctx context.Context, c client.Client) {
		ticker := time.NewTicker(annotatorPollInterval)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					var reservationPods corev1.PodList
					err := c.List(ctx, &reservationPods,
						client.InNamespace(constants.DefaultResourceReservationName),
						client.HasLabels{constants.GPUGroup},
					)
					if err != nil {
						GinkgoWriter.Printf("reservation pod annotator list error: %v\n", err)
						continue
					}

					for i := range reservationPods.Items {
						pod := reservationPods.Items[i]
						updated := pod.DeepCopy()
						if updated.Annotations == nil {
							updated.Annotations = map[string]string{}
						}

						needsPatch := false
						if updated.Annotations[gpuIndexAnnotationKey] == "" {
							updated.Annotations[gpuIndexAnnotationKey] = annotatorGPUIndexValue
							needsPatch = true
						} else {
							updated.Annotations[gpuIndexHeartbeatKey] = time.Now().UTC().Format(time.RFC3339Nano)
							needsPatch = true
						}

						if needsPatch {
							if err := c.Patch(ctx, updated, client.MergeFrom(&pod)); err != nil {
								GinkgoWriter.Printf("reservation pod annotator patch error: %v\n", err)
							}
						}
					}
				}
			}
		}()
	}

	BeforeEach(func(ctx context.Context) {
		testNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-" + randomstring.HumanFriendlyEnglishString(10),
			},
		}
		Expect(ctrlClient.Create(ctx, testNamespace)).To(Succeed())

		testDepartment = utils.CreateQueueObject("test-department", "")
		Expect(ctrlClient.Create(ctx, testDepartment)).To(Succeed())

		testQueue = utils.CreateQueueObject("test-queue", testDepartment.Name)
		Expect(ctrlClient.Create(ctx, testQueue)).To(Succeed())

		nodes = make([]*corev1.Node, numNodes)
		for i := range numNodes {
			nodeCfg := utils.DefaultNodeConfig(fmt.Sprintf("scale-node-%d", i))
			nodeCfg.GPUs = gpusPerNode
			nodes[i] = utils.CreateNodeObject(ctx, ctrlClient, nodeCfg)
			Expect(ctrlClient.Create(ctx, nodes[i])).To(Succeed())
		}

		// Ensure the reservation namespace exists
		reservationNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: constants.DefaultResourceReservationName},
		}
		err := ctrlClient.Create(ctx, reservationNs)
		if err != nil {
			Expect(client.IgnoreAlreadyExists(err)).To(Succeed())
		}

		backgroundCtx, cancel = context.WithCancel(context.Background())

		// Build GPU group state for all nodes × GPUs
		groups = make([]gpuGroupState, 0, numNodes*gpusPerNode)
		for nodeIdx := range numNodes {
			for gpuIdx := range gpusPerNode {
				gpuGroup := fmt.Sprintf("%s-gpu%d-group", nodes[nodeIdx].Name, gpuIdx)
				groups = append(groups, gpuGroupState{
					gpuGroup: gpuGroup,
					nodeName: nodes[nodeIdx].Name,
				})
			}
		}
	})

	AfterEach(func(ctx context.Context) {
		cancel()

		// Clean up reservation pods
		_ = ctrlClient.DeleteAllOf(ctx, &corev1.Pod{},
			client.InNamespace(constants.DefaultResourceReservationName))

		// Clean up test resources
		if testNamespace != nil {
			err := utils.DeleteAllInNamespace(ctx, ctrlClient, testNamespace.Name,
				&corev1.Pod{},
				&corev1.ConfigMap{},
				&kaiv1alpha2.BindRequest{},
			)
			Expect(err).NotTo(HaveOccurred())
		}

		for _, node := range nodes {
			_ = ctrlClient.Delete(ctx, node)
		}
		if testQueue != nil {
			_ = ctrlClient.Delete(ctx, testQueue)
		}
		if testDepartment != nil {
			_ = ctrlClient.Delete(ctx, testDepartment)
		}
	})

	// Pre-create reservation pods and fraction BindRequests, then let the
	// binder controller run. The controller's SyncForNode (called at the start
	// of each BindRequest reconcile) will discover reservation pods with no
	// matching labeled fraction pods and delete them.
	It("should preserve reservation pods when active BindRequests exist", func(ctx context.Context) {
		// Step 1: Pre-create reservation pods for all 32 GPU groups.
		// These simulate the state after ReserveGpuDevice created them.
		for i := range groups {
			g := &groups[i]
			g.reservationPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("gpu-reservation-%s-%d", g.nodeName, i),
					Namespace: constants.DefaultResourceReservationName,
					Labels: map[string]string{
						constants.AppLabelName: constants.DefaultResourceReservationName,
						constants.GPUGroup:     g.gpuGroup,
					},
				},
				Spec: corev1.PodSpec{
					NodeName:           g.nodeName,
					ServiceAccountName: constants.DefaultResourceReservationName,
					Containers: []corev1.Container{
						{Name: "resource-reservation", Image: "test-image"},
					},
				},
			}
			Expect(ctrlClient.Create(ctx, g.reservationPod)).To(Succeed())
		}

		// Step 2: Create fraction pods WITHOUT GPU group labels.
		// This simulates the window where ReserveGpuDevice has created the
		// reservation pod but hasn't yet labeled the fraction pod.
		for i := range groups {
			g := &groups[i]
			g.fractionPod = utils.CreatePodObject(
				testNamespace.Name,
				fmt.Sprintf("fraction-pod-%d", i),
				corev1.ResourceRequirements{},
			)
			if g.fractionPod.Annotations == nil {
				g.fractionPod.Annotations = map[string]string{}
			}
			g.fractionPod.Annotations[constants.GpuSharingConfigMapAnnotation] =
				fmt.Sprintf("%s-shared-gpu", g.fractionPod.Name)
			Expect(ctrlClient.Create(ctx, g.fractionPod)).To(Succeed())

			configMapPrefix := g.fractionPod.Annotations[constants.GpuSharingConfigMapAnnotation]
			capabilitiesConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-0", configMapPrefix),
					Namespace: testNamespace.Name,
				},
				Data: map[string]string{},
			}
			Expect(ctrlClient.Create(ctx, capabilitiesConfigMap)).To(Succeed())

			directEnvConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-0-evar", configMapPrefix),
					Namespace: testNamespace.Name,
				},
				Data: map[string]string{},
			}
			Expect(ctrlClient.Create(ctx, directEnvConfigMap)).To(Succeed())
		}

		// Step 3: Create BindRequests for all fraction pods.
		// The binder will reconcile these, calling SyncForNode which
		// triggers the reservation pod cleanup logic.
		for i := range groups {
			g := &groups[i]
			g.bindRequest = &kaiv1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("scale-bind-%d", i),
					Namespace: testNamespace.Name,
				},
				Spec: kaiv1alpha2.BindRequestSpec{
					PodName:              g.fractionPod.Name,
					SelectedNode:         g.nodeName,
					ReceivedResourceType: "Fraction",
					SelectedGPUGroups:    []string{g.gpuGroup},
					ReceivedGPU: &kaiv1alpha2.ReceivedGPU{
						Count:   1,
						Portion: "0.5",
					},
				},
			}
			Expect(ctrlClient.Create(ctx, g.bindRequest)).To(Succeed())
		}

		startReservationPodAnnotator(backgroundCtx, ctrlClient)
		err := binder.RunBinder(cfg, backgroundCtx)
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(1 * time.Second)

		// Step 5: Check how many reservation pods survived.
		var reservationPods corev1.PodList
		Expect(ctrlClient.List(ctx, &reservationPods,
			client.InNamespace(constants.DefaultResourceReservationName),
			client.HasLabels{constants.GPUGroup},
		)).To(Succeed())

		survivedCount := len(reservationPods.Items)
		totalGroups := len(groups)

		GinkgoWriter.Printf("\n=== Scale Race Results ===\n")
		GinkgoWriter.Printf("Total GPU groups: %d\n", totalGroups)
		GinkgoWriter.Printf("Reservation pods survived: %d\n", survivedCount)
		GinkgoWriter.Printf("Reservation pods deleted:  %d\n", totalGroups-survivedCount)

		if survivedCount < totalGroups {
			deletedGroups := []string{}
			survivingGroups := map[string]bool{}
			for _, pod := range reservationPods.Items {
				survivingGroups[pod.Labels[constants.GPUGroup]] = true
			}
			for _, g := range groups {
				if !survivingGroups[g.gpuGroup] {
					deletedGroups = append(deletedGroups, g.gpuGroup)
				}
			}
			GinkgoWriter.Printf("Deleted groups: %v\n", deletedGroups)
		}

		// Without the fix: SyncForNode sees reservation pods but no labeled
		// fraction pods (cache lag simulation) → deletes reservation pods.
		// With the fix: hasActiveBindRequestsForGpuGroup prevents deletion.
		Expect(survivedCount).To(Equal(totalGroups),
			"Expected all %d reservation pods to survive, but %d were deleted. "+
				"This indicates the race condition where SyncForGpuGroup deletes "+
				"reservation pods despite active BindRequests.",
			totalGroups, totalGroups-survivedCount)
	})
})
