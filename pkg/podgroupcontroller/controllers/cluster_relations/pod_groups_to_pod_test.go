// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_relations

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

func TestGetAllPodsOfPodGroup(t *testing.T) {
	type args struct {
		ctx           context.Context
		podGroup      *v2alpha2.PodGroup
		inClusterPods []client.Object
	}
	tests := []struct {
		name    string
		args    args
		want    v1.PodList
		wantErr bool
	}{
		{
			"Pod group with a single pod",
			args{
				context.TODO(),
				&v2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Namespace: "n1", Name: "pg1"}},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{podGroupAnnotationForPodName: "pg1"},
						},
					},
				},
			},
			v1.PodList{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
				Items: []v1.Pod{{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pod1",
						Annotations:     map[string]string{podGroupAnnotationForPodName: "pg1"},
						ResourceVersion: "999",
					},
				}},
			},
			false,
		},
		{
			"Pod group with a single pod - do not pick pods with different pod-groups or namespaces",
			args{
				context.TODO(),
				&v2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Namespace: "n1", Name: "pg1"}},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{podGroupAnnotationForPodName: "pg1"},
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod2",
							Annotations: map[string]string{podGroupAnnotationForPodName: "pg2"},
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n2",
							Name:        "pod1",
							Annotations: map[string]string{podGroupAnnotationForPodName: "pg1"},
						},
					},
				},
			},
			v1.PodList{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
				Items: []v1.Pod{{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pod1",
						Annotations:     map[string]string{podGroupAnnotationForPodName: "pg1"},
						ResourceVersion: "999",
					},
				}},
			},
			false,
		},
		{
			"Pod group with a multiple pods",
			args{
				context.TODO(),
				&v2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Namespace: "n1", Name: "pg1"}},
				[]client.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod1",
							Annotations: map[string]string{podGroupAnnotationForPodName: "pg1"},
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:   "n1",
							Name:        "pod2",
							Annotations: map[string]string{podGroupAnnotationForPodName: "pg1"},
						},
					},
				},
			},
			v1.PodList{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
				Items: []v1.Pod{{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "n1",
						Name:            "pod1",
						Annotations:     map[string]string{podGroupAnnotationForPodName: "pg1"},
						ResourceVersion: "999",
					}},
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace:       "n1",
							Name:            "pod2",
							Annotations:     map[string]string{podGroupAnnotationForPodName: "pg1"},
							ResourceVersion: "999",
						},
					}},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := v1.AddToScheme(scheme)
			if err != nil {
				t.Fatal(err)
			}
			kubeClientBuilder := fake.NewClientBuilder().WithScheme(scheme).
				WithIndex(&v1.Pod{}, PodGroupToPodsIndexer, PodGroupNameIndexerFunc)
			if tt.args.inClusterPods != nil {
				kubeClientBuilder = kubeClientBuilder.WithObjects(tt.args.inClusterPods...)
			}
			kubeClient := kubeClientBuilder.Build()

			got, err := GetAllPodsOfPodGroup(tt.args.ctx, tt.args.podGroup, kubeClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAllPodsOfPodGroup() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err = verifyPodList(got, tt.want); err != nil {
				t.Errorf("GetAllPodsOfPodGroup() failed with %v: got = %v, want %v", err, got, tt.want)
			}
		})
	}
}

func TestPodGroupNameIndexerFunc(t *testing.T) {
	tests := []struct {
		name   string
		rawObj client.Object
		want   []string
	}{
		{
			"Non pod object",
			&v1.Node{},
			nil,
		},
		{
			"Pod with no annotations",
			&v1.Pod{},
			nil,
		},
		{
			"Pod with no pod-group",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						podGroupAnnotationForPodName: "pg1",
					},
				},
			},
			[]string{"pg1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodGroupNameIndexerFunc(tt.rawObj); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PodGroupNameIndexerFunc() = %v, want %v", got, tt.want)
			}
		})
	}
}

func verifyPodList(l, r v1.PodList) error {
	if len(l.Items) != len(r.Items) {
		return fmt.Errorf("PodList length mismatch: %d != %d", len(l.Items), len(r.Items))
	}
	for i := range l.Items {
		if l.Items[i].Name != r.Items[i].Name {
			return fmt.Errorf("PodList item mismatch, name: %s != %s", l.Items[i].Name, r.Items[i].Name)
		}
		if l.Items[i].Namespace != r.Items[i].Namespace {
			return fmt.Errorf("PodList item mismatch, namespace: %s != %s", l.Items[i].Namespace, r.Items[i].Namespace)
		}
	}
	return nil
}
