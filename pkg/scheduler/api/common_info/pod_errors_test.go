// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"reflect"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func TestFitErrors_Error(t *testing.T) {
	type fields struct {
		nodes map[string]*TasksFitError
		err   string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			"If no individual node errors exist, print only the main error ",
			fields{
				err: "Pod team-a/task-pv-pod fails scheduling with error(s):\nfailed to run Volume Binding: pod has unbound immediate PersistentVolumeClaims. Reasons: pod has unbound immediate PersistentVolumeClaims\n",
			},
			"Pod team-a/task-pv-pod fails scheduling with error(s):\nfailed to run Volume Binding: pod has unbound immediate PersistentVolumeClaims. Reasons: pod has unbound immediate PersistentVolumeClaims\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &TasksFitErrors{
				nodes: tt.fields.nodes,
				err:   tt.fields.err,
			}
			if got := f.Error(); got != tt.want {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewFitErrorInsufficientResource(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()
	type args struct {
		name              string
		namespace         string
		nodeName          string
		resourceRequested *resource_info.ResourceRequirements
		usedResource      *resource_info.Resource
		capacityResource  *resource_info.Resource
		capacityGpuMemory int64
		gangSchedulingJob bool
		suffix            string
	}
	tests := []struct {
		name string
		args args
		want *TasksFitError
	}{
		{
			name: "Not enough cpu",
			args: args{
				name:              "t1",
				namespace:         "n1",
				nodeName:          "node1",
				resourceRequested: resource_info.NewResourceRequirements(0, 1500, 1000),
				usedResource:      BuildResource("500m", "1M"),
				capacityResource:  BuildResource("1000m", "2M"),
				capacityGpuMemory: 0,
				gangSchedulingJob: false,
			},
			want: &TasksFitError{
				taskName:        "t1",
				taskNamespace:   "n1",
				NodeName:        "node1",
				Reasons:         []string{"node(s) didn't have enough resources: CPU cores"},
				DetailedReasons: []string{"Node didn't have enough resources: CPU cores, requested: 1.5, used: 0.5, capacity: 1"},
			},
		},
		{
			name: "Not enough whole gpus",
			args: args{
				name:              "t1",
				namespace:         "n1",
				nodeName:          "node1",
				resourceRequested: resource_info.NewResourceRequirements(2, 500, 1000),
				usedResource:      BuildResourceWithGpu("500m", "1M", "1", "1"),
				capacityResource:  BuildResourceWithGpu("1000m", "2M", "2", "110"),
				capacityGpuMemory: 0,
				gangSchedulingJob: false,
			},
			want: &TasksFitError{
				taskName:        "t1",
				taskNamespace:   "n1",
				NodeName:        "node1",
				Reasons:         []string{"node(s) didn't have enough resources: GPUs"},
				DetailedReasons: []string{"Node didn't have enough resources: GPUs, requested: 2, used: 1, capacity: 2"},
			},
		},
		{
			name: "Not enough fractional gpus",
			args: args{
				name:              "t1",
				namespace:         "n1",
				nodeName:          "node1",
				resourceRequested: resource_info.NewResourceRequirements(0.5, 500, 1000),
				usedResource:      resource_info.NewResource(500, 1000, 1.8),
				capacityResource:  BuildResourceWithGpu("1000m", "2M", "2", "110"),
				capacityGpuMemory: 0,
				gangSchedulingJob: false,
			},
			want: &TasksFitError{
				taskName:        "t1",
				taskNamespace:   "n1",
				NodeName:        "node1",
				Reasons:         []string{"node(s) didn't have enough resources: GPUs"},
				DetailedReasons: []string{"Node didn't have enough resources: GPUs, requested: 0.5, used: 1.8, capacity: 2"},
			},
		},
		{
			name: "Not enough multi fractional gpus",
			args: args{
				name:      "t1",
				namespace: "n1",
				nodeName:  "node1",
				resourceRequested: &resource_info.ResourceRequirements{
					BaseResource:           *resource_info.EmptyBaseResource(),
					GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithMultiFraction(2, 0.5, 0),
				},
				usedResource:      resource_info.NewResource(500, 1000, 1.8),
				capacityResource:  BuildResourceWithGpu("1000m", "2M", "2", "110"),
				capacityGpuMemory: 0,
				gangSchedulingJob: false,
			},
			want: &TasksFitError{
				taskName:        "t1",
				taskNamespace:   "n1",
				NodeName:        "node1",
				Reasons:         []string{"node(s) didn't have enough resources: GPUs"},
				DetailedReasons: []string{"Node didn't have enough resources: GPUs, requested: 2 X 0.5, used: 1.8, capacity: 2"},
			},
		},
		{
			name: "Not enough gpu memory capacity",
			args: args{
				name:      "t1",
				namespace: "n1",
				nodeName:  "node1",
				resourceRequested: &resource_info.ResourceRequirements{
					BaseResource:           *resource_info.EmptyBaseResource(),
					GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(0, 2000),
				},
				usedResource:      resource_info.NewResource(500, 1000, 1.8),
				capacityResource:  BuildResourceWithGpu("1000m", "2M", "2", "110"),
				capacityGpuMemory: 1000,
				gangSchedulingJob: false,
			},
			want: &TasksFitError{
				taskName:        "t1",
				taskNamespace:   "n1",
				NodeName:        "node1",
				Reasons:         []string{"node(s) didn't have enough resources: GPU memory"},
				DetailedReasons: []string{"Node didn't have enough resources: Each gpu on the node has a gpu memory capacity of 1000 Mib. 2000 Mib of gpu memory has been requested."},
			},
		},
		{
			name: "Not enough cpu due to pod overhead",
			args: args{
				name:              "t1",
				namespace:         "n1",
				nodeName:          "node1",
				resourceRequested: resource_info.NewResourceRequirements(0, 1500, 1000),
				usedResource:      BuildResource("500m", "1M"),
				capacityResource:  BuildResource("1000m", "2M"),
				capacityGpuMemory: 0,
				gangSchedulingJob: false,
				suffix:            "Message suffix",
			},
			want: &TasksFitError{
				taskName:        "t1",
				taskNamespace:   "n1",
				NodeName:        "node1",
				Reasons:         []string{"node(s) didn't have enough resources: CPU cores. Message suffix"},
				DetailedReasons: []string{"Node didn't have enough resources: CPU cores, requested: 1.5, used: 0.5, capacity: 1. Message suffix"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usedVector := tt.args.usedResource.ToVector(vectorMap)
			capacityVector := tt.args.capacityResource.ToVector(vectorMap)
			if got := NewFitErrorInsufficientResource(tt.args.name, tt.args.namespace, tt.args.nodeName,
				tt.args.resourceRequested, usedVector, capacityVector, vectorMap, tt.args.capacityGpuMemory,
				tt.args.gangSchedulingJob, tt.args.suffix); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewFitErrorInsufficientResource() = %v, want %v", got, tt.want)
			}
		})
	}
}
