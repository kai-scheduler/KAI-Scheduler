// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package statefulset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func newGrouper() *StatefulSetGrouper {
	dg := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	return NewStatefulSetGrouper(dg)
}

func statefulSetOwner(name, namespace string, annotations map[string]interface{}, spec map[string]interface{}) *unstructured.Unstructured {
	obj := map[string]interface{}{
		"kind":       "StatefulSet",
		"apiVersion": "apps/v1",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"uid":       "test-uid",
		},
	}
	if annotations != nil {
		obj["metadata"].(map[string]interface{})["annotations"] = annotations
	}
	if spec != nil {
		obj["spec"] = spec
	}
	return &unstructured.Unstructured{Object: obj}
}

// Case A: no annotation — existing MinAvailable=1 behavior unchanged.
func TestGetPodGroupMetadata_NoAnnotation(t *testing.T) {
	owner := statefulSetOwner("workers", "ns", nil, map[string]interface{}{
		"podManagementPolicy": "OrderedReady",
	})
	pg, err := newGrouper().GetPodGroupMetadata(owner, &v1.Pod{})
	assert.NoError(t, err)
	assert.Equal(t, int32(1), pg.MinAvailable)
}

// Case A (default policy, no annotation): still fine.
func TestGetPodGroupMetadata_NoAnnotation_DefaultPolicy(t *testing.T) {
	owner := statefulSetOwner("workers", "ns", nil, nil)
	pg, err := newGrouper().GetPodGroupMetadata(owner, &v1.Pod{})
	assert.NoError(t, err)
	assert.Equal(t, int32(1), pg.MinAvailable)
}

// Case B: annotation present + Parallel → use requested value.
func TestGetPodGroupMetadata_ValidAnnotation_Parallel(t *testing.T) {
	tests := []struct {
		annotation string
		want       int32
	}{
		{"4", 4},
		{"8", 8},
		{"1", 1},
	}
	for _, tt := range tests {
		t.Run(tt.annotation, func(t *testing.T) {
			owner := statefulSetOwner("workers", "ns",
				map[string]interface{}{"kai.scheduler/min-member": tt.annotation},
				map[string]interface{}{"podManagementPolicy": "Parallel"},
			)
			pg, err := newGrouper().GetPodGroupMetadata(owner, &v1.Pod{})
			assert.NoError(t, err)
			assert.Equal(t, tt.want, pg.MinAvailable)
		})
	}
}

// Case C: invalid annotation values → error.
func TestGetPodGroupMetadata_InvalidAnnotation(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
	}{
		{"non-numeric", "abc"},
		{"non-int", "1.5"},
		{"zero", "0"},
		{"negative", "-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := statefulSetOwner("workers", "ns",
				map[string]interface{}{"kai.scheduler/min-member": tt.annotation},
				map[string]interface{}{"podManagementPolicy": "Parallel"},
			)
			pg, err := newGrouper().GetPodGroupMetadata(owner, &v1.Pod{})
			assert.Error(t, err)
			assert.Nil(t, pg)
		})
	}
}

// Case D: annotation present + explicit OrderedReady → error.
func TestGetPodGroupMetadata_Annotation_ExplicitOrderedReady(t *testing.T) {
	owner := statefulSetOwner("workers", "ns",
		map[string]interface{}{"kai.scheduler/min-member": "4"},
		map[string]interface{}{"podManagementPolicy": "OrderedReady"},
	)
	pg, err := newGrouper().GetPodGroupMetadata(owner, &v1.Pod{})
	assert.Error(t, err)
	assert.Nil(t, pg)
	assert.Contains(t, err.Error(), "podManagementPolicy: Parallel")
	assert.Contains(t, err.Error(), "OrderedReady")
}

// Case E: annotation present + absent podManagementPolicy → treated as OrderedReady → error.
func TestGetPodGroupMetadata_Annotation_DefaultPolicy(t *testing.T) {
	owner := statefulSetOwner("workers", "ns",
		map[string]interface{}{"kai.scheduler/min-member": "4"},
		nil,
	)
	pg, err := newGrouper().GetPodGroupMetadata(owner, &v1.Pod{})
	assert.Error(t, err)
	assert.Nil(t, pg)
	assert.Contains(t, err.Error(), "OrderedReady (default)")
}

// Case B (min-member="1") + OrderedReady → still an error (strict contract: annotation present requires Parallel).
func TestGetPodGroupMetadata_Annotation_One_OrderedReady(t *testing.T) {
	owner := statefulSetOwner("workers", "ns",
		map[string]interface{}{"kai.scheduler/min-member": "1"},
		map[string]interface{}{"podManagementPolicy": "OrderedReady"},
	)
	pg, err := newGrouper().GetPodGroupMetadata(owner, &v1.Pod{})
	assert.Error(t, err)
	assert.Nil(t, pg)
	assert.Contains(t, err.Error(), "podManagementPolicy: Parallel")
}

// Case F: only batch-min-member present (no kai.scheduler/min-member) → ignored by StatefulSetGrouper.
func TestGetPodGroupMetadata_BatchAnnotationAlone_Ignored(t *testing.T) {
	owner := statefulSetOwner("workers", "ns",
		map[string]interface{}{"kai.scheduler/batch-min-member": "4"},
		map[string]interface{}{"podManagementPolicy": "OrderedReady"},
	)
	pg, err := newGrouper().GetPodGroupMetadata(owner, &v1.Pod{})
	assert.NoError(t, err)
	// kai.scheduler/batch-min-member is not read by StatefulSetGrouper; DefaultGrouper gives MinAvailable=1.
	assert.Equal(t, int32(1), pg.MinAvailable)
}

// Name() check.
func TestStatefulSetGrouper_Name(t *testing.T) {
	assert.Equal(t, "StatefulSet Grouper", newGrouper().Name())
}
