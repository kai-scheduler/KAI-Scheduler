// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestSumResources(t *testing.T) {
	type args struct {
		left  v1.ResourceList
		right v1.ResourceList
	}
	tests := []struct {
		name string
		args args
		want v1.ResourceList
	}{
		{
			"Add same single resource",
			args{
				v1.ResourceList{"r1": resource.MustParse("1")},
				v1.ResourceList{"r1": resource.MustParse("1")},
			},
			v1.ResourceList{"r1": resource.MustParse("2")},
		},
		{
			"Add whole to fraction",
			args{
				v1.ResourceList{"r1": resource.MustParse("1")},
				v1.ResourceList{"r1": resource.MustParse("500m")},
			},
			v1.ResourceList{"r1": resource.MustParse("1500m")},
		},
		{
			"Add multiple resources",
			args{
				v1.ResourceList{"r1": resource.MustParse("1"), "r2": resource.MustParse("2")},
				v1.ResourceList{"r1": resource.MustParse("1"), "r3": resource.MustParse("3")},
			},
			v1.ResourceList{"r1": resource.MustParse("2"), "r2": resource.MustParse("2"),
				"r3": resource.MustParse("3")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SumResources(tt.args.left, tt.args.right)
			err := EqualResourceLists(got, tt.want)
			if err != nil {
				t.Errorf("SumResources() = \nGot:\n%v\nWant:\n%v, err:\n%v", got, tt.want, err)
			}
		})
	}
}
