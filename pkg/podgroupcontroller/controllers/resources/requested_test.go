// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
)

func Test_extractRequestedResources(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want v1.ResourceList
	}{
		{
			"Pod with gpu fraction",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuFraction: "0.5"},
				},
			},
			v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("0.5")},
		},
		{
			"Pod with gpu multi fraction",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFraction:            "0.5",
						constants.GpuFractionsNumDevices: "2",
					},
				},
			},
			v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("1")},
		},
		{
			"Pod with gpu memory",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuMemory: "2000"},
				},
			},
			v1.ResourceList{gpuMemoryResourceName: resource.MustParse("2000")},
		},
		{
			"Regular cpu pod",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"cpu":    resource.MustParse("100m"),
									"memory": resource.MustParse("1G"),
								},
							},
						},
					},
				},
			},
			v1.ResourceList{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractGPUSharingRequestedResources(tt.pod)
			if err != nil {
				t.Errorf("extractRequestedResources() returned error %v", err)
			}

			if err = compareResourceLists(got, tt.want); err != nil {
				t.Errorf("extractRequestedResources() list differ from the wanted one: %v ", err)
			}
		})
	}
}

func compareResourceLists(rightList, leftList v1.ResourceList) error {
	if len(rightList) != len(leftList) {
		return fmt.Errorf("the amount of resources kinds is different between the 2 lists."+
			" right-list: %v, left-list: %v", rightList, leftList)
	}

	for resourceName, rightListResourceValue := range rightList {
		leftListResourceValue, ok := leftList[resourceName]
		if !ok {
			return fmt.Errorf("the resource %s exists in the rightList but doesn't exist in the leftList",
				resourceName)
		}
		if !rightListResourceValue.Equal(leftListResourceValue) {
			return fmt.Errorf("for the resource %s, the values differ between the list."+
				" right-list: %v, left-list: %v", resourceName, rightListResourceValue, leftListResourceValue)
		}
	}

	return nil
}
