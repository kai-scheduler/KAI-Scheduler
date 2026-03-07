// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package env_tests

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xyproto/randomstring"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	kaiv1alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v1alpha2"
	schedulingv2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/env-tests/queuecontroller"
	"github.com/kai-scheduler/kai-scheduler/pkg/env-tests/utils"
)

var _ = Describe("QueueController", Ordered, func() {
	// Define a timeout for eventually assertions
	const interval = time.Millisecond * 10
	const defaultTimeout = interval * 200

	var (
		testNamespace  *corev1.Namespace
		testDepartment *schedulingv2.Queue
		testQueue      *schedulingv2.Queue
		testNode       *corev1.Node

		backgroundCtx context.Context
		cancel        context.CancelFunc
	)

	BeforeEach(func(ctx context.Context) {
		// Create a test namespace
		testNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-" + randomstring.HumanFriendlyEnglishString(10),
			},
		}
		Expect(ctrlClient.Create(ctx, testNamespace)).To(Succeed())

		testDepartment = utils.CreateQueueObject("test-department", "")
		Expect(ctrlClient.Create(ctx, testDepartment)).To(Succeed(), "Failed to create test department")

		testQueue = utils.CreateQueueObject("test-queue", testDepartment.Name)
		Expect(ctrlClient.Create(ctx, testQueue)).To(Succeed(), "Failed to create test queue")

		testNode = utils.CreateNodeObject(ctx, ctrlClient, utils.DefaultNodeConfig("test-node"))
		Expect(ctrlClient.Create(ctx, testNode)).To(Succeed(), "Failed to create test node")

		backgroundCtx, cancel = context.WithCancel(context.Background())
		err := queuecontroller.RunQueueController(cfg, backgroundCtx)
		Expect(err).NotTo(HaveOccurred(), "Failed to run queuecontroller")
	})

	AfterEach(func(ctx context.Context) {
		Expect(ctrlClient.Delete(ctx, testDepartment)).To(Succeed(), "Failed to delete test department")
		Expect(ctrlClient.Delete(ctx, testQueue)).To(Succeed(), "Failed to delete test queue")
		Expect(ctrlClient.Delete(ctx, testNode)).To(Succeed(), "Failed to delete test node")

		err := utils.WaitForObjectDeletion(ctx, ctrlClient, testDepartment, defaultTimeout, interval)
		Expect(err).NotTo(HaveOccurred(), "Failed to wait for test department to be deleted")

		err = utils.WaitForObjectDeletion(ctx, ctrlClient, testQueue, defaultTimeout, interval)
		Expect(err).NotTo(HaveOccurred(), "Failed to wait for test queue to be deleted")

		err = utils.WaitForObjectDeletion(ctx, ctrlClient, testNode, defaultTimeout, interval)
		Expect(err).NotTo(HaveOccurred(), "Failed to wait for test node to be deleted")

		cancel()
	})

	Context("simple queue test", func() {
		AfterEach(func(ctx context.Context) {
			err := utils.DeleteAllInNamespace(ctx, ctrlClient, testNamespace.Name,
				&corev1.Pod{},
				&schedulingv2alpha2.PodGroup{},
				&resourcev1beta1.ResourceClaim{},
				&kaiv1alpha2.BindRequest{},
			)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete test resources")

			err = utils.WaitForNoObjectsInNamespace(ctx, ctrlClient, testNamespace.Name, defaultTimeout, interval,
				&corev1.PodList{},
				&schedulingv2alpha2.PodGroupList{},
				&resourcev1beta1.ResourceClaimList{},
				&kaiv1alpha2.BindRequestList{},
			)
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for test resources to be deleted")
		})

		It("Should update queue status", func(ctx context.Context) {
			testPod := utils.CreatePodObject(testNamespace.Name, "test-pod", corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})
			Expect(ctrlClient.Create(ctx, testPod)).To(Succeed(), "Failed to create test pod")

			Expect(utils.GroupPods(ctx, ctrlClient, utils.PodGroupConfig{
				QueueName:    testQueue.Name,
				PodgroupName: "test-podgroup",
				MinMember:    1,
			}, []*corev1.Pod{testPod})).To(Succeed(), "Failed to group pod")

			var podgroup schedulingv2alpha2.PodGroup
			Expect(ctrlClient.Get(ctx, client.ObjectKey{Name: "test-podgroup", Namespace: testNamespace.Name}, &podgroup)).To(Succeed(), "Failed to get test podgroup")

			podgroupCopy := podgroup.DeepCopy()
			podgroupCopy.Status.ResourcesStatus.Allocated = corev1.ResourceList{
				constants.NvidiaGpuResource: resource.MustParse("420"),
			}
			Expect(ctrlClient.Status().Patch(ctx, podgroupCopy, client.MergeFrom(&podgroup))).To(Succeed(), "Failed to patch test podgroup status")

			var queue schedulingv2.Queue
			// Wait for queue status allocated resources to be updated
			err := wait.PollUntilContextTimeout(ctx, interval, defaultTimeout, true, func(ctx context.Context) (bool, error) {
				err := ctrlClient.Get(ctx, client.ObjectKey{Name: "test-queue"}, &queue)
				if err != nil {
					return false, err
				}

				if queue.Status.Allocated == nil {
					return false, nil
				}

				if queue.Status.Allocated[constants.NvidiaGpuResource] != resource.MustParse("420") {
					return false, nil
				}

				return true, nil
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to wait for queue status allocated resources to be updated")
		})
	})
})
