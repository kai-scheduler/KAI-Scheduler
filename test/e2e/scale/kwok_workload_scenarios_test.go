// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/scale/topology"
)

func TestTopologyNodeCount(t *testing.T) {
	testCases := []struct {
		name      string
		requested int
		expected  int
	}{
		{name: "below minimum", requested: 100, expected: 512},
		{name: "default scale", requested: 500, expected: 512},
		{name: "exact power", requested: 512, expected: 512},
		{name: "one thousand nodes", requested: 1000, expected: 1024},
		{name: "two thousand nodes", requested: 2000, expected: 2048},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := topologyNodeCount(testCase.requested)
			if err != nil {
				t.Fatalf("topologyNodeCount(%d): %v", testCase.requested, err)
			}
			if actual != testCase.expected {
				t.Fatalf("topologyNodeCount(%d): got %d, want %d", testCase.requested, actual, testCase.expected)
			}
		})
	}

	if _, err := topologyNodeCount(0); err == nil {
		t.Fatal("expected non-positive node count to fail")
	}
	if _, err := topologyNodeCount(int(^uint(0) >> 1)); err == nil {
		t.Fatal("expected node count overflow to fail")
	}
}

func TestTopologyConfigForNodeCount(t *testing.T) {
	testCases := []struct {
		nodes         int
		racksPerBlock int
		nodePoolCount int
		nodesPerZone  int
	}{
		{nodes: 512, racksPerBlock: 16, nodePoolCount: 256, nodesPerZone: 256},
		{nodes: 1024, racksPerBlock: 32, nodePoolCount: 512, nodesPerZone: 512},
		{nodes: 2048, racksPerBlock: 64, nodePoolCount: 1024, nodesPerZone: 1024},
	}

	for _, testCase := range testCases {
		config, err := topologyConfigForNodeCount(testCase.nodes)
		if err != nil {
			t.Fatalf("topologyConfigForNodeCount(%d): %v", testCase.nodes, err)
		}
		if config.totalNodes() != testCase.nodes {
			t.Errorf("total nodes for %d: got %d", testCase.nodes, config.totalNodes())
		}
		if config.racksPerBlock != testCase.racksPerBlock {
			t.Errorf("racks per block for %d: got %d, want %d", testCase.nodes, config.racksPerBlock, testCase.racksPerBlock)
		}
		if config.nodePoolCount() != testCase.nodePoolCount {
			t.Errorf("node pools for %d: got %d, want %d", testCase.nodes, config.nodePoolCount(), testCase.nodePoolCount)
		}
		nodePools := topology.GenerateNodePools(config.levels(), config.nodesPerRack, map[string]string{})
		if len(nodePools) != testCase.nodePoolCount {
			t.Errorf("generated node pools for %d: got %d, want %d", testCase.nodes, len(nodePools), testCase.nodePoolCount)
		}
		if config.nodesPerZone() != testCase.nodesPerZone {
			t.Errorf("nodes per zone for %d: got %d, want %d", testCase.nodes, config.nodesPerZone(), testCase.nodesPerZone)
		}
	}

	if _, err := topologyConfigForNodeCount(500); err == nil {
		t.Fatal("expected unsupported topology node count to fail")
	}
}

