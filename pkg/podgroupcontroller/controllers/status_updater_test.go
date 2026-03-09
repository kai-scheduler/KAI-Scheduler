// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers/cluster_relations"
)

func Test_handlePodGroupStatus(t *testing.T) {
	type clusterData struct {
		podGroup                 *v2alpha2.PodGroup
		inClusterPods            []client.Object
		inClusterPriorityClasses []client.Object
	}
	tests := []struct {
		name                  string
		config                Configs
		clusterData           clusterData
		expectedChangedStatus v2alpha2.PodGroupStatus
		wantErr               bool
	}{
		{
			"Single pod cpu only",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c1",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{"pod-group-name": "pg1"},
						},
						Spec: v1.PodSpec{
							PriorityClassName: "c1",
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
									},
								},
							},
						},
						Status: v1.PodStatus{Phase: v1.PodPending},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated:               nil,
					AllocatedNonPreemptible: nil,
					Requested: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1")},
				},
			},
			false,
		},
		{
			"Single pod with gpu and third party resources preemptible",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c1",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{"pod-group-name": "pg1"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("500m"),
											v1.ResourceMemory: resource.MustParse("1G"),
											"nvidia.com/gpu":  resource.MustParse("2"),
										},
									},
								},
							},
						},
						Status: v1.PodStatus{Phase: v1.PodPending},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated:               nil,
					AllocatedNonPreemptible: nil,
					Requested: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU:    resource.MustParse("500m"),
						v1.ResourceMemory: resource.MustParse("1G"),
						"nvidia.com/gpu":  resource.MustParse("2"),
					},
				},
			},
			false,
		},
		{
			"Single pod cpu only non preemptible",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{"pod-group-name": "pg1"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
									},
								},
							},
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
							Conditions: []v1.PodCondition{
								{
									Type:   v1.PodScheduled,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c2",
						},
						Value: 100,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1")},
					AllocatedNonPreemptible: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1")},
					Requested: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1")},
				},
			},
			false,
		},
		{
			"Single pending pod",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{"pod-group-name": "pg1"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
									},
								},
							},
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c2",
						},
						Value: 100,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated:               nil,
					AllocatedNonPreemptible: nil,
					Requested: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1"),
					},
				},
			},
			false,
		},
		{
			"Single running pod",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{"pod-group-name": "pg1"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
									},
								},
							},
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
							Conditions: []v1.PodCondition{
								{
									Type:   v1.PodScheduled,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c2",
						},
						Value: 100,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1"),
					},
					AllocatedNonPreemptible: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1"),
					},
					Requested: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1"),
					},
				},
			},
			false,
		},
		{
			"Single succeeded pod",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{"pod-group-name": "pg1"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
									},
								},
							},
						},
						Status: v1.PodStatus{
							Phase: v1.PodSucceeded,
							Conditions: []v1.PodCondition{
								{
									Type:   v1.PodScheduled,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c2",
						},
						Value: 100,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated:               nil,
					AllocatedNonPreemptible: nil,
					Requested:               nil,
				},
			},
			false,
		},
		{
			"Single failed pod",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{"pod-group-name": "pg1"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
									},
								},
							},
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
							Conditions: []v1.PodCondition{
								{
									Type:   v1.PodScheduled,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c2",
						},
						Value: 100,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated:               nil,
					AllocatedNonPreemptible: nil,
					Requested:               nil,
				},
			},
			false,
		},
		{
			"Single pod cpu only preemptible",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c1",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{"pod-group-name": "pg1"},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
									},
								},
							},
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
							Conditions: []v1.PodCondition{
								{
									Type:   v1.PodScheduled,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c2",
						},
						Value: 100,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1")},
					AllocatedNonPreemptible: nil,
					Requested: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU: resource.MustParse("1")},
				},
			},
			false,
		},
		{
			"Single pod gpu fraction",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c1",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "n1",
							Name:      "pod1",
							Annotations: map[string]string{
								"pod-group-name": "pg1",
								"gpu-fraction":   "0.4",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("500m"),
											v1.ResourceMemory: resource.MustParse("1G"),
										},
									},
								},
							},
						},
						Status: v1.PodStatus{Phase: v1.PodPending},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated:               nil,
					AllocatedNonPreemptible: nil,
					Requested: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU:    resource.MustParse("500m"),
						v1.ResourceMemory: resource.MustParse("1G"),
						"nvidia.com/gpu":  resource.MustParse("400m"),
					},
				},
			},
			false,
		},
		{
			"Single pod gpu memory - fraction allocated",
			Configs{},
			clusterData{
				&v2alpha2.PodGroup{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "scheduling.run.ai/v2alpha2",
						Kind:       "PodGroup",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pg1",
						ResourceVersion: "999",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c1",
					},
				},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "n1",
							Name:      "pod1",
							Annotations: map[string]string{
								"pod-group-name":         "pg1",
								"gpu-memory":             "1000",
								"received-resource-type": "Fraction",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("500m"),
											v1.ResourceMemory: resource.MustParse("1G"),
										},
									},
								},
							},
							NodeName: "n1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
							Conditions: []v1.PodCondition{
								{
									Type:   v1.PodScheduled,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
					&v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "n1",
							Labels: map[string]string{"nvidia.com/gpu.memory": "4000"},
						},
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 75,
					},
				},
			},
			v2alpha2.PodGroupStatus{
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU:    resource.MustParse("500m"),
						v1.ResourceMemory: resource.MustParse("1G"),
						"nvidia.com/gpu":  resource.MustParse("250m"),
					},
					AllocatedNonPreemptible: nil,
					Requested: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU:      resource.MustParse("500m"),
						v1.ResourceMemory:   resource.MustParse("1G"),
						"run.ai/gpu.memory": resource.MustParse("1k"),
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := createScheme(t)
			kubeClientBuilder := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v2alpha2.PodGroup{}).
				WithIndex(&v1.Pod{}, cluster_relations.PodGroupToPodsIndexer, cluster_relations.PodGroupNameIndexerFunc)
			clusterObjects := tt.clusterData.inClusterPods
			clusterObjects = append(clusterObjects, tt.clusterData.inClusterPriorityClasses...)
			clusterObjects = append(clusterObjects, tt.clusterData.podGroup)
			kubeClient := kubeClientBuilder.WithObjects(clusterObjects...).Build()
			podReconciler := &PodGroupReconciler{
				Client: kubeClient,
				config: tt.config,
			}

			originalPodGroup := v2alpha2.PodGroup{}
			err := kubeClient.Get(
				context.TODO(),
				types.NamespacedName{Namespace: tt.clusterData.podGroup.Namespace, Name: tt.clusterData.podGroup.Name},
				&originalPodGroup,
			)
			if err != nil {
				t.Errorf("handlePodGroupStatus() failed to retrive original pod-group with error = %v", err)
				return
			}
			_, err = podReconciler.handlePodGroupStatus(context.TODO(), &originalPodGroup)
			if (err != nil) != tt.wantErr {
				t.Errorf("handlePodGroupStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			updatedPodGroup := v2alpha2.PodGroup{}
			err = kubeClient.Get(
				context.TODO(),
				types.NamespacedName{Namespace: tt.clusterData.podGroup.Namespace, Name: tt.clusterData.podGroup.Name},
				&updatedPodGroup,
			)
			if err != nil {
				t.Errorf("handlePodGroupStatus() failed to retrive updated pod-group with error = %v", err)
				return
			}

			if !reflect.DeepEqual(updatedPodGroup.Status, tt.expectedChangedStatus) {
				t.Errorf("handlePodGroupStatus() got = %v, want %v",
					updatedPodGroup.Status, tt.expectedChangedStatus)
			}
		})
	}
}

func createScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	err := v1.AddToScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}
	err = schedulingv1.AddToScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}
	err = v2alpha2.AddToScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}
	return scheme
}
