// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pytorch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
)

func TestPyTorchWarnOnSemiPreemptibleSegmented(t *testing.T) {
	tests := []struct {
		name            string
		segmented       bool
		semiPreemptible bool
		expectWarning   bool
	}{
		{name: "segmented and semi-preemptible warns", segmented: true, semiPreemptible: true, expectWarning: true},
		{name: "segmented but not semi-preemptible no warning", segmented: true, semiPreemptible: false, expectWarning: false},
		{name: "semi-preemptible but not segmented no warning", segmented: false, semiPreemptible: true, expectWarning: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var job = getPytorchJobWithSegments(1, 4, "2")
			if !tt.segmented {
				job = getPytorchJobWithSegments(1, 4, "")
			}
			grouper := newTestPyTorchGrouper()

			labels := map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			}
			if tt.semiPreemptible {
				labels[constants.PreemptibilityLabelKey] = "semi-preemptible"
			}
			annotations := map[string]string{}
			if tt.segmented {
				annotations[constants.SegmentSizeKey] = "2"
			}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-job-worker-0",
					Namespace:   "test_namespace",
					Labels:      labels,
					Annotations: annotations,
				},
			}

			metadata, err := grouper.GetPodGroupMetadata(job, pod)
			assert.NoError(t, err)
			if tt.expectWarning {
				assert.NotEmpty(t, metadata.Warnings)
			} else {
				assert.Empty(t, metadata.Warnings)
			}
		})
	}
}
