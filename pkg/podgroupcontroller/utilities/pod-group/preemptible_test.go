// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group

import (
	"context"
	"testing"

	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

func TestIsPreemptible(t *testing.T) {
	type args struct {
		podGroup                 *v2alpha2.PodGroup
		inClusterPriorityClasses []client.Object
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			"Tranning class pod-group is preemptable",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c1",
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
						Value: 125,
					},
				},
			},
			true,
			false,
		},
		{
			"Inference class pod-group is non preemptable",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
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
						Value: 125,
					},
				},
			},
			false,
			false,
		},
		{
			"Pod group with missing priority class",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n2",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c3",
						},
						Value: 125,
					},
				},
			},
			true,
			false,
		},
		{
			"Custom preemptable class",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c3",
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c3",
						},
						Value: 83,
					},
				},
			},
			true,
			false,
		},
		{
			"Custom non preemptable class",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c3",
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c3",
						},
						Value: 200,
					},
				},
			},
			false,
			false,
		},
		{
			"Priority class not found - use global default priority class",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						GlobalDefault: true,
						Value:         125,
					},
				},
			},
			false,
			false,
		},
		{
			"Both specific and global default priority classes not found - use system default priority",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
					},
					Spec: v2alpha2.PodGroupSpec{
						PriorityClassName: "c2",
					},
				},
				[]client.Object{
					&schedulingv1.PriorityClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Value: 125,
					},
				},
			},
			// Assuming system default priority is below non-preemptible threshold
			true,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := schedulingv1.AddToScheme(scheme)
			if err != nil {
				t.Fatal(err)
			}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.args.inClusterPriorityClasses...).
				Build()

			got, err := IsPreemptible(context.TODO(), tt.args.podGroup, kubeClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsPreemptible() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("IsPreemptible() got = %v, want %v", got, tt.want)
			}
		})
	}
}