func TestWorkloadScenarioSizing(t *testing.T) {
	if got := inferenceNodesPerDeployment(); got != 4 {
		t.Fatalf("inference nodes per deployment: got %d, want 4", got)
	}
	if got := inferencePodsPerDeployment(); got != 8 {
		t.Fatalf("inference pods per deployment: got %d, want 8", got)
	}
	for _, testCase := range []struct {
		nodes       int
		deployments int
		heroPods    int
	}{
		{nodes: 512, deployments: 128, heroPods: 256},
		{nodes: 2048, deployments: 512, heroPods: 1024},
	} {
		deployments, err := inferenceDeploymentCount(testCase.nodes)
		if err != nil || deployments != testCase.deployments {
			t.Errorf("inference deployments for %d nodes: got %d, want %d, err %v",
				testCase.nodes, deployments, testCase.deployments, err)
		}
		config, err := topologyConfigForNodeCount(testCase.nodes)
		if err != nil || config.nodesPerZone() != testCase.heroPods {
			t.Errorf("hero pods for %d nodes: got %d, want %d, err %v",
				testCase.nodes, config.nodesPerZone(), testCase.heroPods, err)
		}
	}
	if legacyTopologyWorkloadPods != 512 {
		t.Fatalf("legacy topology workload pods: got %d, want 512", legacyTopologyWorkloadPods)
	}
	if _, err := inferenceDeploymentCount(18); err == nil {
		t.Fatal("expected non-divisible inference node count to fail")
	}
	if victimMinMember, reclaimerPods, err := splitClusterForElasticReclaim(500); err != nil || victimMinMember != 250 || reclaimerPods != 250 {
		t.Fatalf("even cluster split: got victim minMember %d and %d reclaimers, err %v", victimMinMember, reclaimerPods, err)
	}
	if victimMinMember, reclaimerPods, err := splitClusterForElasticReclaim(15); err != nil || victimMinMember != 7 || reclaimerPods != 8 {
		t.Fatalf("odd cluster split: got victim minMember %d and %d reclaimers, err %v", victimMinMember, reclaimerPods, err)
	}
	if _, _, err := splitClusterForElasticReclaim(0); err == nil {
		t.Fatal("expected non-positive node count to fail")
	}
}

func TestDomainPathsForPods(t *testing.T) {
	pods := []v1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod"}, Spec: v1.PodSpec{NodeName: "node"}}}
	nodes := []v1.Node{{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node",
			Labels: map[string]string{
				topologyZoneLabel:  "zone1",
				topologyBlockLabel: "block1",
				topologyRackLabel:  "rack1",
			},
		},
	}}
	domains, err := domainPathsForPods(pods, nodes, []string{topologyZoneLabel, topologyBlockLabel, topologyRackLabel})
	if err != nil {
		t.Fatalf("domainPathsForPods: %v", err)
	}
	if len(domains) != 1 || domains[0] != topologyZoneLabel+"=zone1/"+topologyBlockLabel+"=block1/"+topologyRackLabel+"=rack1" {
		t.Fatalf("unexpected domains: %v", domains)
	}

	delete(nodes[0].Labels, topologyRackLabel)
	if _, err := domainPathsForPods(pods, nodes, []string{topologyZoneLabel, topologyBlockLabel, topologyRackLabel}); err == nil {
		t.Fatal("expected missing topology label to fail")
	}
	if _, err := domainPathsForPods(pods, nil, []string{topologyZoneLabel}); err == nil {
		t.Fatal("expected missing node to fail")
	}
	if _, err := domainPathsForPods(nil, nodes, []string{topologyZoneLabel}); err == nil {
		t.Fatal("expected missing pods to fail")
	}
	if _, err := domainPathsForPods(pods, nodes, nil); err == nil {
		t.Fatal("expected missing topology domains to fail")
	}
}

// A terminating Job Pod may retain both its tracking finalizer and its original
// PodScheduled condition even after KWOK has marked it Failed.
func TestReclaimCountsOnlyActiveScheduledPods(t *testing.T) {
	pods := []v1.Pod{}
	for _, phase := range []v1.PodPhase{v1.PodPending, v1.PodRunning, v1.PodFailed, v1.PodSucceeded} {
		pods = append(pods, v1.Pod{
			Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{
				Limits: v1.ResourceList{"nvidia.com/gpu": resource.MustParse("8")},
			}}}},
			Status: v1.PodStatus{Phase: phase, Conditions: []v1.PodCondition{
				{Type: v1.PodScheduled, Status: v1.ConditionTrue},
			}},
		})
	}
	pods = append(pods, v1.Pod{Status: v1.PodStatus{Phase: v1.PodPending}})
	count, gpus := scheduledResources(pods)
	if count != 2 || gpus != 16 || countActiveScheduledPods(pods) != count {
		t.Fatalf("expected two active scheduled Pods using 16 GPUs, got %d Pods and %d GPUs", count, gpus)
	}
}
