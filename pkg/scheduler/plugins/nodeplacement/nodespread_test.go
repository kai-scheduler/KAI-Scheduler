// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeplacement_test

import (
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/nodeplacement"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeSpread(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeSpread Suite")
}

var _ = Describe("NodeSpread", func() {
	Describe("calculateScore", func() {
		It("should score gpu jobs based on free whole gpus", func() {
			cases := []struct {
				gpuCount     int
				nonAllocated float64
				expected     float64
			}{
				{
					gpuCount:     0,
					nonAllocated: 0,
					expected:     0,
				},
				{
					gpuCount:     1,
					nonAllocated: 0,
					expected:     0,
				},
				{
					gpuCount:     1,
					nonAllocated: 1,
					expected:     1,
				},
				{
					gpuCount:     2,
					nonAllocated: 1,
					expected:     0.5,
				},
				{
					gpuCount:     4,
					nonAllocated: 1,
					expected:     0.25,
				},
				{
					gpuCount:     4,
					nonAllocated: 3,
					expected:     0.75,
				},
			}

			for _, c := range cases {
				task := &pod_info.PodInfo{
					ResReq: resource_info.NewResourceRequirementsWithGpus(1),
				}

				node := &node_info.NodeInfo{
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								node_info.GpuCountLabel: strconv.Itoa(c.gpuCount),
							},
						},
					},
					Idle:      resource_info.NewResource(0, 0, c.nonAllocated),
					Releasing: resource_info.EmptyResource(),
				}

				plugin := nodeplacement.New(map[string]string{
					constants.GPUResource: constants.SpreadStrategy,
					constants.CPUResource: constants.SpreadStrategy,
				})
				ssn := createFakeTestSession(map[string]*node_info.NodeInfo{node.Name: node})
				Expect(ssn.NodeOrderFns).To(HaveLen(0), "NodeOrderFns should be empty")
				plugin.OnSessionOpen(ssn)
				Expect(ssn.NodeOrderFns).To(HaveLen(1), "NodeOrderFns should have one element")
				nof := ssn.NodeOrderFns[len(ssn.NodeOrderFns)-1]

				actual, err := nof(task, node)
				Expect(err).To(Not(HaveOccurred()))
				Expect(actual).To(Equal(c.expected))

				task = &pod_info.PodInfo{
					ResReq: resource_info.NewResourceRequirements(0, 1, 0),
				}

				node = &node_info.NodeInfo{
					Node:        &corev1.Node{},
					Idle:        resource_info.NewResource(c.nonAllocated, 0, 0),
					Allocatable: resource_info.NewResource(float64(c.gpuCount), 0, 0),
					Releasing:   resource_info.EmptyResource(),
				}

				actual, err = nof(task, node)
				Expect(err).To(Not(HaveOccurred()))
				Expect(actual).To(Equal(c.expected))
			}
		})

		It("should account for shared GPU consumption when scoring", func() {
			// Node with 2 GPUs, one GPU has 0.5 allocated (50 of 100 memory)
			// Expected: (1 idle whole GPU + 0.5 available on shared) / 2 total = 0.75
			nodeWithShared := &node_info.NodeInfo{
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-with-shared",
						Labels: map[string]string{
							node_info.GpuCountLabel: "2",
						},
					},
				},
				Idle:                   resource_info.NewResource(0, 0, 1), // 1 whole idle GPU
				Releasing:              resource_info.EmptyResource(),
				MemoryOfEveryGpuOnNode: 100,
				GpuSharingNodeInfo: node_info.GpuSharingNodeInfo{
					ReleasingSharedGPUs:       map[string]bool{},
					UsedSharedGPUsMemory:      map[string]int64{"gpu-0": 50},
					ReleasingSharedGPUsMemory: map[string]int64{},
					AllocatedSharedGPUsMemory: map[string]int64{"gpu-0": 50},
				},
			}

			// Node with 2 GPUs, completely empty
			// Expected: 2 / 2 = 1.0
			nodeEmpty := &node_info.NodeInfo{
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-empty",
						Labels: map[string]string{
							node_info.GpuCountLabel: "2",
						},
					},
				},
				Idle:                   resource_info.NewResource(0, 0, 2), // 2 whole idle GPUs
				Releasing:              resource_info.EmptyResource(),
				MemoryOfEveryGpuOnNode: 100,
				GpuSharingNodeInfo: node_info.GpuSharingNodeInfo{
					ReleasingSharedGPUs:       map[string]bool{},
					UsedSharedGPUsMemory:      map[string]int64{},
					ReleasingSharedGPUsMemory: map[string]int64{},
					AllocatedSharedGPUsMemory: map[string]int64{},
				},
			}

			task := &pod_info.PodInfo{
				ResReq: resource_info.NewResourceRequirementsWithGpus(1),
			}

			nodes := map[string]*node_info.NodeInfo{
				nodeWithShared.Name: nodeWithShared,
				nodeEmpty.Name:      nodeEmpty,
			}
			plugin := nodeplacement.New(map[string]string{
				constants.GPUResource: constants.SpreadStrategy,
				constants.CPUResource: constants.SpreadStrategy,
			})
			ssn := createFakeTestSession(nodes)
			plugin.OnSessionOpen(ssn)
			nof := ssn.NodeOrderFns[len(ssn.NodeOrderFns)-1]

			sharedScore, err := nof(task, nodeWithShared)
			Expect(err).NotTo(HaveOccurred())
			Expect(sharedScore).To(Equal(0.75))

			emptyScore, err := nof(task, nodeEmpty)
			Expect(err).NotTo(HaveOccurred())
			Expect(emptyScore).To(Equal(1.0))

			// The empty node should score higher (more available), ensuring spread
			Expect(emptyScore).To(BeNumerically(">", sharedScore))
		})
	})
})
