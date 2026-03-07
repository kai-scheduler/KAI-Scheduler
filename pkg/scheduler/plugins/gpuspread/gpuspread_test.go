// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpuspread

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
)

const fakeNodeName = "node"

func TestGpuSpread(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GpuSpread Suite")
}

var _ = Describe("GpuSpread scoring", func() {
	cases := map[string]struct {
		totalMem      int64
		usedMem       int64
		gpuIdx        string
		expectedScore float64
		expectedErr   error
	}{
		"Whole GPU": {
			totalMem:      1024,
			usedMem:       0,
			gpuIdx:        pod_info.WholeGpuIndicator,
			expectedScore: 1,
		},
		"Free GPU": {
			totalMem:      1024,
			usedMem:       0,
			gpuIdx:        "1000",
			expectedScore: 1,
		},
		"Half allocated GPU": {
			totalMem:      1024,
			usedMem:       512,
			gpuIdx:        "1000",
			expectedScore: 0.5,
		},
		"Fully allocated GPU": {
			totalMem:      1024,
			usedMem:       1024,
			gpuIdx:        "1000",
			expectedScore: 0,
		},
		"Corrupted GPU memory": {
			totalMem:      0,
			usedMem:       0,
			gpuIdx:        "1000",
			expectedScore: 0,
			expectedErr:   fmt.Errorf("node <%s> has invalid GPU memory", fakeNodeName),
		},
		"Unsupported GPU memory": {
			totalMem:      50,
			usedMem:       0,
			gpuIdx:        "1000",
			expectedScore: 0,
			expectedErr:   fmt.Errorf("node <%s> has invalid GPU memory", fakeNodeName),
		},
	}

	task := pod_info.NewTaskInfo(&v1.Pod{}, nil, resource_info.NewResourceVectorMap())

	for caseName, caseSpec := range cases {
		caseName := caseName
		caseSpec := caseSpec
		It(caseName, func() {
			nodeInfo := createFakeSingleGpuNodeInfo(caseSpec.totalMem, caseSpec.usedMem)

			actualScore, err := gpuOrderFn(task, nodeInfo, caseSpec.gpuIdx)
			if caseSpec.expectedErr != nil {
				Expect(err).To(Equal(caseSpec.expectedErr))
			} else {
				Expect(err).To(Not(HaveOccurred()))
			}

			if actualScore != caseSpec.expectedScore {
				fmt.Printf("Expected score: %f, actual score: %f\n", caseSpec.expectedScore, actualScore)
				Expect(false).To(Equal(true))
			}
		})
	}
})

func createFakeSingleGpuNodeInfo(totalGpuMem int64, usedGpuMem int64) *node_info.NodeInfo {
	return &node_info.NodeInfo{
		Name:                   fakeNodeName,
		MemoryOfEveryGpuOnNode: totalGpuMem,
		GpuSharingNodeInfo: node_info.GpuSharingNodeInfo{
			UsedSharedGPUsMemory: map[string]int64{
				"1000": usedGpuMem,
			},
		},
	}
}
