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
	v12 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

func DescribePVCSpecs() bool {
	return Describe("Scheduling of Pod with PVC", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					PodCount: 1,
				},
			)
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

		It("Schedule pod with PV and PVC", func(ctx context.Context) {
			testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			pvName := utils.GenerateRandomK8sName(10)
			pv := rd.CreatePersistentVolumeObject(pvName, "/tmp")
			_, err := testCtx.KubeClientset.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			pvcName := utils.GenerateRandomK8sName(10)
			pvc := rd.CreatePersistentVolumeClaimObject(pvcName, "manual", resource.MustParse("1k"))
			_, err = testCtx.KubeClientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(ctx, pvc, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod.Spec.Volumes = []v1.Volume{
				{
					Name: "pv-storage",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			}
			pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
				{
					Name:      "pv-storage",
					MountPath: "/tmp/a",
				},
			}
			pod.Labels[testconfig.GetConfig().QueueLabelKey] = testCtx.Queues[0].Name

			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		Context("CSI storageclass", func() {
			var storageClass *v12.StorageClass

			BeforeAll(func(ctx context.Context) {
				storageClassName := utils.GenerateRandomK8sName(10)
				storageClass = rd.CreateStorageClass(storageClassName)
				_, err := testCtx.KubeClientset.StorageV1().StorageClasses().Create(ctx, storageClass, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			})

			AfterAll(func(ctx context.Context) {
				testCtx.ClusterCleanup(ctx)
			})

			AfterEach(func(ctx context.Context) {
				testCtx.TestContextCleanup(ctx)
			})

			It("Don't schedule pod with pvc that exceeds volume capacity", func(ctx context.Context) {
				capacity.SkipIfCSIDriverIsMissing(ctx, testCtx.KubeClientset, "local.csi.openebs.io")
				capacity.SkipIfCSICapacitiesAreMissing(ctx, testCtx.KubeClientset)

				testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

				capacities, err := testCtx.KubeClientset.StorageV1().CSIStorageCapacities("openebs").List(ctx,
					metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())

				quantity := resource.MustParse("0")
				for _, capacity := range capacities.Items {
					if quantity.Value() < capacity.Capacity.Value() {
						quantity.Set(capacity.Capacity.Value())
					}
				}
				Expect(quantity.Value()).NotTo(BeZero())

				quantity.Set(quantity.Value() * 2)

				pvcName := utils.GenerateRandomK8sName(10)
				pvc := rd.CreatePersistentVolumeClaimObject(pvcName, storageClass.Name, quantity)
				_, err = testCtx.KubeClientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(ctx, pvc,
					metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
				pod.Spec.Volumes = []v1.Volume{
					{
						Name: "pv-storage",
						VolumeSource: v1.VolumeSource{
							PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
								ClaimName: pvcName,
							},
						},
					},
				}
				pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
					{
						Name:      "pv-storage",
						MountPath: "/tmp/a",
					},
				}
				pod.Labels[testconfig.GetConfig().QueueLabelKey] = testCtx.Queues[0].Name

				_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())

				wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
			})

			It("Don't schedule pod with invalid storage class", func(ctx context.Context) {
				capacity.SkipIfCSIDriverIsMissing(ctx, testCtx.KubeClientset, "local.csi.openebs.io")
				capacity.SkipIfCSICapacitiesAreMissing(ctx, testCtx.KubeClientset)

				testNamespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

				capacities, err := testCtx.KubeClientset.StorageV1().CSIStorageCapacities("openebs").List(ctx,
					metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())

				quantity := resource.MustParse("0")
				for _, capacity := range capacities.Items {
					if quantity.Value() < capacity.Capacity.Value() {
						quantity.Set(capacity.Capacity.Value())
					}
				}
				Expect(quantity.Value()).NotTo(BeZero())

				quantity.Set(int64(float64(quantity.Value()) * 0.5))

				pvcName := utils.GenerateRandomK8sName(10)
				pvc := rd.CreatePersistentVolumeClaimObject(pvcName, "invalid-storageclass", quantity)
				_, err = testCtx.KubeClientset.CoreV1().PersistentVolumeClaims(testNamespace).Create(ctx, pvc,
					metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
				pod.Spec.Volumes = []v1.Volume{
					{
						Name: "pv-storage",
						VolumeSource: v1.VolumeSource{
							PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
								ClaimName: pvcName,
							},
						},
					},
				}
				pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
					{
						Name:      "pv-storage",
						MountPath: "/tmp/a",
					},
				}
				pod.Labels[testconfig.GetConfig().QueueLabelKey] = testCtx.Queues[0].Name

				_, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())

				wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
			})
		})
	})
}
