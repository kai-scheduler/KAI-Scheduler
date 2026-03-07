// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
)

func Test_getReceivedFraction(t *testing.T) {
	tests := []struct {
		name    string
		pod     *v1.Pod
		node    *v1.Node
		want    resource.Quantity
		wantErr bool
	}{
		{
			"Direct fraction request",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuFraction: "0.4"},
				},
			},
			nil,
			resource.MustParse("0.4"),
			false,
		},
		{
			"Memory request + Nvidia node",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuMemory: "2000"},
				},
				Spec: v1.PodSpec{NodeName: "n1"},
			},
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{constants.NvidiaGpuMemory: "4000"},
				},
			},
			resource.MustParse("0.5"),
			false,
		},
		{
			"Memory request + Amd node",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuMemory: "4000"},
				},
				Spec: v1.PodSpec{NodeName: "n1"},
			},
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{"beta.amd.com/gpu.vram.16G": "1"},
				},
			},
			resource.MustParse("0.25"),
			false,
		},
		{
			"No request annotations",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			nil,
			resource.Quantity{},
			true,
		},
		{
			"Missing node",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuMemory: "2000"},
				},
				Spec: v1.PodSpec{NodeName: "n2"},
			},
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{constants.NvidiaGpuMemory: "4000"},
				},
			},
			resource.Quantity{},
			true,
		},
		{
			"Memory request + node has both Amd and Nvidia labels - error",
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuMemory: "2000"},
				},
				Spec: v1.PodSpec{NodeName: "n1"},
			},
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{constants.NvidiaGpuMemory: "4000", "beta.amd.com/gpu.vram.32G": "1"},
				},
			},
			resource.Quantity{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := v1.AddToScheme(scheme)
			if err != nil {
				t.Fatal(err)
			}
			kubeBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.node != nil {
				kubeBuilder = kubeBuilder.WithObjects(tt.node)
			}
			kubeClient := kubeBuilder.Build()

			got, err := calculateAllocatedFraction(context.TODO(), tt.pod, kubeClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("calculateAllocatedFraction() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calculateAllocatedFraction() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getFractionFromMemoryRequest(t *testing.T) {
	type args struct {
		gpuMemoryStr string
		nodeName     string
	}
	tests := []struct {
		name    string
		args    args
		node    *v1.Node
		want    resource.Quantity
		wantErr bool
	}{
		{
			"Node with Nvidia memory label",
			args{
				gpuMemoryStr: "2000",
				nodeName:     "n1",
			},
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{constants.NvidiaGpuMemory: "4000"},
				},
			},
			resource.MustParse("0.5"),
			false,
		},
		{
			"Node with Amd memory label",
			args{
				gpuMemoryStr: "4000",
				nodeName:     "n1",
			},
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{"beta.amd.com/gpu.vram.16G": "1"},
				},
			},
			resource.MustParse("0.25"),
			false,
		},
		{
			"invalid gpu memory value",
			args{
				gpuMemoryStr: "abc",
				nodeName:     "n1",
			},
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{constants.NvidiaGpuMemory: "4000"},
				},
			},
			resource.Quantity{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := v1.AddToScheme(scheme)
			if err != nil {
				t.Fatal(err)
			}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.node).Build()

			got, err := getFractionFromMemoryRequest(context.TODO(), tt.args.gpuMemoryStr, tt.args.nodeName, kubeClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("getFractionFromMemoryRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getFractionFromMemoryRequest() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getNodeSingleGpuMemory(t *testing.T) {
	tests := []struct {
		name    string
		node    *v1.Node
		want    float64
		wantErr bool
	}{
		{
			"Node with Amd memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{"beta.amd.com/gpu.vram.16G": "1"},
				},
			},
			16000,
			false,
		},
		{
			"Node with Nvidia memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{constants.NvidiaGpuMemory: "5000"},
				},
			},
			5000,
			false,
		},
		{
			"Node without memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"A": "5000"},
				},
			},
			0,
			true,
		},
		{
			"Node with invalid Amd memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "n1",
					Labels: map[string]string{"beta.amd.com/gpu.vram.1bG": "1"},
				},
			},
			0,
			true,
		},
		{
			"Node with invalid Nvidia memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{constants.NvidiaGpuMemory: "abv"},
				},
			},
			0,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := v1.AddToScheme(scheme)
			if err != nil {
				t.Fatal(err)
			}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.node).Build()

			got, err := getNodeSingleGpuMemory(context.TODO(), tt.node.Name, kubeClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("getNodeSingleGpuMemory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getNodeSingleGpuMemory() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getNodeSingleAmdGpuMemory(t *testing.T) {
	tests := []struct {
		name    string
		node    *v1.Node
		want    float64
		wantErr bool
	}{
		{
			"Node with memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"beta.amd.com/gpu.vram.16G": "1"},
				},
			},
			16000,
			false,
		},
		{
			"Node without memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"A": "5000"},
				},
			},
			0,
			true,
		},
		{
			"Node with invalid memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"beta.amd.com/gpu.vram.16b8": "1"},
				},
			},
			0,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getNodeSingleAmdGpuMemory(tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("getNodeSingleAmdGpuMemory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getNodeSingleAmdGpuMemory() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getNodeSingleNvidiaGpuMemory(t *testing.T) {
	tests := []struct {
		name    string
		node    *v1.Node
		want    float64
		wantErr bool
	}{
		{
			"Node with memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{constants.NvidiaGpuMemory: "5000"},
				},
			},
			5000,
			false,
		},
		{
			"Node without memory label",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"A": "5000"},
				},
			},
			0,
			true,
		},
		{
			"Node with memory label - invalid value",
			&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{constants.NvidiaGpuMemory: "abd"},
				},
			},
			0,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getNodeSingleNvidiaGpuMemory(tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("getNodeSingleNvidiaGpuMemory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getNodeSingleNvidiaGpuMemory() got = %v, want %v", got, tt.want)
			}
		})
	}
}
