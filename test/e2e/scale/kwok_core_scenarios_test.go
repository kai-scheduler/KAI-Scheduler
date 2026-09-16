// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTopologyConfigForProfile(t *testing.T) {
	tests := []struct {
		profile      string
		totalNodes   int
		nodesPerZone int
	}{
		{profile: "", totalNodes: 512, nodesPerZone: 256},
		{profile: smokeScaleProfile, totalNodes: 16, nodesPerZone: 8},
	}
	for _, test := range tests {
		config, err := topologyConfigForProfile(test.profile)
		if err != nil {
			t.Fatalf("topologyConfigForProfile(%q): %v", test.profile, err)
		}
		if got := config.totalNodes(); got != test.totalNodes {
			t.Fatalf("total nodes for %q: got %d, want %d", test.profile, got, test.totalNodes)
		}
		if got := config.nodesPerZone(); got != test.nodesPerZone {
			t.Fatalf("nodes per zone for %q: got %d, want %d", test.profile, got, test.nodesPerZone)
		}
	}
	if _, err := topologyConfigForProfile("unknown"); err == nil {
		t.Fatal("expected unsupported profile to fail")
	}
}

func TestCoreScenarioSizing(t *testing.T) {
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
	deployments, err = inferenceDeploymentCount(16)
	if err != nil || deployments != 4 {
		t.Fatalf("smoke inference deployments: got %d, err %v", deployments, err)
	}
	if _, err := inferenceDeploymentCount(18); err == nil {
		t.Fatal("expected non-divisible inference node count to fail")
	}
	if half, err := halfClusterSize(500); err != nil || half != 250 {
		t.Fatalf("half cluster: got %d, err %v", half, err)
	}
	if _, err := halfClusterSize(15); err == nil {
		t.Fatal("expected odd node count to fail")
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
