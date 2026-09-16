// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScaleTopologyConfig(t *testing.T) {
	if got := scaleTopologyConfig.totalNodes(); got != 512 {
		t.Fatalf("total nodes: got %d, want 512", got)
	}
	if got := scaleTopologyConfig.nodesPerZone(); got != 256 {
		t.Fatalf("nodes per zone: got %d, want 256", got)
	}
}

func TestWorkloadScenarioSizing(t *testing.T) {
	if got := inferenceNodesPerDeployment(); got != 4 {
		t.Fatalf("inference nodes per deployment: got %d, want 4", got)
	}
	if got := inferencePodsPerDeployment(); got != 8 {
		t.Fatalf("inference pods per deployment: got %d, want 8", got)
	}
	deployments, err := inferenceDeploymentCount(512)
	if err != nil || deployments != 128 {
		t.Fatalf("production inference deployments: got %d, err %v", deployments, err)
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
