/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package ray

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	v1apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait/watcher"
)

var _ = Describe("Ray integration", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		skipIfRayNotInstalled(ctx, testCtx)

		Expect(rayv1.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	Context("RayJob submission", func() {
		AfterEach(func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			Expect(testCtx.ControllerClient.DeleteAllOf(ctx, &rayv1.RayJob{}, client.InNamespace(namespace))).To(Succeed())
			Expect(testCtx.ControllerClient.DeleteAllOf(ctx, &v1.ConfigMap{}, client.InNamespace(namespace))).To(Succeed())

			testCtx.TestContextCleanup(ctx)
		})
		It("should run the pods of the RayJob", func(ctx context.Context) {
			rayJob, configMap := createExampleRayJob(testCtx.Queues[0], v1.ResourceRequirements{}, v1.ResourceRequirements{}, 1)
			Expect(testCtx.ControllerClient.Create(ctx, configMap)).To(Succeed())
			Expect(testCtx.ControllerClient.Create(ctx, rayJob)).To(Succeed())

			rayCluster := waitForRayCluster(ctx, testCtx, rayJob)
			rayPods := waitForRayPodsCreation(ctx, testCtx, rayCluster, 2)

			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, rayJob.Namespace, rayPods)
		})

		It("should be able to submit even if there are not enough resources available", func(ctx context.Context) {
			limitedQueueName := "limited-" + utils.GenerateRandomK8sName(10)
			parentQueue := testCtx.Queues[1]
			limitedQueue := queue.CreateQueueObject(limitedQueueName, parentQueue.Name)
			limitedQueue.Spec.Resources.CPU.Quota = 2000
			limitedQueue.Spec.Resources.CPU.Limit = 2000
			testCtx.AddQueues(ctx, []*v2.Queue{limitedQueue})

			rayJob, configMap := createExampleRayJob(
				limitedQueue, v1.ResourceRequirements{}, v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("1"),
					},
					Requests: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("1"),
					},
				}, 3)
			Expect(testCtx.ControllerClient.Create(ctx, configMap)).To(Succeed())
			Expect(testCtx.ControllerClient.Create(ctx, rayJob)).To(Succeed())

			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, rayJob)).To(Succeed())
				Expect(testCtx.ControllerClient.Delete(ctx, configMap)).To(Succeed())
			}()

			rayCluster := waitForRayCluster(ctx, testCtx, rayJob)
			rayPods := waitForRayPodsCreation(ctx, testCtx, rayCluster, 4)
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, rayJob.Namespace, rayPods, 3)
		})

		It("should run an elastic RayJob", func(ctx context.Context) {
			rayJob, configMap := createExampleRayJob(testCtx.Queues[0], v1.ResourceRequirements{}, v1.ResourceRequirements{}, 1)
			rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas = ptr.To(int32(10))
			rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].MinReplicas = ptr.To(int32(1))
			Expect(testCtx.ControllerClient.Create(ctx, configMap)).To(Succeed())
			Expect(testCtx.ControllerClient.Create(ctx, rayJob)).To(Succeed())

			rayCluster := waitForRayCluster(ctx, testCtx, rayJob)
			rayPods := waitForRayPodsCreation(ctx, testCtx, rayCluster, 2)

			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, rayJob.Namespace, rayPods)
		})

		It("should not run an elastic RayJob if not all subgroups are satisfied", func(ctx context.Context) {
			rayJob, configMap := createExampleRayJob(testCtx.Queues[0], v1.ResourceRequirements{}, v1.ResourceRequirements{}, 1)

			workerGroupA := rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0]
			workerGroupA.GroupName = "worker-group-a"
			workerGroupA.Replicas = ptr.To(int32(5))
			workerGroupA.MinReplicas = ptr.To(int32(1))
			workerGroupA.MaxReplicas = ptr.To(int32(5))

			workerGroupB := workerGroupA.DeepCopy()
			workerGroupB.GroupName = "worker-group-b"
			workerGroupB.Replicas = ptr.To(int32(5))
			workerGroupB.MinReplicas = ptr.To(int32(1))
			workerGroupB.MaxReplicas = ptr.To(int32(5))
			workerGroupB.Template.Spec.Containers[0].Resources = v1.ResourceRequirements{
				Requests: v1.ResourceList{
					constants.GpuResource: resource.MustParse("99"),
				},
				Limits: v1.ResourceList{
					constants.GpuResource: resource.MustParse("99"),
				},
			}

			rayJob.Spec.RayClusterSpec.WorkerGroupSpecs = []rayv1.WorkerGroupSpec{workerGroupA, *workerGroupB}

			Expect(testCtx.ControllerClient.Create(ctx, configMap)).To(Succeed())
			Expect(testCtx.ControllerClient.Create(ctx, rayJob)).To(Succeed())

			rayCluster := waitForRayCluster(ctx, testCtx, rayJob)
			rayPods := waitForRayPodsCreation(ctx, testCtx, rayCluster, 2)

			wait.ForAtLeastNPodsUnschedulable(ctx, testCtx.ControllerClient, rayJob.Namespace, rayPods, 2)

			// Get the PodGroup name from one of the ray pods and verify it has 3 subgroups
			var updatedPod v1.Pod
			Expect(testCtx.ControllerClient.Get(ctx, client.ObjectKeyFromObject(rayPods[0]), &updatedPod)).To(Succeed())
			podGroupName := updatedPod.Annotations[constants.PodGroupAnnotationForPod]
			Expect(podGroupName).NotTo(BeEmpty(), "PodGroup annotation not found on pod")

			podGroup, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(rayJob.Namespace).Get(
				ctx, podGroupName, metav1.GetOptions{})
			Expect(err).To(Succeed())
			Expect(len(podGroup.Spec.SubGroups)).To(Equal(3),
				"Expected 3 subgroups (1 head + 2 worker groups)")
		})
	})
})

