// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

var _ = Describe("compactSchedulerPod", func() {
	succeededPod := func() *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-0",
				Namespace: "team-a",
				UID:       "worker-0-uid",
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod:           "pg-1",
					"kubectl.kubernetes.io/last-applied-configuration": "{...}",
				},
				Labels: map[string]string{
					commonconstants.SubGroupLabelKey: "workers",
					"app":                            "trainer",
				},
				ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubelet"}},
			},
			Spec: v1.PodSpec{
				NodeName:     "node-1",
				NodeSelector: map[string]string{"gpu": "true"},
				Tolerations:  []v1.Toleration{{Key: "nvidia.com/gpu"}},
				Volumes:      []v1.Volume{{Name: "data"}},
				Containers: []v1.Container{{
					Name: "main",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					},
				}},
			},
			Status: v1.PodStatus{
				Phase:             v1.PodSucceeded,
				PodIP:             "10.0.0.1",
				Conditions:        []v1.PodCondition{{Type: v1.PodReady}},
				ContainerStatuses: []v1.ContainerStatus{{Name: "main"}},
			},
		}
	}

	Context("succeeded pods", func() {
		It("retains identity, podgroup, subgroup and phase", func() {
			transformed, err := compactSchedulerPod(succeededPod())
			Expect(err).NotTo(HaveOccurred())

			compact, ok := transformed.(*v1.Pod)
			Expect(ok).To(BeTrue())
			Expect(compact.Name).To(Equal("worker-0"))
			Expect(compact.Namespace).To(Equal("team-a"))
			Expect(compact.UID).To(BeEquivalentTo("worker-0-uid"))
			Expect(compact.Annotations).To(HaveKeyWithValue(commonconstants.PodGroupAnnotationForPod, "pg-1"))
			Expect(compact.Labels).To(HaveKeyWithValue(commonconstants.SubGroupLabelKey, "workers"))
			Expect(compact.Status.Phase).To(Equal(v1.PodSucceeded))
		})

		It("drops spec, status and metadata not read for succeeded pods", func() {
			transformed, err := compactSchedulerPod(succeededPod())
			Expect(err).NotTo(HaveOccurred())

			compact := transformed.(*v1.Pod)
			Expect(compact.Annotations).To(HaveLen(1))
			Expect(compact.Labels).To(HaveLen(1))
			Expect(compact.ManagedFields).To(BeNil())
			Expect(compact.Spec).To(Equal(v1.PodSpec{}))
			Expect(compact.Status.PodIP).To(BeEmpty())
			Expect(compact.Status.Conditions).To(BeNil())
			Expect(compact.Status.ContainerStatuses).To(BeNil())
		})

		It("leaves the informer's copy untouched", func() {
			pod := succeededPod()
			_, err := compactSchedulerPod(pod)
			Expect(err).NotTo(HaveOccurred())

			Expect(pod.Spec.Containers).To(HaveLen(1))
			Expect(pod.Annotations).To(HaveLen(2))
		})

		It("keeps enough for the podgroup of a completing gang to stay non-stale", func() {
			vectorMap := resource_info.NewResourceVectorMap()
			taskInfo := func(pod *v1.Pod) *pod_info.PodInfo {
				delete(pod.Labels, commonconstants.SubGroupLabelKey)
				transformed, err := compactSchedulerPod(pod)
				Expect(err).NotTo(HaveOccurred())
				return pod_info.NewTaskInfo(transformed.(*v1.Pod), vectorMap)
			}

			succeeded := taskInfo(succeededPod())
			Expect(succeeded.Status).To(Equal(pod_status.Succeeded))
			Expect(succeeded.Job).To(BeEquivalentTo("pg-1"))

			running := succeededPod()
			running.Name = "worker-1"
			running.UID = "worker-1-uid"
			running.Status.Phase = v1.PodRunning

			podGroup := podgroup_info.NewPodGroupInfo("pg-1", succeeded, taskInfo(running))
			podGroup.GetAllPodSets()[podgroup_info.DefaultSubGroup].SetMinAvailable(2)

			Expect(podGroup.IsStale()).To(BeFalse())
		})

		It("omits the podgroup annotation and subgroup label when the pod has neither", func() {
			pod := succeededPod()
			pod.Annotations = nil
			pod.Labels = nil

			transformed, err := compactSchedulerPod(pod)
			Expect(err).NotTo(HaveOccurred())

			compact := transformed.(*v1.Pod)
			Expect(compact.Annotations).To(BeNil())
			Expect(compact.Labels).To(BeNil())
		})
	})

	Context("non-succeeded pods", func() {
		It("keeps the fields scheduling depends on", func() {
			pod := succeededPod()
			pod.Status.Phase = v1.PodRunning

			transformed, err := compactSchedulerPod(pod)
			Expect(err).NotTo(HaveOccurred())

			compact := transformed.(*v1.Pod)
			Expect(compact.Spec.NodeName).To(Equal("node-1"))
			Expect(compact.Spec.NodeSelector).To(HaveKeyWithValue("gpu", "true"))
			Expect(compact.Spec.Tolerations).To(HaveLen(1))
			Expect(compact.Spec.Containers).To(HaveLen(1))
			Expect(compact.Spec.Containers[0].Resources.Requests).To(HaveKey(v1.ResourceCPU))
			Expect(compact.Annotations).To(HaveLen(2))
			Expect(compact.ManagedFields).To(BeNil())
		})
	})
})
