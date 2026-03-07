// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nominatednode

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/scores"
)

func TestNominatedNode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NominatedNode Suite")
}

var _ = Describe("NominatedNode Scoring tests", func() {
	cases := map[string]struct {
		task          *pod_info.PodInfo
		node          *node_info.NodeInfo
		expectedScore float64
	}{
		"No nominated node": {
			task: &pod_info.PodInfo{
				Pod: &v1.Pod{
					Status: v1.PodStatus{},
				},
			},
			node: &node_info.NodeInfo{
				Name: "worker-node",
			},
			expectedScore: 0,
		},
		"Different nominated node": {
			task: &pod_info.PodInfo{
				Pod: &v1.Pod{
					Status: v1.PodStatus{
						NominatedNodeName: "other-node",
					},
				},
			},
			node: &node_info.NodeInfo{
				Name: "worker-node",
			},
			expectedScore: 0,
		},
		"Matching nominated node": {
			task: &pod_info.PodInfo{
				Pod: &v1.Pod{
					Status: v1.PodStatus{
						NominatedNodeName: "worker-node",
					},
				},
			},
			node: &node_info.NodeInfo{
				Name: "worker-node",
			},
			expectedScore: scores.NominatedNode,
		},
	}
	for caseName, caseSpec := range cases {
		caseName := caseName
		caseSpec := caseSpec
		It(caseName, func() {
			plugin := &nominatedNodeNamePlugin{}
			nodeOrderFn := plugin.nodeOrderFn()
			actualScore, err := nodeOrderFn(caseSpec.task, caseSpec.node)
			Expect(err).To(BeNil())
			if actualScore != caseSpec.expectedScore {
				fmt.Printf("Expected score: %f, actual score: %f\n", caseSpec.expectedScore, actualScore)
				Expect(false).To(Equal(true))
			}
		})
	}
})
