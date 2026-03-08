// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package patcher

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers/metadata"
)

func TestShouldUpdatePodGroupStatus(t *testing.T) {
	type args struct {
		podGroup         *v2alpha2.PodGroup
		podGroupMetadata *metadata.PodGroupMetadata
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"Should update",
			args{
				podGroup: &v2alpha2.PodGroup{
					Status: v2alpha2.PodGroupStatus{
						ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
							Requested: map[v1.ResourceName]resource.Quantity{
								"cpu": resource.MustParse("1"),
							},
							Allocated:               map[v1.ResourceName]resource.Quantity{},
							AllocatedNonPreemptible: map[v1.ResourceName]resource.Quantity{},
						},
					},
				},
				podGroupMetadata: &metadata.PodGroupMetadata{
					Preemptible: true,
					Requested:   map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse("1")},
					Allocated:   map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse("1")},
				},
			},
			true,
		},
		{
			"Should not update",
			args{
				podGroup: &v2alpha2.PodGroup{
					Status: v2alpha2.PodGroupStatus{
						ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
							Requested: map[v1.ResourceName]resource.Quantity{
								"cpu": resource.MustParse("1"),
							},
							Allocated: map[v1.ResourceName]resource.Quantity{
								"cpu": resource.MustParse("1"),
							},
							AllocatedNonPreemptible: map[v1.ResourceName]resource.Quantity{},
						},
					},
				},
				podGroupMetadata: &metadata.PodGroupMetadata{
					Preemptible: true,
					Requested:   map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse("1")},
					Allocated:   map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse("1")},
				},
			},
			false,
		},
		{
			"Should not update - extra data in status",
			args{
				podGroup: &v2alpha2.PodGroup{
					Status: v2alpha2.PodGroupStatus{
						Running: 3,
						ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
							Requested: map[v1.ResourceName]resource.Quantity{
								"cpu": resource.MustParse("1"),
							},
							Allocated: map[v1.ResourceName]resource.Quantity{
								"cpu": resource.MustParse("1"),
							},
							AllocatedNonPreemptible: map[v1.ResourceName]resource.Quantity{},
						},
					},
				},
				podGroupMetadata: &metadata.PodGroupMetadata{
					Preemptible: true,
					Requested:   map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse("1")},
					Allocated:   map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse("1")},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldUpdatePodGroupStatus(tt.args.podGroup, tt.args.podGroupMetadata); got != tt.want {
				t.Errorf("ShouldUpdatePodGroupStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdatePodGroupStatus(t *testing.T) {
	type args struct {
		podGroup         *v2alpha2.PodGroup
		podGroupMetadata *metadata.PodGroupMetadata
	}
	tests := []struct {
		name         string
		args         args
		wantErr      bool
		wantedStatus *v2alpha2.PodGroupStatus
	}{
		{
			"Add request",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
						Name:      "m1",
					},
					Status: v2alpha2.PodGroupStatus{
						Phase: v2alpha2.PodGroupPhase("p1"),
					},
				},
				&metadata.PodGroupMetadata{
					Requested: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("1"),
					},
				},
			},
			false,
			&v2alpha2.PodGroupStatus{
				Phase: v2alpha2.PodGroupPhase("p1"),
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Requested: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("1"),
					},
				},
			},
		},
		{
			"Add request + allocated + preemptible",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
						Name:      "m1",
					},
					Status: v2alpha2.PodGroupStatus{
						Phase: v2alpha2.PodGroupPhase("p1"),
					},
				},
				&metadata.PodGroupMetadata{
					Preemptible: true,
					Requested: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("1"),
						"gpu": resource.MustParse("3"),
					},
					Allocated: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("500m"),
					},
				},
			},
			false,
			&v2alpha2.PodGroupStatus{
				Phase: v2alpha2.PodGroupPhase("p1"),
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Requested: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("1"),
						"gpu": resource.MustParse("3"),
					},
					Allocated: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("500m"),
					},
				},
			},
		},
		{
			"Add request + allocated + non preemptible",
			args{
				&v2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "n1",
						Name:      "m1",
					},
					Status: v2alpha2.PodGroupStatus{
						Phase: v2alpha2.PodGroupPhase("p1"),
					},
				},
				&metadata.PodGroupMetadata{
					Preemptible: false,
					Requested: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("1"),
						"gpu": resource.MustParse("3"),
					},
					Allocated: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("500m"),
					},
				},
			},
			false,
			&v2alpha2.PodGroupStatus{
				Phase: v2alpha2.PodGroupPhase("p1"),
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Requested: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("1"),
						"gpu": resource.MustParse("3"),
					},
					AllocatedNonPreemptible: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("500m"),
					},
					Allocated: map[v1.ResourceName]resource.Quantity{
						"cpu": resource.MustParse("500m"),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := createScheme(t)
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&v2alpha2.PodGroup{}).
				WithObjects(tt.args.podGroup).Build()

			if err := UpdatePodGroupStatus(
				context.TODO(), tt.args.podGroup, tt.args.podGroupMetadata, kubeClient,
			); (err != nil) != tt.wantErr {
				t.Errorf("UpdatePodGroupStatus() error = %v, wantErr %v", err, tt.wantErr)
			}

			originalPodGroup := v2alpha2.PodGroup{}
			err := kubeClient.Get(
				context.TODO(),
				types.NamespacedName{Namespace: tt.args.podGroup.Namespace, Name: tt.args.podGroup.Name},
				&originalPodGroup,
			)
			if err != nil {
				t.Errorf("handlePodGroupStatus() failed to retrive original pod-group with error = %v", err)
				return
			}

			if !reflect.DeepEqual(&originalPodGroup.Status, tt.wantedStatus) {
				t.Errorf("Status after update - originalPodGroup.Status = %v, wantedStatus %v",
					&originalPodGroup.Status, tt.wantedStatus)
			}
		})
	}
}

func createScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()

	err := v2alpha2.AddToScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}
	return scheme
}
