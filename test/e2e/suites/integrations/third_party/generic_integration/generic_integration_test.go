/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package generic_integration

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"

	v1 "k8s.io/api/core/v1"
	apiextension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Generic podgrouper integration", Ordered, func() {
	var (
		testCtx   *testcontext.TestContext
		namespace string
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
		namespace = queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	It("Non integrated third party -  submit", func(ctx context.Context) {
		apiExtensionClient, err := apiextension.NewForConfig(testCtx.KubeConfig)
		Expect(err).NotTo(HaveOccurred())

		err = crd.CreateTestcrd(ctx, apiExtensionClient)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			crd.DeleteTestcrd(ctx, apiExtensionClient)
		}()

		crdOwnerName := utils.GenerateRandomK8sName(10)
		err = crd.CreateTestcrdInstance(ctx, testCtx, namespace, crdOwnerName)
		Expect(err).To(Succeed())
		crdOwnerInstance, err := crd.GetTestcrdInstance(ctx, testCtx, namespace, crdOwnerName)
		defer func() {
			err = crd.DeleteTestcrdInstance(ctx, testCtx, namespace, crdOwnerName)
			Expect(err).To(Succeed())
		}()
		Expect(err).NotTo(HaveOccurred())
		Expect(crdOwnerInstance.GetName()).To(Equal(crdOwnerName))
		Expect(crdOwnerInstance.GetNamespace()).To(Equal(namespace))

		pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
		pod.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: crd.TestcrdGroup + "/" + crd.TestcrdVersion,
				Kind:       crd.TestcrdKind,
				Name:       crdOwnerName,
				UID:        crdOwnerInstance.GetUID(),
			},
		}
		_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).To(Succeed())

		// wait for the pod group to be created
		wait.WaitForPodGroupsToBeReady(ctx, testCtx.ControllerClient, namespace, 1)
	})
})
