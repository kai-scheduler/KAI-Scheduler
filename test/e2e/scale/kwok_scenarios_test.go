// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import "testing"

func TestFractionWorkloadSize(t *testing.T) {
	tests := []struct {
		name         string
		nodes        int
		fractionGPUs int
		smallPods    int
		largePods    int
		totalPods    int
	}{
		{
			name:         "small scale run",
			nodes:        24,
			fractionGPUs: 66,
			smallPods:    264,
			largePods:    66,
			totalPods:    330,
		},
		{
			name:         "current baseline",
			nodes:        500,
			fractionGPUs: 1400,
			smallPods:    5600,
			largePods:    1400,
			totalPods:    7000,
		},
		{
			name:         "rounds each configuration down",
			nodes:        501,
			fractionGPUs: 1402,
			smallPods:    5608,
			largePods:    1402,
			totalPods:    7010,
		},
		{
			name:         "two thousand nodes",
			nodes:        2000,
			fractionGPUs: 5600,
			smallPods:    22400,
			largePods:    5600,
			totalPods:    28000,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fractionGPUs, smallPods, largePods := fractionWorkloadSize(test.nodes)
			if fractionGPUs != test.fractionGPUs {
				t.Fatalf("fraction GPUs: got %d, want %d", fractionGPUs, test.fractionGPUs)
			}
			if smallPods != test.smallPods {
				t.Fatalf("small fraction pods: got %d, want %d", smallPods, test.smallPods)
			}
			if largePods != test.largePods {
				t.Fatalf("large fraction pods: got %d, want %d", largePods, test.largePods)
			}
			if smallPods+largePods != test.totalPods {
				t.Fatalf("total pods: got %d, want %d", smallPods+largePods, test.totalPods)
			}

			if smallPods%smallFractionPerGPU != 0 || largePods%largeFractionPerGPU != 0 {
				t.Fatalf("pod counts do not represent whole GPUs: small=%d, large=%d", smallPods, largePods)
			}
			accountedGPUs := smallPods/smallFractionPerGPU + largePods/largeFractionPerGPU
			if accountedGPUs != fractionGPUs {
				t.Fatalf("GPU demand: got %d, reported %d", accountedGPUs, fractionGPUs)
			}

			clusterGPUs := test.nodes * gpusPerNode
			if fractionGPUs*20 > clusterGPUs*7 {
				t.Fatalf("fraction GPU demand %d exceeds 35%% of cluster capacity %d", fractionGPUs, clusterGPUs)
			}
		})
	}
}