func waitForRayCluster(ctx context.Context, testCtx *testcontext.TestContext, rayJob *rayv1.RayJob) *rayv1.RayCluster {
	condition := func(event watch.Event) bool {
		rayClusterList, ok := event.Object.(*rayv1.RayClusterList)
		if !ok {
			return false
		}
		if len(rayClusterList.Items) != 1 {
			return false
		}
		rayCluster := rayClusterList.Items[0]
		if rayCluster.OwnerReferences[0].Name != rayJob.Name {
			return false
		}
		return true
	}

	rayWatcher := watcher.NewGenericWatcher[rayv1.RayClusterList](testCtx.ControllerClient,
		condition, client.InNamespace(rayJob.Namespace))
	if !watcher.ForEvent(ctx, testCtx.ControllerClient, rayWatcher) {
		Fail("Failed to wait for ray cluster")
	}

	rayClusters := &rayv1.RayClusterList{}
	err := testCtx.ControllerClient.List(ctx, rayClusters, client.InNamespace(rayJob.Namespace))
	Expect(err).NotTo(HaveOccurred())
	return &rayClusters.Items[0]
}

func waitForRayPodsCreation(ctx context.Context, testCtx *testcontext.TestContext, rayCluster *rayv1.RayCluster,
	expectedCount int) []*v1.Pod {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"ray.io/cluster": rayCluster.Name,
		},
	}
	wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, labelSelector, expectedCount)

	rayPods := &v1.PodList{}
	err := testCtx.ControllerClient.List(ctx, rayPods, client.InNamespace(rayCluster.Namespace))
	Expect(err).NotTo(HaveOccurred())

	var pods []*v1.Pod
	for index := range rayPods.Items {
		pods = append(pods, &rayPods.Items[index])
	}

	return pods
}

