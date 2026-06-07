// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podWithAnnotations(ann map[string]string) *v1.Pod {
	return &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: ann}}
}

func TestNumaPlacementFromPod(t *testing.T) {
	observed := `[{"zone":"node-0","amount":{"cpu":"4","nvidia.com/gpu":"1"}}]`
	predicted := `[{"zone":"node-1","amount":{"cpu":"2"}}]`

	t.Run("nil when no annotation", func(t *testing.T) {
		assert.Nil(t, numaPlacementFromPod(podWithAnnotations(nil)))
	})

	t.Run("observed wins over predicted", func(t *testing.T) {
		p := numaPlacementFromPod(podWithAnnotations(map[string]string{
			NUMAPlacementObservedAnnotation:  observed,
			NUMAPlacementPredictedAnnotation: predicted,
		}))
		assert.Equal(t, []string{"node-0"}, p.Zones())
		gpu := p[0].Amount["nvidia.com/gpu"]
		assert.Equal(t, int64(1), gpu.Value())
	})

	t.Run("falls back to predicted", func(t *testing.T) {
		p := numaPlacementFromPod(podWithAnnotations(map[string]string{
			NUMAPlacementPredictedAnnotation: predicted,
		}))
		assert.Equal(t, []string{"node-1"}, p.Zones())
	})

	t.Run("nil on malformed annotation (no guessing)", func(t *testing.T) {
		assert.Nil(t, numaPlacementFromPod(podWithAnnotations(map[string]string{
			NUMAPlacementObservedAnnotation: "{not-json",
		})))
	})
}

func TestNumaPlacementClone(t *testing.T) {
	orig := NUMAPlacement{{Zone: "node-0", Amount: v1.ResourceList{"cpu": resource.MustParse("4")}}}
	clone := orig.Clone()

	// Mutating the clone's amount must not affect the original (deep copy).
	clone[0].Amount["cpu"] = resource.MustParse("8")
	clone[0].Zone = "node-1"

	origCPU := orig[0].Amount["cpu"]
	assert.Equal(t, int64(4), origCPU.Value(), "original amount unchanged")
	assert.Equal(t, "node-0", orig[0].Zone, "original zone unchanged")

	assert.Nil(t, NUMAPlacement(nil).Clone())
}
