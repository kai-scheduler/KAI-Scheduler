// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/api/constants"
)

func TestFractionAssignments(t *testing.T) {
	assignments := fractionAssignments(7001, defaultFractionMix)
	if len(assignments) != 7001 {
		t.Fatalf("got %d assignments, want 7001", len(assignments))
	}
	counts := map[int]int{}
	for _, perGPU := range assignments {
		counts[perGPU]++
	}
	if counts[2] != 3500 || counts[8] != 3501 {
		t.Fatalf("unexpected split: %v", counts)
	}
	if got := fractionPodCount(500); got != 7000 {
		t.Fatalf("fractionPodCount(500) = %d, want 7000", got)
	}
}

func TestFractionGPUsNeededFitsDefaultCluster(t *testing.T) {
	needed := fractionGPUsNeeded(fractionAssignments(fractionPodCount(500), defaultFractionMix))
	if needed > 500*gpusPerNode {
		t.Fatalf("default mix needs %.1f GPUs, cluster has %d", needed, 500*gpusPerNode)
	}
}

func fractionTestPod(name, node, group, memory string) v1.Pod {
	return v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{constants.GPUGroup: group},
			Annotations: map[string]string{fractionMemoryAnnotationKey(): memory},
		},
		Spec: v1.PodSpec{NodeName: node},
	}
}

func TestValidateFractionPacking(t *testing.T) {
	pods := []v1.Pod{
		fractionTestPod("a", "n1", "g1", "40960Mi"),
		fractionTestPod("b", "n1", "g1", "40960Mi"),
		fractionTestPod("c", "n2", "g1", "10240Mi"),
	}
	groups, err := validateFractionPacking(pods, 81920)
	if err != nil || groups != 2 {
		t.Fatalf("groups=%d err=%v, want 2 groups and no error", groups, err)
	}

	pods = append(pods, fractionTestPod("d", "n1", "g1", "1Mi"))
	if _, err := validateFractionPacking(pods, 81920); err == nil {
		t.Fatal("expected over-commit error")
	}

	unbound := fractionTestPod("e", "", "", "1Mi")
	if _, err := validateFractionPacking([]v1.Pod{unbound}, 81920); err == nil {
		t.Fatal("expected error for pod without GPU group")
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := percentile(sorted, 50); got != 5 {
		t.Fatalf("p50 = %v, want 5", got)
	}
	if got := percentile(sorted, 95); got != 10 {
		t.Fatalf("p95 = %v, want 10", got)
	}
	if got := percentile(nil, 95); got != 0 {
		t.Fatalf("empty percentile = %v, want 0", got)
	}
}
