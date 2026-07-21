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
		err string
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
				err: tt.fields.err,
			}
			if got := f.Error(); got != tt.want {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFitErrorsAddNodeErrorIgnoresTypedNil(t *testing.T) {
	fitErrors := NewFitErrors()
	var fitError *TasksFitError

	fitErrors.AddNodeError(fitError)

	if got := fitErrors.Error(); got != ResourcesWereNotFoundMsg {
		t.Fatalf("Error() = %q, want %q", got, ResourcesWereNotFoundMsg)
	}
}

func TestFitErrorsAggregatesNodeReasons(t *testing.T) {
	fitErrors := NewFitErrors()
	fitErrors.AddNodeError(NewFitErrorWithDetailedMessage(
		"pod", "namespace", "node-a",
		[]string{"node(s) didn't have enough resources: GPUs"},
		"Node didn't have enough resources: GPUs, requested: 1, used: 8, capacity: 8",
	))
	fitErrors.AddNodeError(NewFitErrorWithDetailedMessage(
		"pod", "namespace", "node-b",
		[]string{
			"node(s) didn't have enough resources: GPUs",
			"node(s) didn't match Pod's node affinity/selector",
		},
		"Node didn't have enough resources: GPUs, requested: 1, used: 8, capacity: 8",
		"node(s) didn't match Pod's node affinity/selector",
	))
	want := "no nodes with enough resources were found: 1 node(s) didn't match Pod's node affinity/selector. \n" +
		"2 node(s) didn't have enough resources: GPUs."
	if got := fitErrors.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestFitErrorsStoresCompactReasonCounts(t *testing.T) {
	fitErrors := NewFitErrors()
	fitErrors.AddNodeError(NewFitErrorWithDetailedMessage(
		"pod", "namespace", "node-a",
		[]string{"MissingGPU"},
		"node-a detailed GPU message",
	))
	fitErrors.AddNodeError(NewFitErrorWithDetailedMessage(
		"pod", "namespace", "node-b",
		[]string{"MissingGPU", "NoStorage"},
		"node-b detailed GPU message",
		"node-b detailed storage message",
	))

	if got := fitErrors.ReasonCount("MissingGPU"); got != 2 {
		t.Fatalf("ReasonCount(MissingGPU) = %d, want 2", got)
	}
	if got := fitErrors.ReasonCount("NoStorage"); got != 1 {
		t.Fatalf("ReasonCount(NoStorage) = %d, want 1", got)
	}
	if got := fitErrors.UniqueReasonCount(); got != 2 {
		t.Fatalf("UniqueReasonCount() = %d, want 2", got)
	}
	if !fitErrors.HasNodeErrors() {
		t.Fatal("HasNodeErrors() = false, want true")
	}

	want := "no nodes with enough resources were found: 1 NoStorage. \n2 MissingGPU."
	if got := fitErrors.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestFitErrorsDetailedErrorUsesTransientNodeErrors(t *testing.T) {
	fitErrors := NewFitErrors()
	fitErrors.AddNodeError(NewFitError("pod", "namespace", "node-a", "MissingGPU"))

	nodeErrors := []*TasksFitError{
		NewFitErrorWithDetailedMessage(
			"pod", "namespace", "node-b", []string{"NoStorage"}, "node-b detailed storage message"),
		NewFitErrorWithDetailedMessage(
			"pod", "namespace", "node-a", []string{"MissingGPU"}, "node-a detailed GPU message"),
	}
	want := "\n<node-a>: node-a detailed GPU message." +
		"\n<node-b>: node-b detailed storage message." +
		"\nno nodes with enough resources were found."
	if got := fitErrors.DetailedError(nodeErrors); got != want {
		t.Fatalf("DetailedError() = %q, want %q", got, want)
	}
	if got := fitErrors.ReasonCount("MissingGPU"); got != 1 {
		t.Fatalf("ReasonCount(MissingGPU) after DetailedError = %d, want 1", got)
	}
}

func TestAddNodeErrorDoesNotFormatStructuredFitError(t *testing.T) {
	fitErrors := NewFitErrors()
	fitError := NewFitError("pod", "namespace", "node-a", "MissingGPU")
	fitErrors.AddNodeError(fitError)

	allocations := testing.AllocsPerRun(100, func() {
		fitErrors.AddNodeError(fitError)
	})
	if allocations != 0 {
		t.Fatalf("AddNodeError() allocations = %v, want 0", allocations)
	}
}

func TestAddNodeErrorIgnoresTypedNilFitError(t *testing.T) {
	fitErrors := NewFitErrors()
	var fitError *TasksFitError
	var err error = fitError

	fitErrors.AddNodeError(err)

	if fitErrors.HasNodeErrors() {
		t.Fatal("HasNodeErrors() = true after typed nil error, want false")
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
				&tt.args.resourceRequested.GpuResourceRequirement, tt.args.resourceRequested.ToVector(vectorMap),
				usedVector, capacityVector, vectorMap, tt.args.capacityGpuMemory,
				tt.args.gangSchedulingJob, tt.args.suffix); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewFitErrorInsufficientResource() = %v, want %v", got, tt.want)
			}
		})
	}
}
