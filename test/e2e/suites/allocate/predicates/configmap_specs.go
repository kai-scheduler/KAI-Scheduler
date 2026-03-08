/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package predicates

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeConfigMapSpecs() bool {
	return Describe("Config map scheduling", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("Schedule pod with config map", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					PodCount: 1,
				},
			)

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod.Spec.Volumes = []v1.Volume{
				{
					Name: "config-volume",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "config-map",
							},
						},
					},
				},
			}
			pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
				{
					Name:      "config-volume",
					MountPath: "/data",
				},
			}

			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-map",
					Namespace: pod.Namespace,
				},
				Data: map[string]string{
					"config": "data",
				},
			}

			err := testCtx.ControllerClient.Create(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		It("Not scheduling if configmap doesn't exist", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					PodCount: 1,
				},
			)

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod.Spec.Volumes = []v1.Volume{
				{
					Name: "config-volume",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "config-map",
							},
						},
					},
				},
			}
			pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
				{
					Name:      "config-volume",
					MountPath: "/data",
				},
			}

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
		})
	})
}
