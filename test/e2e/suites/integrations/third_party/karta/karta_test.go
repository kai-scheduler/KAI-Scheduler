/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package karta

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	pgconstants "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait/watcher"
	"github.com/kai-scheduler/api/constants"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
	v2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

const (
	kartaCrdName    = "kartas.run.ai"
	kartaCrdVersion = "v1alpha1"

	customJobGroup    = "e2e.example.com"
	customJobVersion  = "v1"
	customJobKind     = "CustomJob"
	customJobPlural   = "customjobs"
	customJobSingular = "customjob"
	customJobCrdName  = customJobPlural + "." + customJobGroup

	customJobRoleLabel = "e2e.example.com/role"

	podGrouperDeploymentName = "pod-grouper"
)

//go:embed customjobs_crd.yaml customjob_karta.yaml podgrouper_access.yaml
var kartaFixtures embed.FS

var _ = Describe("Karta integration", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, kartaCrdName, kartaCrdVersion)
		Expect(kartav1alpha1.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())

		Expect(applyKartaFixture(ctx, testCtx, "customjobs_crd.yaml")).To(Succeed())
		Expect(applyKartaFixture(ctx, testCtx, "podgrouper_access.yaml")).To(Succeed())
		waitForCustomJobCrd(ctx, testCtx)
		Expect(wait.ForRolloutRestartDeployment(
			ctx, testCtx.ControllerClient,
			testconfig.GetConfig().SystemPodsNamespace,
			podGrouperDeploymentName,
		)).To(Succeed())

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
		Expect(deleteKartaFixture(ctx, testCtx, "podgrouper_access.yaml")).To(Succeed())
		Expect(deleteKartaFixture(ctx, testCtx, "customjobs_crd.yaml")).To(Succeed())
		Expect(wait.ForRolloutRestartDeployment(
			ctx, testCtx.ControllerClient,
			testconfig.GetConfig().SystemPodsNamespace,
			podGrouperDeploymentName,
		)).To(Succeed())
	})

	It("groups an unknown workload GVK using Karta gang scheduling instructions", func(ctx context.Context) {
		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		customJobName := "custom-job-" + utils.GenerateRandomK8sName(10)

		kt, err := loadKartaDefinition()
		Expect(err).To(Succeed())
		Expect(testCtx.ControllerClient.Create(ctx, kt)).To(Succeed())
		defer func() {
			Expect(testCtx.ControllerClient.Delete(ctx, kt)).To(Succeed())
		}()

		coordinatorReplicas := 1
		workerReplicas := 2

		customJob := createCustomJob(
			namespace, customJobName, testconfig.GetConfig().QueueLabelKey, testCtx.Queues[0].Name,
			coordinatorReplicas, workerReplicas,
		)
		Expect(testCtx.ControllerClient.Create(ctx, customJob)).To(Succeed())
		defer func() {
			Expect(testCtx.ControllerClient.Delete(ctx, customJob)).To(Succeed())
		}()

		pods := createCustomJobPods(ctx, testCtx, customJob, coordinatorReplicas, workerReplicas)
		wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

		expectedPodGroupName := fmt.Sprintf("%s-%s-customjob", pgconstants.PodGroupNamePrefix, customJob.GetUID())
		wait.WaitForPodGroupToExist(ctx, testCtx.ControllerClient, namespace, expectedPodGroupName)

		podGroup := &v2alpha2.PodGroup{}
		Expect(testCtx.ControllerClient.Get(ctx,
			runtimeClient.ObjectKey{Namespace: namespace, Name: expectedPodGroupName}, podGroup)).To(Succeed())
		Expect(podGroup.Spec.MinMember).To(BeNil())
		Expect(podGroup.Spec.MinSubGroup).To(Equal(ptr.To(int32(2))))
		Expect(podGroup.Spec.Queue).To(Equal(testCtx.Queues[0].Name))
		Expect(podGroup.Spec.SubGroups).To(ConsistOf(
			subGroupWithMinMember("coordinator", coordinatorReplicas),
			subGroupWithMinMember("worker", workerReplicas),
		))
		Expect(podGroup.OwnerReferences).To(ConsistOf(metav1.OwnerReference{
			APIVersion: customJobGroup + "/" + customJobVersion,
			Kind:       customJobKind,
			Name:       customJobName,
			UID:        customJob.GetUID(),
		}))

		Eventually(func(g Gomega) {
			for _, pod := range pods {
				updatedPod := &v1.Pod{}
				g.Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(pod), updatedPod)).To(Succeed())
				g.Expect(updatedPod.Annotations[constants.PodGroupAnnotationForPod]).To(Equal(expectedPodGroupName))
				g.Expect(updatedPod.Labels[constants.SubGroupLabelKey]).To(Equal(updatedPod.Labels[customJobRoleLabel]))
			}
		}).WithTimeout(watcher.FlowTimeout).WithPolling(time.Second).WithContext(ctx).Should(Succeed())
	})
})

