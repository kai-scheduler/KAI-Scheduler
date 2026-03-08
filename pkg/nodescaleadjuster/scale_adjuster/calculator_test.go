// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale_adjuster

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/consts"
	testutils "github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/test-utils"
)

func Test_calculateNumScalingDevices(t *testing.T) {
	tests := []struct {
		name string
		pods []*v1.Pod
		want int64
	}{
		{
			"No scaling pods",
			[]*v1.Pod{},
			0,
		},
		{
			"Single scaling pod",
			[]*v1.Pod{
				testutils.CreateScalingPod("ns1", "pod1", 2),
			},
			2,
		},
		{
			"Similar scaling pods",
			[]*v1.Pod{
				testutils.CreateScalingPod("ns1", "pod1", 2),
				testutils.CreateScalingPod("ns2", "pod2", 2),
				testutils.CreateScalingPod("ns3", "pod3", 2),
			},
			6,
		},
		{
			"Different scaling pods",
			[]*v1.Pod{
				testutils.CreateScalingPod("ns1", "pod1", 1),
				testutils.CreateScalingPod("ns2", "pod2", 2),
				testutils.CreateScalingPod("ns3", "pod3", 4),
			},
			7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCalculator(consts.DefaultGPUMemoryToFractionRatio)
			got := c.calculateNumScalingDevices(tt.pods)
			if got != tt.want {
				t.Errorf("calculateNumScalingDevices() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_calculateMaxScalingDevices(t *testing.T) {
	tests := []struct {
		name string
		pods []*v1.Pod
		want int64
	}{
		{
			"No scaling pods",
			[]*v1.Pod{},
			0,
		},
		{
			"Single scaling pod",
			[]*v1.Pod{
				testutils.CreateScalingPod("ns1", "pod1", 2),
			},
			2,
		},
		{
			"Similar scaling pods",
			[]*v1.Pod{
				testutils.CreateScalingPod("ns1", "pod1", 2),
				testutils.CreateScalingPod("ns2", "pod2", 2),
				testutils.CreateScalingPod("ns3", "pod3", 2),
			},
			2,
		},
		{
			"Different scaling pods",
			[]*v1.Pod{
				testutils.CreateScalingPod("ns1", "pod1", 1),
				testutils.CreateScalingPod("ns2", "pod2", 2),
				testutils.CreateScalingPod("ns3", "pod3", 4),
				testutils.CreateScalingPod("ns4", "pod4", 3),
			},
			4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCalculator(consts.DefaultGPUMemoryToFractionRatio)
			got := c.calculateMaxScalingDevices(tt.pods)
			if got != tt.want {
				t.Errorf("calculateMaxScalingDevices() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_calculateNumNeededDevices(t *testing.T) {
	tests := []struct {
		name           string
		pods           []*v1.Pod
		numPodsToScale int
		numDevices     int64
	}{
		{
			"No pods",
			[]*v1.Pod{},
			0,
			0,
		},
		{
			"Single pod, all fit into a single device",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "pod1", "0.2", 2),
			},
			1,
			1,
		},
		{
			"Single pod, sum devices is an integer",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "pod1", "0.5", 2),
			},
			1,
			1,
		},
		{
			"Single pod, sum devices is greater than 1",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "pod1", "0.6", 2),
			},
			1,
			2,
		},
		{
			"Single pod, sum devices is greater than 1.5",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "pod1", "0.6", 2),
			},
			1,
			2,
		},
		{
			"Multiple pods require single device",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "pod1", "0.1", 2),
				testutils.CreateUnschedulableFractionPod("ns2", "pod2", "0.5", 1),
			},
			2,
			1,
		},
		{
			"Multiple pods require single device - second option",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "pod1", "0.2", 2),
				testutils.CreateUnschedulableFractionPod("ns2", "pod2", "0.1", 3),
			},
			2,
			1,
		},
		{
			"Multiple pods require single device - third option",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "pod1", "0.1", 2),
				testutils.CreateUnschedulableFractionPod("ns2", "pod2", "0.1", 2),
				testutils.CreateUnschedulableFractionPod("ns3", "pod3", "0.1", 2),
				testutils.CreateUnschedulableFractionPod("ns4", "pod4", "0.1", 2),
				testutils.CreateUnschedulableFractionPod("ns5", "pod5", "0.1", 2),
			},
			5,
			1,
		},
		{
			"Multiple pods require multiple devices",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "pod1", "0.3", 2),
				testutils.CreateUnschedulableFractionPod("ns2", "pod2", "0.5", 1),
			},
			2,
			2,
		},
		{
			"Single pod requires GPU memory",
			[]*v1.Pod{
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod1", "1024", 1),
			},
			1,
			1,
		},
		{
			"Multiple pods require GPU memory",
			[]*v1.Pod{
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod1", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod2", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod3", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod4", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod5", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod6", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod7", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod8", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod9", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod10", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "pod11", "1024", 1),
			},
			11,
			2,
		},
		{
			"Mixture of GPU fraction pods and GPU memory pods",
			[]*v1.Pod{
				testutils.CreateUnschedulableFractionPod("ns1", "fraction-pod1", "0.9", 2),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "mem-pod1", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "mem-pod2", "1024", 1),
				testutils.CreateUnschedulablePodWithGpuMemory("ns1", "mem-pod3", "1024", 1),
			},
			4,
			3,
		},
		{
			"Pod Without GPU Fraction annotation",
			[]*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "pod1",
						Namespace:   "ns1",
						Annotations: map[string]string{},
					},
				},
			},
			0,
			0,
		},
		{
			"Pod With invalid Num GPU Devices annotation",
			[]*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "ns1",
						Annotations: map[string]string{
							constants.GpuFraction: "abc",
						},
					},
				},
			},
			0,
			0,
		},
		{
			"Multiple pods, some are invalid",
			[]*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "pod1",
						Namespace:   "ns1",
						Annotations: map[string]string{},
					},
				},
				testutils.CreateUnschedulableFractionPod("ns2", "pod2", "0.5", 1),
			},
			1,
			1,
		},
		{
			"Pod With invalid Num GPU Devices annotation",
			[]*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "namespace1",
						Annotations: map[string]string{
							constants.GpuFraction:            "0.5",
							constants.GpuFractionsNumDevices: "0.5",
						},
					},
				},
			},
			0,
			0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCalculator(consts.DefaultGPUMemoryToFractionRatio)
			numDevices, podsToScale := c.calculateNumNeededDevices(tt.pods)
			if len(podsToScale) != tt.numPodsToScale {
				t.Errorf("calculateNumNeededDevices() len(podsToScale) = %v, want %v",
					len(podsToScale), tt.numPodsToScale)
			}
			if numDevices != tt.numDevices {
				t.Errorf("calculateNumNeededDevices() numDevices = %v, want %v", numDevices, tt.numDevices)
			}
		})
	}
}
