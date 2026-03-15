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

			vectorMap := resource_info.NewResourceVectorMap()

			for _, c := range cases {
				task := &pod_info.PodInfo{
					ResReq: resource_info.NewResourceRequirementsWithGpus(1),
				}

				idle := resource_info.NewResource(0, 0, c.nonAllocated)
				releasing := resource_info.EmptyResource()

				node := &node_info.NodeInfo{
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								node_info.GpuCountLabel: strconv.Itoa(c.gpuCount),
							},
						},
					},
					Idle:            idle,
					IdleVector:      idle.ToVector(vectorMap),
					Releasing:       releasing,
					ReleasingVector: releasing.ToVector(vectorMap),
					VectorMap:       vectorMap,
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

				idle2 := resource_info.NewResource(c.nonAllocated, 0, 0)
				allocatable := resource_info.NewResource(float64(c.gpuCount), 0, 0)
				releasing2 := resource_info.EmptyResource()

				node = &node_info.NodeInfo{
					Node:              &corev1.Node{},
					Idle:              idle2,
					IdleVector:        idle2.ToVector(vectorMap),
					Allocatable:       allocatable,
					AllocatableVector: allocatable.ToVector(vectorMap),
					Releasing:         releasing2,
					ReleasingVector:   releasing2.ToVector(vectorMap),
					VectorMap:         vectorMap,
				}

				actual, err = nof(task, node)
				Expect(err).To(Not(HaveOccurred()))
				Expect(actual).To(Equal(c.expected))
			}
		})
	})
})