func loadKartaDefinition() (*kartav1alpha1.Karta, error) {
	data, err := kartaFixtures.ReadFile("customjob_karta.yaml")
	if err != nil {
		return nil, err
	}

	kt := &kartav1alpha1.Karta{}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	if err = decoder.Decode(kt); err != nil {
		return nil, err
	}
	return kt, nil
}

func createCustomJob(
	namespace, name, queueLabelKey, queueName string,
	coordinatorReplicas, workerReplicas int,
) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": customJobGroup + "/" + customJobVersion,
		"kind":       customJobKind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]any{
				queueLabelKey: queueName,
			},
		},
		"spec": map[string]any{
			"coordinator": map[string]any{
				"replicas": int64(coordinatorReplicas),
				"template": map[string]any{
					"spec": map[string]any{},
				},
			},
			"worker": map[string]any{
				"replicas": int64(workerReplicas),
				"template": map[string]any{
					"spec": map[string]any{},
				},
			},
		},
	}}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   customJobGroup,
		Version: customJobVersion,
		Kind:    customJobKind,
	})
	return obj
}

func createCustomJobPods(
	ctx context.Context, testCtx *testcontext.TestContext, owner *unstructured.Unstructured,
	coordinatorReplicas, workerReplicas int,
) []*v1.Pod {
	pods := make([]*v1.Pod, 0, coordinatorReplicas+workerReplicas)
	pods = append(pods, createComponentPods(ctx, testCtx, owner, "coordinator", coordinatorReplicas)...)
	pods = append(pods, createComponentPods(ctx, testCtx, owner, "worker", workerReplicas)...)
	return pods
}

func createComponentPods(
	ctx context.Context, testCtx *testcontext.TestContext, owner *unstructured.Unstructured,
	componentName string, replicas int,
) []*v1.Pod {
	pods := make([]*v1.Pod, 0, replicas)
	for i := range replicas {
		pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
		pod.Name = fmt.Sprintf("%s-%s-%d", owner.GetName(), componentName, i)
		pod.Labels[customJobRoleLabel] = componentName
		pod.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: customJobGroup + "/" + customJobVersion,
				Kind:       customJobKind,
				Name:       owner.GetName(),
				UID:        owner.GetUID(),
			},
		}
		createdPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).To(Succeed())
		pods = append(pods, createdPod)
	}
	return pods
}

func subGroupWithMinMember(name string, minMember int) v2alpha2.SubGroup {
	return v2alpha2.SubGroup{
		Name:      name,
		MinMember: ptr.To(int32(minMember)),
	}
}

func waitForCustomJobCrd(ctx context.Context, testCtx *testcontext.TestContext) {
	Eventually(func(g Gomega) {
		crdObj := &unstructured.Unstructured{}
		crdObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "apiextensions.k8s.io",
			Version: "v1",
			Kind:    "CustomResourceDefinition",
		})
		g.Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKey{Name: customJobCrdName}, crdObj)).To(Succeed())
	}).WithTimeout(time.Minute).WithPolling(time.Second).WithContext(ctx).Should(Succeed())
}

func applyKartaFixture(ctx context.Context, testCtx *testcontext.TestContext, fileName string) error {
	return forEachKartaFixtureObject(fileName, func(obj *unstructured.Unstructured) error {
		return applyObject(ctx, testCtx, obj)
	})
}

func deleteKartaFixture(ctx context.Context, testCtx *testcontext.TestContext, fileName string) error {
	return forEachKartaFixtureObject(fileName, func(obj *unstructured.Unstructured) error {
		err := testCtx.ControllerClient.Delete(ctx, obj)
		return runtimeClient.IgnoreNotFound(err)
	})
}

func forEachKartaFixtureObject(fileName string, fn func(*unstructured.Unstructured) error) error {
	data, err := kartaFixtures.ReadFile(fileName)
	if err != nil {
		return err
	}
	data = bytes.ReplaceAll(data, []byte("${KAI_NAMESPACE}"), []byte(testconfig.GetConfig().SystemPodsNamespace))

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		obj := &unstructured.Unstructured{}
		err = decoder.Decode(obj)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if obj.Object == nil {
			continue
		}
		if err = fn(obj); err != nil {
			return err
		}
	}
}

func applyObject(ctx context.Context, testCtx *testcontext.TestContext, obj *unstructured.Unstructured) error {
	err := testCtx.ControllerClient.Create(ctx, obj)
	if err == nil || !errors.IsAlreadyExists(err) {
		return err
	}

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(obj.GroupVersionKind())
	key := runtimeClient.ObjectKey{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	if err = testCtx.ControllerClient.Get(ctx, key, current); err != nil {
		return err
	}

	obj.SetResourceVersion(current.GetResourceVersion())
	return testCtx.ControllerClient.Update(ctx, obj)
}
