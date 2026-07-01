/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	resourcev1alhpa3 "k8s.io/api/resource/v1alpha3"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	version "k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	faketesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	kubeaischedulerfake "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	fakeschedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/typed/scheduling/v1alpha2/fake"
	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
)

func TestCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test cache")
}

var _ = Describe("Cache", func() {
	Describe("New", func() {
		Context("Pod informer filtering", func() {
			It("should filter terminal pods without filtering pods by scheduler name", func() {
				kubeClient := fake.NewSimpleClientset()
				cache := New(&SchedulerCacheParams{
					KubeClient:         kubeClient,
					KAISchedulerClient: kubeaischedulerfake.NewSimpleClientset(),
					NodePoolParams:     &conf.SchedulingNodePoolParams{},
					DiscoveryClient:    kubeClient.Discovery(),
				})

				stopCh := make(chan struct{})
				defer close(stopCh)
				cache.Run(stopCh)
				cache.WaitForCacheSync(stopCh)

				podSelectors := []string{}
				nonPodSelectors := map[string][]string{}
				for _, action := range kubeClient.Actions() {
					switch typedAction := action.(type) {
					case faketesting.ListAction:
						selector := typedAction.GetListRestrictions().Fields.String()
						if action.GetResource().Resource == "pods" {
							podSelectors = append(podSelectors, selector)
						} else if selector != "" {
							nonPodSelectors[action.GetResource().Resource] = append(nonPodSelectors[action.GetResource().Resource], selector)
						}
					case faketesting.WatchAction:
						selector := typedAction.GetWatchRestrictions().Fields.String()
						if action.GetResource().Resource == "pods" {
							podSelectors = append(podSelectors, selector)
						} else if selector != "" {
							nonPodSelectors[action.GetResource().Resource] = append(nonPodSelectors[action.GetResource().Resource], selector)
						}
					}
				}

				Expect(podSelectors).NotTo(BeEmpty())
				for _, selector := range podSelectors {
					Expect(selector).To(ContainSubstring("status.phase!=Succeeded"))
					Expect(selector).To(ContainSubstring("status.phase!=Failed"))
					Expect(selector).NotTo(ContainSubstring("spec.schedulerName"))
				}
				Expect(nonPodSelectors).To(BeEmpty())
			})
		})

		Context("DRA Feature Gate", func() {
			DescribeTable("should record DRA availability based on Kubernetes version and resource API availability",
				func(serverMajor, serverMinor string, resourceGroupVersions []string, expectDRAAvailable bool) {
					fakeClient := fake.NewClientset()
					fakeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{
						Major: serverMajor,
						Minor: serverMinor,
					}

					for _, groupVersion := range resourceGroupVersions {
						fakeClient.Resources = append(fakeClient.Resources, &metav1.APIResourceList{GroupVersion: groupVersion})
					}

					params := &SchedulerCacheParams{
						KubeClient:         fakeClient,
						KAISchedulerClient: kubeaischedulerfake.NewSimpleClientset(),
						NodePoolParams:     &conf.SchedulingNodePoolParams{},
						DiscoveryClient:    fakeClient.Discovery(),
					}

					cache := New(params)

					Expect(cache).NotTo(BeNil())
					Expect(featuregates.DynamicResourcesEnabled()).To(Equal(expectDRAAvailable))
				},
				Entry("compatible version (1.32) with resource API should enable DRA", "1", "32", []string{resourcev1beta1.SchemeGroupVersion.String()}, true),
				Entry("compatible version (1.32) without resource API should not enable DRA", "1", "32", []string{}, false),
				Entry("incompatible version (1.25) with resource API should not enable DRA", "1", "25", []string{resourcev1beta1.SchemeGroupVersion.String()}, false),
				Entry("incompatible version (1.25) without resource API should not enable DRA", "1", "25", []string{}, false),
				Entry("edge case version (1.31) with resource API should not enable DRA", "1", "31", []string{resourcev1alhpa3.SchemeGroupVersion.String()}, false),
				Entry("higher compatible version (1.35) with resource API should enable DRA", "1", "34", []string{resourcev1.SchemeGroupVersion.String()}, true),
			)
		})

		Context("Pod informer transform", func() {
			It("should strip unused pod fields while preserving scheduler-relevant data", func() {
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "namespace-1",
						UID:       types.UID("pod-uid"),
						Labels: map[string]string{
							"app": "test",
						},
						Annotations: map[string]string{
							"runai/shared-gpu-configmap": "shared-gpu-configmap-prefix",
						},
					},
					Spec: v1.PodSpec{
						NodeName:          "node-1",
						PriorityClassName: "priority-class-1",
						Priority:          ptr.To(int32(7)),
						SchedulerName:     "kai-scheduler",
						RestartPolicy:     v1.RestartPolicyAlways,
						NodeSelector:      map[string]string{"kai.scheduler/node-pool": "pool-a"},
						SchedulingGates:   []v1.PodSchedulingGate{{Name: "gate-1"}},
						RuntimeClassName:  ptr.To("nvidia"),
						Overhead:          v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
						Affinity: &v1.Affinity{
							NodeAffinity: &v1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
									NodeSelectorTerms: []v1.NodeSelectorTerm{{
										MatchExpressions: []v1.NodeSelectorRequirement{{
											Key:      "kai.scheduler/node-pool",
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{"pool-a"},
										}},
									}},
								},
							},
						},
						Tolerations: []v1.Toleration{{
							Key:      "kai.scheduler/node-pool",
							Operator: v1.TolerationOpEqual,
							Value:    "pool-a",
							Effect:   v1.TaintEffectNoSchedule,
						}},
						TopologySpreadConstraints: []v1.TopologySpreadConstraint{{
							MaxSkew:           1,
							TopologyKey:       "topology.kubernetes.io/zone",
							WhenUnsatisfiable: v1.DoNotSchedule,
						}},
						Volumes: []v1.Volume{
							{
								Name: "config-volume",
								VolumeSource: v1.VolumeSource{
									ConfigMap: &v1.ConfigMapVolumeSource{
										LocalObjectReference: v1.LocalObjectReference{Name: "config-map"},
									},
								},
							},
							{
								Name: "pvc-volume",
								VolumeSource: v1.VolumeSource{
									PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
										ClaimName: "claim-1",
									},
								},
							},
						},
						InitContainers: []v1.Container{{
							Name:  "init-1",
							Image: "busybox:latest",
							Ports: []v1.ContainerPort{{Name: "http", HostPort: 3000, ContainerPort: 80}},
							Env: []v1.EnvVar{
								{Name: "BIG_VALUE", Value: strings.Repeat("x", 1024)},
								{
									Name: "CONFIG_REF",
									ValueFrom: &v1.EnvVarSource{
										ConfigMapKeyRef: &v1.ConfigMapKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: "env-config"},
											Key:                  "value",
										},
									},
								},
							},
							EnvFrom: []v1.EnvFromSource{{
								ConfigMapRef: &v1.ConfigMapEnvSource{
									LocalObjectReference: v1.LocalObjectReference{Name: "env-from-config"},
								},
							}},
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("500m")},
							},
							VolumeMounts: []v1.VolumeMount{{Name: "config-volume", MountPath: "/config"}},
						}},
						Containers: []v1.Container{{
							Name:  "main",
							Image: "busybox:latest",
							Ports: []v1.ContainerPort{{Name: "http", HostPort: 4000, ContainerPort: 8080}},
							Env: []v1.EnvVar{
								{Name: "BIG_VALUE", Value: strings.Repeat("y", 1024)},
								{
									Name: "CONFIG_REF",
									ValueFrom: &v1.EnvVarSource{
										ConfigMapKeyRef: &v1.ConfigMapKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: "env-config"},
											Key:                  "value",
										},
									},
								},
							},
							EnvFrom: []v1.EnvFromSource{{
								ConfigMapRef: &v1.ConfigMapEnvSource{
									LocalObjectReference: v1.LocalObjectReference{Name: "env-from-config"},
								},
							}},
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1000m")},
							},
							VolumeMounts: []v1.VolumeMount{{Name: "config-volume", MountPath: "/config"}},
						}},
						EphemeralContainers: []v1.EphemeralContainer{{
							EphemeralContainerCommon: v1.EphemeralContainerCommon{
								Name:  "debugger",
								Image: "busybox:latest",
								Env: []v1.EnvVar{
									{Name: "BIG_VALUE", Value: strings.Repeat("z", 1024)},
									{
										Name: "CONFIG_REF",
										ValueFrom: &v1.EnvVarSource{
											ConfigMapKeyRef: &v1.ConfigMapKeySelector{
												LocalObjectReference: v1.LocalObjectReference{Name: "env-config"},
												Key:                  "value",
											},
										},
									},
								},
								EnvFrom: []v1.EnvFromSource{{
									ConfigMapRef: &v1.ConfigMapEnvSource{
										LocalObjectReference: v1.LocalObjectReference{Name: "env-from-config"},
									},
								}},
								VolumeMounts: []v1.VolumeMount{{Name: "config-volume", MountPath: "/debug-config"}},
							},
						}},
					},
				}

				cache, stopCh := setupCacheWithObjects(true, []runtime.Object{pod}, &schedulingv1alpha2.BindRequest{})
				defer close(stopCh)

				storedPod, err := cache.(*SchedulerCache).KubeInformerFactory().Core().V1().Pods().Lister().
					Pods(pod.Namespace).Get(pod.Name)
				Expect(err).NotTo(HaveOccurred())
				Expect(storedPod.ManagedFields).To(BeNil())
				Expect(storedPod.Spec.Containers).To(HaveLen(1))
				Expect(storedPod.Spec.Containers[0].Env).To(HaveLen(1))
				Expect(storedPod.Spec.Containers[0].Env[0].Name).To(Equal("CONFIG_REF"))
				Expect(storedPod.Spec.Containers[0].Env[0].ValueFrom).NotTo(BeNil())
				Expect(storedPod.Spec.Containers[0].Env[0].ValueFrom.ConfigMapKeyRef).NotTo(BeNil())
				Expect(storedPod.Spec.Containers[0].Env[0].Value).To(BeEmpty())
				Expect(storedPod.Spec.Containers[0].EnvFrom).To(HaveLen(1))
				Expect(storedPod.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
				Expect(storedPod.Spec.Containers[0].Ports).To(HaveLen(1))
				Expect(storedPod.Spec.Containers[0].Ports[0].HostPort).To(Equal(int32(4000)))
				Expect(storedPod.Spec.InitContainers).To(HaveLen(1))
				Expect(storedPod.Spec.InitContainers[0].Env).To(HaveLen(1))
				Expect(storedPod.Spec.InitContainers[0].Env[0].Name).To(Equal("CONFIG_REF"))
				Expect(storedPod.Spec.InitContainers[0].Ports[0].HostPort).To(Equal(int32(3000)))
				Expect(storedPod.Spec.EphemeralContainers).To(HaveLen(1))
				Expect(storedPod.Spec.EphemeralContainers[0].Env).To(HaveLen(1))
				Expect(storedPod.Spec.EphemeralContainers[0].Env[0].Name).To(Equal("CONFIG_REF"))
				Expect(storedPod.Spec.EphemeralContainers[0].VolumeMounts).To(HaveLen(1))
				Expect(storedPod.Spec.Volumes).To(HaveLen(2))
				Expect(storedPod.Spec.Affinity).NotTo(BeNil())
				Expect(storedPod.Spec.NodeSelector).To(HaveKeyWithValue("kai.scheduler/node-pool", "pool-a"))
				Expect(storedPod.Spec.Tolerations).To(HaveLen(1))
				Expect(storedPod.Spec.TopologySpreadConstraints).To(HaveLen(1))
				Expect(storedPod.Spec.PriorityClassName).To(Equal("priority-class-1"))
				Expect(storedPod.Spec.Priority).NotTo(BeNil())
				Expect(storedPod.Spec.SchedulingGates).To(HaveLen(1))
				Expect(storedPod.Spec.RuntimeClassName).NotTo(BeNil())
				Expect(*storedPod.Spec.RuntimeClassName).To(Equal("nvidia"))
			})
		})
	})
	Describe("Bind", func() {
		Context("failure to bind", func() {
			It("should return error", func() {
				objects := []runtime.Object{
					&v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
						},
					},
				}
				cache, stopCh := setupCacheWithObjects(true, objects, &schedulingv1alpha2.BindRequest{})
				defer close(stopCh)

				cache.(*SchedulerCache).kubeAiSchedulerClient.SchedulingV1alpha2().(*fakeschedulingv1alpha2.FakeSchedulingV1alpha2).PrependReactor(
					"create", "bindrequests",
					func(action faketesting.Action) (handled bool, ret runtime.Object, err error) {
						return true, nil, fmt.Errorf("failed to create bind request")
					},
				)

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "namespace-1",
						UID:       types.UID("pod-uid"),
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container-1",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"cpu":            resource.MustParse("2000m"),
										"memory":         resource.MustParse("5Gi"),
										"nvidia.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodPending,
					},
				}

				taskInfo := pod_info.NewTaskInfo(pod, resource_info.NewResourceVectorMap())

				err := cache.Bind(taskInfo, "node-1", map[string]string{})
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("Stale BindRequests Cleanup", func() {
		It("Delete a single stale bind request",
			func() {
				cache, stopCh := setupCacheWithObjects(true, []runtime.Object{}, &schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bind-request-1",
						Namespace: "namespace-1",
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      "pod-1",
						SelectedNode: "node-1",
						BackoffLimit: ptr.To(int32(1)),
					},
					Status: schedulingv1alpha2.BindRequestStatus{
						Phase:          schedulingv1alpha2.BindRequestPhaseFailed,
						FailedAttempts: 1,
					},
				})
				defer close(stopCh)

				kubeAiSchedulerClient := cache.(*SchedulerCache).kubeAiSchedulerClient
				bindRequestsAfterCleanup, err := kubeAiSchedulerClient.SchedulingV1alpha2().BindRequests("namespace-1").List(context.TODO(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(bindRequestsAfterCleanup.Items).To(HaveLen(0))
			},
		)

		It("Delete single stale bind and leaves on", func() {
			cache, stopCh := setupCacheWithObjects(
				true,
				[]runtime.Object{
					&v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod-1",
							Namespace: "namespace-1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bind-request-1",
						Namespace: "namespace-1",
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      "pod-1",
						SelectedNode: "node-1",
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bind-request-2",
						Namespace: "namespace-1",
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      "pod-2",
						SelectedNode: "node-2",
						BackoffLimit: ptr.To(int32(1)),
					},
					Status: schedulingv1alpha2.BindRequestStatus{
						Phase:          schedulingv1alpha2.BindRequestPhaseFailed,
						FailedAttempts: 1,
					},
				},
			)
			defer close(stopCh)

			kubeAiSchedulerClient := cache.(*SchedulerCache).kubeAiSchedulerClient
			bindRequestsAfterCleanup, err := kubeAiSchedulerClient.SchedulingV1alpha2().BindRequests("namespace-1").List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(bindRequestsAfterCleanup.Items).To(HaveLen(1))
		})

		It("Reports all failed deletions", func() {
			cache, stopCh := setupCacheWithObjects(
				false,
				[]runtime.Object{},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bind-request-1",
						Namespace: "namespace-1",
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      "pod-1",
						SelectedNode: "node-1",
						BackoffLimit: ptr.To(int32(1)),
					},
					Status: schedulingv1alpha2.BindRequestStatus{
						Phase:          schedulingv1alpha2.BindRequestPhaseFailed,
						FailedAttempts: 1,
					},
				},
				&schedulingv1alpha2.BindRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bind-request-2",
						Namespace: "namespace-1",
					},
					Spec: schedulingv1alpha2.BindRequestSpec{
						PodName:      "pod-2",
						SelectedNode: "node-2",
						BackoffLimit: ptr.To(int32(1)),
					},
					Status: schedulingv1alpha2.BindRequestStatus{
						Phase:          schedulingv1alpha2.BindRequestPhaseFailed,
						FailedAttempts: 1,
					},
				},
			)
			defer close(stopCh)

			cache.(*SchedulerCache).kubeAiSchedulerClient.SchedulingV1alpha2().(*fakeschedulingv1alpha2.FakeSchedulingV1alpha2).PrependReactor(
				"delete", "bindrequests",
				func(action faketesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("failed to delete bind request")
				},
			)

			_, err := cache.Snapshot()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bind-request-1"))
			Expect(err.Error()).To(ContainSubstring("bind-request-2"))

			kubeAiSchedulerClient := cache.(*SchedulerCache).kubeAiSchedulerClient
			bindRequestsAfterCleanup, err := kubeAiSchedulerClient.SchedulingV1alpha2().BindRequests("namespace-1").List(context.TODO(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(bindRequestsAfterCleanup.Items).To(HaveLen(2))
		})

	})
})

func setupCacheWithObjects(snapshot bool, objects []runtime.Object, kaiSchedulerObjects ...runtime.Object) (Cache, chan struct{}) {
	kubeClient := fake.NewSimpleClientset(objects...)
	kubeAiSchedulerClient := kubeaischedulerfake.NewSimpleClientset(kaiSchedulerObjects...)

	cache := New(&SchedulerCacheParams{
		KubeClient:            kubeClient,
		KAISchedulerClient:    kubeAiSchedulerClient,
		NodePoolParams:        &conf.SchedulingNodePoolParams{},
		FullHierarchyFairness: true,
		DiscoveryClient:       kubeClient.Discovery(),
	})

	stopCh := make(chan struct{})
	cache.Run(stopCh)
	cache.WaitForCacheSync(stopCh)

	if snapshot {
		_, err := cache.Snapshot()
		Expect(err).NotTo(HaveOccurred())
	}

	return cache, stopCh
}