func createExampleRayJob(
	jobQueue *v2.Queue, headResources, workerResources v1.ResourceRequirements, replicas int32,
) (*rayv1.RayJob, *v1.ConfigMap) {
	runtimeEnvYaml := `
    pip:
      - requests==2.26.0
      - pendulum==2.1.2
    env_vars:
      counter_name: "test_counter"`
	namespace := queue.GetConnectedNamespaceToQueue(jobQueue)

	rayjob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-ray-job-" + utils.GenerateRandomK8sName(10),
			Namespace: namespace,
		},
		Spec: rayv1.RayJobSpec{
			Entrypoint:     "sleep infinity",
			RuntimeEnvYAML: runtimeEnvYaml,
			RayClusterSpec: &rayv1.RayClusterSpec{
				RayVersion: "2.4.0",
				HeadGroupSpec: rayv1.HeadGroupSpec{
					RayStartParams: map[string]string{
						"dashboard-host": "0.0.0.0",
					},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								constants.AppLabelName: "engine-e2e",
								"kai.scheduler/queue":  jobQueue.Name,
							},
						},
						Spec: v1.PodSpec{
							SchedulerName: constant.SchedulerName,
							Containers: []v1.Container{
								{
									Name:      "ray-head",
									Image:     "rayproject/ray:2.4.0",
									Resources: headResources,
									Ports: []v1.ContainerPort{
										{
											Name:          "gcs-server",
											ContainerPort: 6379,
										},
										{
											Name:          "dashboard",
											ContainerPort: 8265,
										},
										{
											Name:          "client",
											ContainerPort: 10001,
										},
										{
											Name:          "serve",
											ContainerPort: 8000,
										},
									},
									VolumeMounts: []v1.VolumeMount{
										{
											Name:      "code-sample",
											MountPath: "/home/ray/samples",
										},
									},
								},
							},
							Volumes: []v1.Volume{
								{
									Name: "code-sample",
									VolumeSource: v1.VolumeSource{
										ConfigMap: &v1.ConfigMapVolumeSource{
											LocalObjectReference: v1.LocalObjectReference{
												Name: "ray-job-code-sample",
											},
											Items: []v1.KeyToPath{
												{
													Key:  "sample_code.py",
													Path: "sample_code.py",
												},
											},
										},
									},
								},
							},
						},
					},
				},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						RayStartParams: map[string]string{},
						Replicas:       &replicas,
						MinReplicas:    ptr.To(int32(1)),
						MaxReplicas:    ptr.To(int32(5)),
						GroupName:      "worker-group-1",
						Template: v1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									constants.AppLabelName: "engine-e2e",
									"kai.scheduler/queue":  jobQueue.Name,
								},
							},
							Spec: v1.PodSpec{
								SchedulerName: constant.SchedulerName,
								Containers: []v1.Container{
									{
										Name:      "ray-worker",
										Image:     "rayproject/ray:2.4.0",
										Resources: workerResources,
										Lifecycle: &v1.Lifecycle{
											PreStop: &v1.LifecycleHandler{
												Exec: &v1.ExecAction{
													Command: []string{
														"/bin/sh",
														"-c",
														"ray stop",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ray-job-code-sample",
			Namespace: namespace,
		},
		Data: map[string]string{
			"sample_code.py": `
import ray
import os
import requests

ray.init()

@ray.remote
class Counter:
	def __init__(self):
		# Used to verify runtimeEnv
		self.name = os.getenv("counter_name")
		self.counter = 0

	def inc(self):
		self.counter += 1

	def get_counter(self):
		return "{} got {}".format(self.name, self.counter)

counter = Counter.remote()

for _ in range(5):
	ray.get(counter.inc.remote())
	print(ray.get(counter.get_counter.remote()))

print(requests.__version__)
			`,
		},
	}

	return rayjob, configMap
}

func skipIfRayNotInstalled(ctx context.Context, testCtx *testcontext.TestContext) {
	Expect(apiextensionsv1.AddToScheme(testCtx.ControllerClient.Scheme())).To(Succeed())

	for _, crdBaseName := range []string{"rayjobs", "rayclusters"} {
		crdName := fmt.Sprintf("%s.%s", crdBaseName, rayv1.GroupVersion.Group)
		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, crdName, rayv1.GroupVersion.Version)
	}

	var rayOperatorDeployment v1apps.Deployment
	err := testCtx.ControllerClient.Get(
		ctx, client.ObjectKey{Name: "kuberay-operator", Namespace: "ray"}, &rayOperatorDeployment)
	if err != nil {
		Skip("Ray is not installed: missing operator pod: " + err.Error())
	}
}
