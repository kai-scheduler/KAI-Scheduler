// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

func Test_mapsEqualBySourceKeys(t *testing.T) {
	tests := []struct {
		name   string
		source map[string]string
		target map[string]string
		want   bool
	}{
		{
			"both nil",
			nil,
			nil,
			true,
		},
		{
			"source nil, target not nil",
			nil,
			map[string]string{"key1": "value1"},
			true,
		},
		{
			"source not nil, target nil",
			map[string]string{"key1": "value1"},
			nil,
			false,
		},
		{
			"equal maps",
			map[string]string{"key1": "value1", "key2": "value2"},
			map[string]string{"key1": "value1", "key2": "value2"},
			true,
		},
		{
			"target has extra keys",
			map[string]string{"key1": "value1"},
			map[string]string{"key1": "value1", "key2": "value2"},
			true,
		},
		{
			"different values",
			map[string]string{"key1": "value1"},
			map[string]string{"key1": "different"},
			false,
		},
		{
			"missing key in target",
			map[string]string{"key1": "value1", "key2": "value2"},
			map[string]string{"key1": "value1"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapsEqualBySourceKeys(tt.source, tt.target); got != tt.want {
				t.Errorf("mapsEqualBySourceKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_copyStringMap(t *testing.T) {
	tests := []struct {
		name   string
		source map[string]string
		target map[string]string
		expect map[string]string
	}{
		{
			"nil source",
			nil,
			map[string]string{"key1": "value1"},
			map[string]string{"key1": "value1"},
		},
		{
			"nil target",
			map[string]string{"key1": "value1"},
			nil,
			map[string]string{"key1": "value1"},
		},
		{
			"overwrite existing",
			map[string]string{"key1": "newvalue", "key2": "value2"},
			map[string]string{"key1": "oldvalue"},
			map[string]string{"key1": "newvalue", "key2": "value2"},
		},
		{
			"merge maps",
			map[string]string{"key2": "value2"},
			map[string]string{"key1": "value1"},
			map[string]string{"key1": "value1", "key2": "value2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := copyStringMap(tt.source, tt.target)
			if !reflect.DeepEqual(result, tt.expect) {
				t.Errorf("copyStringMap() returned = %v, want %v", result, tt.expect)
			}
		})
	}
}

func Test_podGroupsEqual(t *testing.T) {
	type args struct {
		leftPodGroup  *enginev2alpha2.PodGroup
		rightPodGroup *enginev2alpha2.PodGroup
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"Two identical pod groups",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			true,
		},
		{
			"Different owner reference",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o2",
								UID:  "32764823",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			false,
		},
		{
			"Different spec",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(2)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			false,
		},
		{
			"Different annotations",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{"A": "a"},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(2)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			false,
		},
		{
			"Different labels",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{"L2": "l2"},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{"L1": "l1"},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := podGroupsEqual(tt.args.leftPodGroup, tt.args.rightPodGroup); got != tt.want {
				t.Errorf("podGroupsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_updatePodGroup(t *testing.T) {
	type args struct {
		oldPodGroup *enginev2alpha2.PodGroup
		newPodGroup *enginev2alpha2.PodGroup
	}
	tests := []struct {
		name                   string
		args                   args
		expectedPodGroupChange *enginev2alpha2.PodGroup
	}{
		{
			"Two identical pod groups",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			&enginev2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "p1",
					Namespace:   "n1",
					Annotations: map[string]string{},
					Labels:      map[string]string{},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "o1",
							UID:  "81726381762",
						},
					},
				},
				Spec: enginev2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(1)),
				},
			},
		},
		{
			"Different owner reference",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o2",
								UID:  "32764823",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			&enginev2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "p1",
					Namespace:   "n1",
					Annotations: map[string]string{},
					Labels:      map[string]string{},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "o2",
							UID:  "32764823",
						},
					},
				},
				Spec: enginev2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(1)),
				},
			},
		},
		{
			"Different spec",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(2)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			&enginev2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "p1",
					Namespace:   "n1",
					Annotations: map[string]string{},
					Labels:      map[string]string{},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "o1",
							UID:  "81726381762",
						},
					},
				},
				Spec: enginev2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(2)),
				},
			},
		},
		{
			"Different annotations",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{"A": "a"},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(2)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{"B": "b"},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			&enginev2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "p1",
					Namespace:   "n1",
					Annotations: map[string]string{"A": "a", "B": "b"},
					Labels:      map[string]string{},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "o1",
							UID:  "81726381762",
						},
					},
				},
				Spec: enginev2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(1)),
				},
			},
		},
		{
			"Different annotations - blocklisted",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{"workload-status": "Pending"},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{"workload-status": "Running"},
						Labels:      map[string]string{},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			&enginev2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "p1",
					Namespace:   "n1",
					Annotations: map[string]string{"workload-status": "Pending"},
					Labels:      map[string]string{},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "o1",
							UID:  "81726381762",
						},
					},
				},
				Spec: enginev2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(1)),
				},
			},
		},
		{
			"Different labels",
			args{
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{"L2": "l2"},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
				&enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Namespace:   "n1",
						Annotations: map[string]string{},
						Labels:      map[string]string{"L1": "l1"},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name: "o1",
								UID:  "81726381762",
							},
						},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						MinMember: ptr.To(int32(1)),
					},
				},
			},
			&enginev2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "p1",
					Namespace:   "n1",
					Annotations: map[string]string{},
					Labels:      map[string]string{"L1": "l1"},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name: "o1",
							UID:  "81726381762",
						},
					},
				},
				Spec: enginev2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(1)),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updatePodGroup(tt.args.oldPodGroup, tt.args.newPodGroup)
		})
	}
}
