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
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	schedulingv2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/env-tests/dynamicresource"
	"github.com/NVIDIA/KAI-scheduler/pkg/env-tests/scheduler"
)

var _ = Describe("Scheduler", Ordered, func() {
	// Define a timeout for eventually assertions
	const interval = time.Millisecond * 10
	const defaultTimeout = interval * 200

	var (
		testNamespace  *corev1.Namespace
		testDepartment *schedulingv2.Queue
		testQueue      *schedulingv2.Queue
		testNode       *corev1.Node
		stopCh         chan struct{}
	)

	BeforeEach(func(ctx context.Context) {
		// Create a test namespace
		testNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-" + randomstring.HumanFriendlyEnglishString(10),
			},
		}
		Expect(ctrlClient.Create(ctx, testNamespace)).To(Succeed())

		testDepartment = CreateQueueObject("test-department", "")
		Expect(ctrlClient.Create(ctx, testDepartment)).To(Succeed(), "Failed to create test department")

		testQueue = CreateQueueObject("test-queue", testDepartment.Name)
		Expect(ctrlClient.Create(ctx, testQueue)).To(Succeed(), "Failed to create test queue")

		testNode = CreateNodeObject(ctx, ctrlClient, DefaultNodeConfig("test-node"))
		Expect(ctrlClient.Create(ctx, testNode)).To(Succeed(), "Failed to create test node")

		stopCh = make(chan struct{})
		err := scheduler.RunScheduler(cfg, stopCh)
		Expect(err).NotTo(HaveOccurred(), "Failed to run scheduler")
	})

	AfterEach(func(ctx context.Context) {
		Expect(ctrlClient.Delete(ctx, testDepartment)).To(Succeed(), "Failed to delete test department")
		Expect(ctrlClient.Delete(ctx, testQueue)).To(Succeed(), "Failed to delete test queue")
		Expect(ctrlClient.Delete(ctx, testNode)).To(Succeed(), "Failed to delete test node")

		err := WaitForObjectDeletion(ctx, ctrlClient, testDepartment, defaultTimeout, interval)
		Expect(err).NotTo(HaveOccurred(), "Failed to wait for test department to be deleted")

		err = WaitForObjectDeletion(ctx, ctrlClient, testQueue, defaultTimeout, interval)
		Expect(err).NotTo(HaveOccurred(), "Failed to wait for test queue to be deleted")

		err = WaitForObjectDeletion(ctx, ctrlClient, testNode, defaultTimeout, interval)
		Expect(err).NotTo(HaveOccurred(), "Failed to wait for test node to be deleted")

		close(stopCh)
	})

	Context("simple pods", func() {
		AfterEach(func(ctx context.Context) {
			err := DeleteAllInNamespace(ctx, ctrlClient, testNamespace.Name,
				&corev1.Pod{},
				&schedulingv2alpha2.PodGroup{},
				&resourceapi.ResourceClaim{},
				&kaiv1alpha2.BindRequest{},
			)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete test resources")

			err = WaitForNoObjectsInNamespace(ctx, ctrlClient, testNamespace.Name, defaultTimeout, interval,
				&corev1.PodList{},
				&schedulingv2alpha2.PodGroupList{},
				&resourceapi.ResourceClaimList{},
				&kaiv1alpha2.BindRequestList{},
			)
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for test resources to be deleted")
		})

		It("Should create a bind request for the pod", func(ctx context.Context) {
			// Create your pod as before
			testPod := CreatePodObject(testNamespace.Name, "test-pod", corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					constants.GpuResource: resource.MustParse("1"),
				},
			})
			Expect(ctrlClient.Create(ctx, testPod)).To(Succeed(), "Failed to create test pod")

			err := GroupPods(ctx, ctrlClient, testQueue.Name, "test-podgroup", []*corev1.Pod{testPod}, 1)
			Expect(err).NotTo(HaveOccurred(), "Failed to group pods")

			// Wait for bind request to be created instead of checking pod binding
			err = WaitForPodScheduled(ctx, ctrlClient, testPod.Name, testNamespace.Name, defaultTimeout, interval)
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for test pod to be scheduled")

			// list bind requests
			bindRequests := &kaiv1alpha2.BindRequestList{}
			Expect(ctrlClient.List(ctx, bindRequests, client.InNamespace(testNamespace.Name))).
				To(Succeed(), "Failed to list bind requests")

			Expect(len(bindRequests.Items)).To(Equal(1), "Expected 1 bind request", PrettyPrintBindRequestList(bindRequests))
		})

		It("Should respect scheduling gates - simple pod", func(ctx context.Context) {
			// Create your pod as before
			testPod := CreatePodObject(testNamespace.Name, "test-pod", corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					constants.GpuResource: resource.MustParse("1"),
				},
			})
			testPod.Spec.SchedulingGates = []corev1.PodSchedulingGate{
				{
					Name: "gated",
				},
			}
			Expect(ctrlClient.Create(ctx, testPod)).To(Succeed(), "Failed to create test pod")

			err := GroupPods(ctx, ctrlClient, testQueue.Name, "test-podgroup", []*corev1.Pod{testPod}, 1)

			Expect(err).NotTo(HaveOccurred(), "Failed to group pods")
			err = wait.PollUntilContextTimeout(ctx, interval, defaultTimeout, true, func(ctx context.Context) (bool, error) {
				var events corev1.EventList
				Expect(ctrlClient.List(ctx, &events, client.InNamespace(testNamespace.Name))).
					To(Succeed(), "Failed to list events")

				for _, event := range events.Items {
					if event.InvolvedObject.Kind != "PodGroup" || event.Reason != "NotReady" {
						continue
					}

					return true, nil
				}

				return false, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for unready podgroup event")

			ctrlClient.Get(ctx, client.ObjectKey{Name: testPod.Name, Namespace: testNamespace.Name}, testPod)
			ungatedPod := testPod.DeepCopy()
			ungatedPod.Spec.SchedulingGates = nil
			patch := client.MergeFrom(testPod)
			Expect(ctrlClient.Patch(ctx, ungatedPod, patch)).To(Succeed(), "Failed to remove scheduling gates from test pod")

			err = WaitForPodScheduled(ctx, ctrlClient, testPod.Name, testNamespace.Name, defaultTimeout, interval)
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for test pod to be scheduled")

			// list bind requests
			bindRequests := &kaiv1alpha2.BindRequestList{}
			Expect(ctrlClient.List(ctx, bindRequests, client.InNamespace(testNamespace.Name))).
				To(Succeed(), "Failed to list bind requests")

			Expect(len(bindRequests.Items)).To(Equal(1), "Expected 1 bind request", PrettyPrintBindRequestList(bindRequests))
		})

		It("Should respect scheduling gates - podgroup", func(ctx context.Context) {
			testPod1 := CreatePodObject(testNamespace.Name, "test-pod-1", corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					constants.GpuResource: resource.MustParse("1"),
				},
			})
			testPod1.Spec.SchedulingGates = []corev1.PodSchedulingGate{
				{
					Name: "gated",
				},
			}
			Expect(ctrlClient.Create(ctx, testPod1)).To(Succeed(), "Failed to create test pod")

			testPod2 := CreatePodObject(testNamespace.Name, "test-pod-2", corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					constants.GpuResource: resource.MustParse("1"),
				},
			})
			testPod2.Spec.SchedulingGates = []corev1.PodSchedulingGate{
				{
					Name: "gated",
				},
			}
			Expect(ctrlClient.Create(ctx, testPod2)).To(Succeed(), "Failed to create test pod")

			err := GroupPods(ctx, ctrlClient, testQueue.Name, "test-podgroup", []*corev1.Pod{testPod1, testPod2}, 1)
			Expect(err).NotTo(HaveOccurred(), "Failed to group pods")

			err = wait.PollUntilContextTimeout(ctx, interval, defaultTimeout, true, func(ctx context.Context) (bool, error) {
				var events corev1.EventList
				Expect(ctrlClient.List(ctx, &events, client.InNamespace(testNamespace.Name))).
					To(Succeed(), "Failed to list events")

				for _, event := range events.Items {
					if event.InvolvedObject.Kind != "PodGroup" || event.Reason != "NotReady" {
						continue
					}

					return true, nil
				}

				return false, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for unready podgroup event")

			ctrlClient.Get(ctx, client.ObjectKey{Name: testPod1.Name, Namespace: testNamespace.Name}, testPod1)
			ungatedPod := testPod1.DeepCopy()
			ungatedPod.Spec.SchedulingGates = nil
			patch := client.MergeFrom(testPod1)
			Expect(ctrlClient.Patch(ctx, ungatedPod, patch)).To(Succeed(), "Failed to remove scheduling gates from test pod")

			err = WaitForPodScheduled(ctx, ctrlClient, testPod1.Name, testNamespace.Name, defaultTimeout, interval)
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for test pod to be scheduled")

			ctrlClient.Get(ctx, client.ObjectKey{Name: testPod2.Name, Namespace: testNamespace.Name}, testPod2)
			// expect testPod2 to not be scheduled
			Expect(testPod2.Status.Phase).To(Equal(corev1.PodPending), "Expected test pod 2 to not be scheduled")

			// ungate testPod2
			ungatedPod2 := testPod2.DeepCopy()
			ungatedPod2.Spec.SchedulingGates = nil
			patch2 := client.MergeFrom(testPod2)
			Expect(ctrlClient.Patch(ctx, ungatedPod2, patch2)).To(Succeed(), "Failed to remove scheduling gates from test pod")

			err = WaitForPodScheduled(ctx, ctrlClient, testPod2.Name, testNamespace.Name, defaultTimeout, interval)
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for test pod to be scheduled")
		})

		It("Should retry updating podgroup status after failure", func(ctx context.Context) {
			// Create your pod as before
			testPod := CreatePodObject(testNamespace.Name, "test-pod", corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					constants.GpuResource: resource.MustParse("999"),
				},
			})
			Expect(ctrlClient.Create(ctx, testPod)).To(Succeed(), "Failed to create test pod")

			// add a webhook to fail updating podgroups
			webhook := admissionv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fail-podgroup-update",
					Namespace: testNamespace.Name,
				},
				Webhooks: []admissionv1.ValidatingWebhook{
					{
						Name: "fail.podgroup.update",
						Rules: []admissionv1.RuleWithOperations{
							{
								Operations: []admissionv1.OperationType{admissionv1.Update},
								Rule: admissionv1.Rule{
									APIGroups:   []string{"scheduling.run.ai"},
									APIVersions: []string{"v2alpha2"},
									Resources:   []string{"podgroups", "podgroups/status"},
									Scope:       func() *admissionv1.ScopeType { s := admissionv1.NamespacedScope; return &s }(),
								},
							},
						},
						TimeoutSeconds: ptr.To(int32(1)),
						SideEffects:    func() *admissionv1.SideEffectClass { f := admissionv1.SideEffectClassNone; return &f }(),
						ClientConfig: admissionv1.WebhookClientConfig{
							Service: &admissionv1.ServiceReference{
								Name:      "fail-podgroup-update",
								Namespace: testNamespace.Name,
								Path:      ptr.To("/validate-scheduling-run-ai-v2alpha2-podgroup"),
							},
						},
						AdmissionReviewVersions: []string{"v1"},
					},
				},
			}
			Expect(ctrlClient.Create(ctx, &webhook)).To(Succeed(), "Failed to create test webhook")
			defer func() {
				// Make sure to delete the webhook even if the test fails
				_ = ctrlClient.Delete(ctx, &webhook)
			}()

			err := GroupPods(ctx, ctrlClient, testQueue.Name, "test-podgroup", []*corev1.Pod{testPod}, 1)
			Expect(err).NotTo(HaveOccurred(), "Failed to group pods")

			// validate that editing the podgroup status fails
			var podgroup schedulingv2alpha2.PodGroup
			Expect(ctrlClient.Get(ctx, client.ObjectKey{Name: "test-podgroup", Namespace: testNamespace.Name}, &podgroup)).
				To(Succeed(), "Failed to get test podgroup")
			podgroup.Status.Pending = 42
			err = ctrlClient.Status().Update(ctx, &podgroup)
			Expect(err).To(HaveOccurred(), "Expected to fail to update podgroup status")

			// Verify that events are published
			deadlineCtx, deadlineCancel := context.WithTimeout(ctx, defaultTimeout)
			defer deadlineCancel()
			err = wait.PollUntilContextTimeout(deadlineCtx, interval, defaultTimeout, true, func(ctx context.Context) (bool, error) {
				var events corev1.EventList
				err := ctrlClient.List(ctx, &events, client.InNamespace(testNamespace.Name))
				if err != nil {
					return false, err
				}

				for _, event := range events.Items {
					if event.InvolvedObject.Kind != "PodGroup" {
						continue
					}

					return true, nil
				}

				return false, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for unready podgroup event")

			// Verify that scheduling conditions were not updated
			Expect(ctrlClient.Get(ctx, client.ObjectKey{Name: "test-podgroup", Namespace: testNamespace.Name}, &podgroup)).
				To(Succeed(), "Failed to get test podgroup")
			Expect(len(podgroup.Status.SchedulingConditions)).To(BeZero(), "Expected no scheduling conditions", podgroup.Status.SchedulingConditions)

			// Delete webhook to allow the podgroup status to be updated
			Expect(ctrlClient.Delete(ctx, &webhook)).To(Succeed(), "Failed to delete test webhook")

			// Verify that the scheduling conditions are updated
			deadlineCtx, deadlineCancel = context.WithTimeout(ctx, defaultTimeout)
			defer deadlineCancel()
			err = wait.PollUntilContextTimeout(deadlineCtx, interval, defaultTimeout, true, func(ctx context.Context) (bool, error) {
				err := ctrlClient.Get(deadlineCtx, client.ObjectKey{Name: "test-podgroup", Namespace: testNamespace.Name}, &podgroup)
				if err != nil {
					return false, err
				}

				if len(podgroup.Status.SchedulingConditions) > 0 {
					return true, nil
				}

				return false, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for podgroup scheduling conditions to be updated")
		})
	})

	Context("dynamic resource", Ordered, func() {
		const (
			deviceClassName = "gpu.nvidia.com"
			driverName      = "nvidia"
			deviceNum       = 8
		)

		BeforeAll(func(ctx context.Context) {
			deviceClass := dynamicresource.CreateDeviceClass(deviceClassName)
			Expect(ctrlClient.Create(ctx, deviceClass)).To(Succeed(), "Failed to create test device class")

			nodeResourceSlice := dynamicresource.CreateNodeResourceSlice(
				"test-node-resource-slice", driverName, testNode.Name, deviceNum)
			Expect(ctrlClient.Create(ctx, nodeResourceSlice)).
				To(Succeed(), "Failed to create test node resource slice")
		})

		AfterEach(func(ctx context.Context) {
			err := DeleteAllInNamespace(ctx, ctrlClient, testNamespace.Name,
				&corev1.Pod{},
				&schedulingv2alpha2.PodGroup{},
				&resourceapi.ResourceClaim{},
				&kaiv1alpha2.BindRequest{},
			)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete test resources")

			err = WaitForNoObjectsInNamespace(ctx, ctrlClient, testNamespace.Name, defaultTimeout, interval,
				&corev1.PodList{},
				&schedulingv2alpha2.PodGroupList{},
				&resourceapi.ResourceClaimList{},
				&kaiv1alpha2.BindRequestList{},
			)
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for test resources to be deleted")
		})

		It("Should create a bind request for the pod with DeviceAllocationModeAll resource claim", func(ctx context.Context) {
			resourceClaim := dynamicresource.CreateResourceClaim(
				"test-resource-claim", testNamespace.Name,
				dynamicresource.DeviceRequest{Class: deviceClassName, Count: -1},
			)
			err := ctrlClient.Create(ctx, resourceClaim)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test resource claim")

			testPod := CreatePodObject(testNamespace.Name, "test-pod", corev1.ResourceRequirements{})
			dynamicresource.UseClaim(testPod, resourceClaim)
			Expect(ctrlClient.Create(ctx, testPod)).To(Succeed(), "Failed to create test pod")

			err = GroupPods(ctx, ctrlClient, testQueue.Name, "test-podgroup", []*corev1.Pod{testPod}, 1)
			Expect(err).NotTo(HaveOccurred(), "Failed to group pods")

			err = WaitForPodScheduled(ctx, ctrlClient, testPod.Name, testNamespace.Name, defaultTimeout, interval)
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for test pod to be scheduled")

			// list bind requests
			bindRequests := &kaiv1alpha2.BindRequestList{}
			Expect(ctrlClient.List(ctx, bindRequests, client.InNamespace(testNamespace.Name))).
				To(Succeed(), "Failed to list bind requests")

			Expect(len(bindRequests.Items)).To(Equal(1), "Expected 1 bind request", PrettyPrintBindRequestList(bindRequests))
		})

		It("Should schedule multiple pods with ExactCount resource claims", func(ctx context.Context) {
			var createdPods []*corev1.Pod
			for i := range 20 {
				resourceClaim := dynamicresource.CreateResourceClaim(
					fmt.Sprintf("test-resource-claim-%d", i),
					testNamespace.Name,
					dynamicresource.DeviceRequest{
						Class: deviceClassName,
						Count: 1,
					},
				)
				err := ctrlClient.Create(ctx, resourceClaim)
				Expect(err).NotTo(HaveOccurred(), "Failed to create test resource claim")

				testPod := CreatePodObject(testNamespace.Name, fmt.Sprintf("test-pod-%d", i), corev1.ResourceRequirements{})
				dynamicresource.UseClaim(testPod, resourceClaim)
				Expect(ctrlClient.Create(ctx, testPod)).To(Succeed(), "Failed to create test pod")
				createdPods = append(createdPods, testPod)

				err = GroupPods(ctx, ctrlClient, testQueue.Name, fmt.Sprintf("test-podgroup-%d", i), []*corev1.Pod{testPod}, 1)
				Expect(err).NotTo(HaveOccurred(), "Failed to group pods")
			}

			podMap := map[string]bool{}
			for _, pod := range createdPods {
				err := WaitForPodScheduled(ctx, ctrlClient, pod.Name, testNamespace.Name, defaultTimeout, interval)
				if err != nil {
					continue
				}
				podMap[pod.Name] = true
			}

			Expect(podMap).To(HaveLen(deviceNum), "Expected exactly %d pods to be scheduled", deviceNum)
		})
	})
})
